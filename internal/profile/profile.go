// Package profile はプロファイル（用途別イメージの定義）の解決とビルドを扱う。
package profile

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/perorin0418/koya/internal/config"
	"github.com/perorin0418/koya/internal/runtime"
)

// Source はプロファイルの出どころ。
type Source string

const (
	Builtin Source = "builtin" // バイナリに同梱
	User    Source = "user"    // ~/.config/koya/profiles
)

// Profile は1つのプロファイル。
type Profile struct {
	Name     string
	Source   Source
	Path     string            // ユーザー定義ならディレクトリのパス
	Files    map[string][]byte // ビルドコンテキストのファイル（相対パス → 内容）
	Parent   string            // FROM koya/<名前> で参照している別プロファイル
	Overrode bool              // 同名の同梱プロファイルを上書きしているか
}

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	argRe    = regexp.MustCompile(`(?im)^\s*ARG\s+([A-Za-z_][A-Za-z0-9_]*)`)
	escapeRe = regexp.MustCompile("(?m)\\A(?:#.*\\n)*?#\\s*escape\\s*=\\s*([\\\\`])")
	fromRe   = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--\S+\s+)*(?:localhost/)?koya/([a-z0-9][a-z0-9_.-]*)(?::latest)?(?:\s|$)`)
)

// ValidName はプロファイル名として使えるかを返す。
func ValidName(name string) bool { return nameRe.MatchString(name) }

// ImageName はプロファイルのイメージ名を返す。
// localhost/ を付けて、レジストリから同名のイメージを取ってこないようにする。
func ImageName(name string) string { return "localhost/koya/" + name + ":latest" }

// Store はプロファイルの置き場所（同梱とユーザー定義）。
type Store struct {
	builtin  fs.FS    // <dir>/<名前>/... を持つ FS
	dir      string   // builtin の中のプロファイルのディレクトリ（profiles-linux / profiles-win）
	userDirs []string // ユーザー定義の置き場所。先にあるものを優先する（先頭が今の名前、後ろは旧名）
	cache    map[string]*Profile
	hashes   map[string]string

	// ビルドのときに渡す引数（Windows コンテナのベースイメージのタグなど）。
	// BuildArgs はハッシュにも含め、変わったら再ビルドする。
	BuildArgs  []string // KEY=VAL
	BuildFlags []string // --isolation process など

	// NoCache なら、レイヤーのキャッシュを使わずにビルドし、外部のイメージ（FROM・COPY --from）も
	// 先に取り直す。親プロファイルも内容に関係なく作り直す（koya build --no-cache）。
	NoCache bool
	rebuilt map[string]bool // NoCache で作り直し済みのプロファイル（同じ親を何度も作り直さない）
}

// NewStore は Linux コンテナ用の Store を作る。builtin は "profiles-linux" ディレクトリを根に持つ FS。
// ユーザー定義は設定ディレクトリの下の profiles-linux。旧名の profiles に置いたものも読む。
func NewStore(builtin fs.FS) *Store {
	return newStore(builtin, "profiles-linux", "profiles")
}

// NewWinStore は Windows コンテナ用（koya run --win）の Store を作る。
// 同梱は builtin の "profiles-win"、ユーザー定義は設定ディレクトリの下の profiles-win。
func NewWinStore(builtin fs.FS) *Store {
	return newStore(builtin, "profiles-win")
}

// newStore は Store を作る。legacy はユーザー定義の旧名の置き場所（dir より後に探す）。
func newStore(builtin fs.FS, dir string, legacy ...string) *Store {
	var userDirs []string
	for _, name := range append([]string{dir}, legacy...) {
		if d := userDirFor(name); d != "" {
			userDirs = append(userDirs, d)
		}
	}
	return &Store{builtin: builtin, dir: dir, userDirs: userDirs, cache: map[string]*Profile{}, hashes: map[string]string{}}
}

// UserDir はこの Store のユーザー定義プロファイルのディレクトリ（今の名前のほう）。
func (s *Store) UserDir() string {
	if len(s.userDirs) == 0 {
		return ""
	}
	return s.userDirs[0]
}

// userPath はユーザー定義プロファイル name のディレクトリを返す（なければ空）。
func (s *Store) userPath(name string) string {
	for _, d := range s.userDirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(filepath.Join(p, "Dockerfile")); err == nil {
			return p
		}
	}
	return ""
}

func userDirFor(name string) string {
	dir := config.Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// Names は使えるプロファイル名を並べて返す。
func (s *Store) Names() []string {
	set := map[string]bool{}
	if ents, err := fs.ReadDir(s.builtin, s.dir); err == nil {
		for _, e := range ents {
			if e.IsDir() && ValidName(e.Name()) {
				set[e.Name()] = true
			}
		}
	}
	for _, d := range s.userDirs {
		if ents, err := os.ReadDir(d); err == nil {
			for _, e := range ents {
				if e.IsDir() && ValidName(e.Name()) {
					if _, err := os.Stat(filepath.Join(d, e.Name(), "Dockerfile")); err == nil {
						set[e.Name()] = true
					}
				}
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Get はプロファイルを読み込む。ユーザー定義を同梱より優先する。
func (s *Store) Get(name string) (*Profile, error) {
	if p, ok := s.cache[name]; ok {
		return p, nil
	}
	if !ValidName(name) {
		return nil, fmt.Errorf("プロファイル名 %q は使えません（英小文字・数字・_ . - のみ）", name)
	}
	_, builtinErr := fs.Stat(s.builtin, path.Join(s.dir, name, "Dockerfile"))
	hasBuiltin := builtinErr == nil

	var p *Profile
	if userPath := s.userPath(name); userPath != "" {
		files, err := readFiles(os.DirFS(userPath), ".")
		if err != nil {
			return nil, err
		}
		p = &Profile{Name: name, Source: User, Path: userPath, Files: files, Overrode: hasBuiltin}
	} else if hasBuiltin {
		files, err := readFiles(s.builtin, path.Join(s.dir, name))
		if err != nil {
			return nil, err
		}
		p = &Profile{Name: name, Source: Builtin, Files: files}
	} else {
		return nil, fmt.Errorf("プロファイル %q がありません（使えるもの: %s）", name, strings.Join(s.Names(), ", "))
	}
	p.Parent = parentOf(p.Files["Dockerfile"])
	s.cache[name] = p
	return p, nil
}

func readFiles(fsys fs.FS, root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		files[rel] = b
		return nil
	})
	return files, err
}

// parentOf は Dockerfile の FROM koya/<名前> から親プロファイル名を取り出す。
func parentOf(dockerfile []byte) string {
	sc := bufio.NewScanner(strings.NewReader(string(dockerfile)))
	for sc.Scan() {
		if m := fromRe.FindStringSubmatch(sc.Text()); m != nil {
			return m[1]
		}
	}
	return ""
}

// Hash はプロファイルの内容（親プロファイルを含む）のハッシュ。変わったら再ビルドする。
func (s *Store) Hash(name string) (string, error) {
	return s.hash(name, map[string]bool{})
}

func (s *Store) hash(name string, visiting map[string]bool) (string, error) {
	if h, ok := s.hashes[name]; ok {
		return h, nil
	}
	if visiting[name] {
		return "", fmt.Errorf("プロファイル %q の FROM koya/... が循環しています", name)
	}
	visiting[name] = true
	p, err := s.Get(name)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	keys := make([]string, 0, len(p.Files))
	for k := range p.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s\x00%d\x00", k, len(p.Files[k]))
		h.Write(p.Files[k])
	}
	args := append([]string(nil), s.BuildArgs...)
	sort.Strings(args)
	for _, a := range args {
		fmt.Fprintf(h, "arg\x00%s\x00", a)
	}
	if p.Parent != "" {
		ph, err := s.hash(p.Parent, visiting)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "parent\x00%s", ph)
	}
	sum := hex.EncodeToString(h.Sum(nil))[:16]
	s.hashes[name] = sum
	return sum, nil
}

// ImageHash はビルド済みイメージの koya.hash ラベルを返す。イメージがなければ空文字。
func ImageHash(rt *runtime.Runtime, name string) string {
	out, err := rt.Output("image", "inspect", "--format", `{{index .Config.Labels "koya.hash"}}`, ImageName(name))
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(out)
	if v == "<no value>" {
		return ""
	}
	return v
}

// Ensure はイメージが最新でなければビルドする（親プロファイルも先に用意する）。
// force なら name 自身は必ずビルドし直す。NoCache なら親も含めて必ずビルドし直す（1回ずつ）。
func (s *Store) Ensure(rt *runtime.Runtime, name string, force bool) error {
	p, err := s.Get(name)
	if err != nil {
		return err
	}
	want, err := s.Hash(name)
	if err != nil {
		return err
	}
	if p.Parent != "" {
		if err := s.Ensure(rt, p.Parent, false); err != nil {
			return err
		}
	}
	if s.NoCache {
		if s.rebuilt[name] {
			return nil
		}
	} else if !force && ImageHash(rt, name) == want {
		return nil
	}
	if err := s.build(rt, p, want); err != nil {
		return err
	}
	if s.NoCache {
		if s.rebuilt == nil {
			s.rebuilt = map[string]bool{}
		}
		s.rebuilt[name] = true
	}
	return nil
}

func (s *Store) build(rt *runtime.Runtime, p *Profile, hash string) error {
	dir, err := os.MkdirTemp("", "koya-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for rel, b := range p.Files {
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "koya: イメージ %s をビルドします（%s）\n", ImageName(p.Name), p.Source)
	args := []string{"build",
		"-t", ImageName(p.Name),
		"--label", "koya.managed=true",
		"--label", "koya.profile=" + p.Name,
		"--label", "koya.hash=" + hash,
	}
	buildArgs := usedBuildArgs(p.Files["Dockerfile"], s.BuildArgs)
	for _, a := range buildArgs {
		args = append(args, "--build-arg", a)
	}
	if s.NoCache {
		// --pull は使わない。FROM localhost/koya/... の親プロファイルまでレジストリから
		// 取ろうとして失敗するため、外部のイメージだけを名前を挙げて取り直す。
		for _, img := range ExternalImages(p.Files["Dockerfile"], buildArgs) {
			fmt.Fprintf(os.Stderr, "koya: イメージ %s を取り直します\n", img)
			if err := rt.Run("pull", img); err != nil {
				return errors.New("イメージの取得に失敗しました: " + err.Error())
			}
		}
		args = append(args, "--no-cache")
	}
	args = append(args, s.BuildFlags...)
	err = rt.Run(append(args, dir)...)
	if err != nil {
		return errors.New("イメージのビルドに失敗しました: " + err.Error())
	}
	return nil
}

// ExternalImages は Dockerfile が使う外部のイメージ（FROM と COPY --from=）を、出てきた順に返す。
// 親プロファイル（koya/<名前>）、ステージ名、ステージ番号、scratch、展開できない変数を含むものは除く。
// 変数は buildArgs、なければ最初の FROM より前の ARG の既定値で展開する。
func ExternalImages(dockerfile []byte, buildArgs []string) []string {
	vars := map[string]string{}
	stages := map[string]bool{}
	seen := map[string]bool{}
	var out []string
	add := func(img string) {
		for k, v := range vars {
			img = strings.ReplaceAll(img, "${"+k+"}", v)
			img = strings.ReplaceAll(img, "$"+k, v)
		}
		l := strings.ToLower(img)
		switch {
		case img == "", strings.Contains(img, "$"), stages[l], l == "scratch",
			strings.HasPrefix(l, "localhost/koya/"), strings.HasPrefix(l, "koya/"), seen[img]:
			return
		}
		if _, err := strconv.Atoi(img); err == nil {
			return
		}
		seen[img] = true
		out = append(out, img)
	}
	sawFrom := false
	for _, line := range logicalLines(dockerfile) {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "ARG":
			if sawFrom || len(f) < 2 {
				continue
			}
			if k, v, ok := strings.Cut(f[1], "="); ok {
				vars[k] = strings.Trim(v, `"'`)
			}
		case "FROM":
			if !sawFrom {
				sawFrom = true
				for _, a := range buildArgs {
					if k, v, ok := strings.Cut(a, "="); ok {
						vars[k] = v
					}
				}
			}
			rest := f[1:]
			for len(rest) > 0 && strings.HasPrefix(rest[0], "--") {
				rest = rest[1:]
			}
			if len(rest) == 0 {
				continue
			}
			add(rest[0])
			if len(rest) >= 3 && strings.EqualFold(rest[1], "AS") {
				stages[strings.ToLower(rest[2])] = true
			}
		case "COPY":
			for _, x := range f[1:] {
				if !strings.HasPrefix(x, "--") {
					break
				}
				if v, ok := strings.CutPrefix(x, "--from="); ok {
					add(v)
				}
			}
		}
	}
	return out
}

