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
	nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
	fromRe = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--\S+\s+)*(?:localhost/)?koya/([a-z0-9][a-z0-9_.-]*)(?::latest)?(?:\s|$)`)
)

// ValidName はプロファイル名として使えるかを返す。
func ValidName(name string) bool { return nameRe.MatchString(name) }

// ImageName はプロファイルのイメージ名を返す。
// localhost/ を付けて、レジストリから同名のイメージを取ってこないようにする。
func ImageName(name string) string { return "localhost/koya/" + name + ":latest" }

// Store はプロファイルの置き場所（同梱とユーザー定義）。
type Store struct {
	builtin fs.FS  // <dir>/<名前>/... を持つ FS
	dir     string // builtin の中のプロファイルのディレクトリ（profiles / profiles-win）
	userDir string
	cache   map[string]*Profile
	hashes  map[string]string

	// ビルドのときに渡す引数（Windows コンテナのベースイメージのタグなど）。
	// BuildArgs はハッシュにも含め、変わったら再ビルドする。
	BuildArgs  []string // KEY=VAL
	BuildFlags []string // --isolation process など
}

// NewStore は Linux コンテナ用の Store を作る。builtin は "profiles" ディレクトリを根に持つ FS。
func NewStore(builtin fs.FS) *Store {
	return newStore(builtin, "profiles")
}

// NewWinStore は Windows コンテナ用（koya run --win）の Store を作る。
// 同梱は builtin の "profiles-win"、ユーザー定義は設定ディレクトリの下の profiles-win。
func NewWinStore(builtin fs.FS) *Store {
	return newStore(builtin, "profiles-win")
}

func newStore(builtin fs.FS, dir string) *Store {
	return &Store{builtin: builtin, dir: dir, userDir: userDirFor(dir), cache: map[string]*Profile{}, hashes: map[string]string{}}
}

// UserDir はユーザー定義プロファイルのディレクトリ（設定ディレクトリの下の profiles）。
func UserDir() string { return userDirFor("profiles") }

// UserDir はこの Store のユーザー定義プロファイルのディレクトリ。
func (s *Store) UserDir() string { return s.userDir }

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
	if s.userDir != "" {
		if ents, err := os.ReadDir(s.userDir); err == nil {
			for _, e := range ents {
				if e.IsDir() && ValidName(e.Name()) {
					if _, err := os.Stat(filepath.Join(s.userDir, e.Name(), "Dockerfile")); err == nil {
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
	userPath := filepath.Join(s.userDir, name)
	if _, err := os.Stat(filepath.Join(userPath, "Dockerfile")); s.userDir != "" && err == nil {
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
// force なら name 自身は必ずビルドし直す。
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
	if !force && ImageHash(rt, name) == want {
		return nil
	}
	return s.build(rt, p, want)
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
	for _, a := range s.BuildArgs {
		args = append(args, "--build-arg", a)
	}
	args = append(args, s.BuildFlags...)
	err = rt.Run(append(args, dir)...)
	if err != nil {
		return errors.New("イメージのビルドに失敗しました: " + err.Error())
	}
	return nil
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