// logicalLines は Dockerfile を、行の継続（既定は \、# escape=` なら `）をつないだ行に分ける。
func logicalLines(dockerfile []byte) []string {
	text := strings.ReplaceAll(string(dockerfile), "\r\n", "\n")
	esc := "\\"
	if m := escapeRe.FindStringSubmatch(text); m != nil {
		esc = m[1]
	}
	var out []string
	var cur strings.Builder
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimRight(l, " \t")
		if strings.HasPrefix(strings.TrimSpace(t), "#") {
			continue // コメントは継続の対象にならない（継続の途中にあっても飛ばす）
		}
		if strings.HasSuffix(t, esc) {
			cur.WriteString(strings.TrimSuffix(t, esc))
			cur.WriteString(" ")
			continue
		}
		cur.WriteString(t)
		out = append(out, cur.String())
		cur.Reset()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// usedBuildArgs は、Dockerfile が ARG で宣言しているビルド引数だけを返す
// （宣言していない引数を渡すと、docker build が「使われなかった」と警告するため）。
func usedBuildArgs(dockerfile []byte, buildArgs []string) []string {
	declared := map[string]bool{}
	for _, m := range argRe.FindAllStringSubmatch(string(dockerfile), -1) {
		declared[m[1]] = true
	}
	var out []string
	for _, a := range buildArgs {
		k, _, _ := strings.Cut(a, "=")
		if declared[k] {
			out = append(out, a)
		}
	}
	return out
}

// ImageLabel はビルド済みイメージのラベルの値を返す。なければ空文字。
func ImageLabel(rt *runtime.Runtime, name, key string) string {
	out, err := rt.Output("image", "inspect", "--format", `{{index .Config.Labels "`+key+`"}}`, ImageName(name))
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(out)
	if v == "<no value>" {
		return ""
	}
	return v
}
