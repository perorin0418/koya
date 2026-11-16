// Package container はコンテナの名前付け、作成、一覧などを扱う。
package container

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perorin0418/koya/internal/profile"
	"github.com/perorin0418/koya/internal/runtime"
)

// ラベル名
const (
	LabelManaged = "koya.managed"
	LabelProfile = "koya.profile"
	LabelWorkdir = "koya.workdir"
	LabelHash    = "koya.hash"
	LabelOpts    = "koya.opts"
)

// DefaultShell はシェルを起動するコマンド。標準入力が端末なら対話シェル、
// パイプならスクリプトとして読む。bash がないイメージでも動くようにする。
var DefaultShell = []string{"sh", "-c",
	`if [ ! -t 0 ]; then if command -v bash >/dev/null 2>&1; then exec bash -s; else exec sh -s; fi; fi; ` +
		`if [ -f /etc/koya/bashrc ] && command -v bash >/dev/null 2>&1; then exec bash --rcfile /etc/koya/bashrc -i; ` +
		`elif command -v bash >/dev/null 2>&1; then exec bash -i; else exec sh -i; fi`}

// Options はコンテナ作成時の設定。
type Options struct {
	Net      string
	ReadOnly bool
	Env      []string
	Ports    []string
	Volumes  []string
}

// Key は設定を比較用の文字列にする（ラベル koya.opts に入れる）。
func (o Options) Key() string {
	var parts []string
	if o.Net != "" {
		parts = append(parts, "net="+o.Net)
	}
	if o.ReadOnly {
		parts = append(parts, "ro")
	}
	add := func(prefix string, vs []string) {
		s := append([]string(nil), vs...)
		sort.Strings(s)
		for _, v := range s {
			parts = append(parts, prefix+"="+v)
		}
	}
	add("e", o.Env)
	add("p", o.Ports)
	add("v", o.Volumes)
	return strings.Join(parts, ";")
}

// Name はコンテナ名を返す: koya-<プロファイル>-<ディレクトリ名>-<パスのハッシュ8桁>
func Name(prof, workdir string) string {
	sum := sha256.Sum256([]byte(workdir))
	return fmt.Sprintf("koya-%s-%s-%s", prof, sanitize(filepath.Base(workdir)), hex.EncodeToString(sum[:])[:8])
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > 30 {
		out = out[:30]
	}
	if out == "" {
		out = "root"
	}
	return out
}

// Info はコンテナの状態。
type Info struct {
	Name    string
	Profile string
	Workdir string
	Status  string
	Hash    string
	Opts    string
}

// Running は起動中かを返す。
func (i Info) Running() bool { return i.Status == "running" }

const inspectFormat = `{{.Name}}	{{index .Config.Labels "koya.profile"}}	{{index .Config.Labels "koya.workdir"}}	{{.State.Status}}	{{index .Config.Labels "koya.hash"}}	{{index .Config.Labels "koya.opts"}}`

func parseInfo(line string) (Info, bool) {
	// 末尾の空のフィールドが欠けていても読めるように、6つに揃える
	f := strings.Split(strings.TrimRight(line, "\r"), "\t")
	if len(f) < 4 {
		return Info{}, false
	}
	for len(f) < 6 {
		f = append(f, "")
	}
	for i := range f {
		if f[i] == "<no value>" {
			f[i] = ""
		}
	}
	return Info{
		Name:    strings.TrimPrefix(f[0], "/"),
		Profile: f[1],
		Workdir: f[2],
		Status:  f[3],
		Hash:    f[4],
		Opts:    f[5],
	}, true
}

// Inspect はコンテナの状態を返す。存在しなければ ok=false。
func Inspect(rt *runtime.Runtime, name string) (Info, bool) {
	out, err := rt.Output("container", "inspect", "--format", inspectFormat, name)
	if err != nil {
		return Info{}, false
	}
	return parseInfo(strings.TrimRight(out, "\r\n"))
}

// List は koya が作ったコンテナを返す。workdir・prof が空ならその条件で絞らない。
func List(rt *runtime.Runtime, workdir, prof string) ([]Info, error) {
	args := []string{"ps", "-a", "-q", "--filter", "label=" + LabelManaged + "=true"}
	if workdir != "" {
		args = append(args, "--filter", "label="+LabelWorkdir+"="+workdir)
	}
	if prof != "" {
		args = append(args, "--filter", "label="+LabelProfile+"="+prof)
	}
	out, err := rt.Output(args...)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) == 0 {
		return nil, nil
	}
	out, err = rt.Output(append([]string{"container", "inspect", "--format", inspectFormat}, ids...)...)
	if err != nil {
		return nil, err
	}
	var infos []Info
	for _, line := range strings.Split(strings.TrimRight(out, "\r\n"), "\n") {
		if info, ok := parseInfo(line); ok {
			infos = append(infos, info)
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Workdir != infos[j].Workdir {
			return infos[i].Workdir < infos[j].Workdir
		}
		return infos[i].Profile < infos[j].Profile
	})
	return infos, nil
}

// Spec はコンテナ作成に必要な情報。
type Spec struct {
	Profile string
	Workdir string
	Hash    string
	Opts    Options
}

// RunArgs は run コマンドの共通引数（イメージ名の手前まで）を返す。
func RunArgs(rt *runtime.Runtime, s Spec) []string {
	mount := s.Workdir + ":" + s.Workdir + ":z"
	if s.Opts.ReadOnly {
		mount = s.Workdir + ":" + s.Workdir + ":ro,z"
	}
	args := []string{
		"--label", LabelManaged + "=true",
		"--label", LabelProfile + "=" + s.Profile,
		"--label", LabelWorkdir + "=" + s.Workdir,
		"--label", LabelHash + "=" + s.Hash,
		"--label", LabelOpts + "=" + s.Opts.Key(),
		"--hostname", "koya-" + s.Profile,
		"-e", "KOYA_PROFILE=" + s.Profile,
		"-v", mount,
		"-w", s.Workdir,
	}
	args = append(args, userArgs(rt)...)
	if s.Opts.Net != "" {
		args = append(args, "--network", s.Opts.Net)
	}
	for _, e := range s.Opts.Env {
		args = append(args, "-e", e)
	}
	for _, p := range s.Opts.Ports {
		args = append(args, "-p", p)
	}
	for _, v := range s.Opts.Volumes {
		args = append(args, "-v", v)
	}
	return args
}

// userArgs はマウントしたファイルの所有者をホストのユーザーのままにするための引数。
func userArgs(rt *runtime.Runtime) []string {
	switch {
	case rt.Kind == "podman" && rt.Rootless:
		// ホストのユーザーをコンテナ内の dev (1000) に対応づける
		return []string{"--userns=keep-id:uid=1000,gid=1000", "--user", "1000:1000"}
	case rt.Rootless:
		// rootless では、コンテナ内の root がホストのユーザーに対応する
		return []string{"--user", "0:0"}
	default:
		return []string{"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-e", "HOME=/home/dev"}
	}
}

// Create は常駐用のコンテナを作って起動する。
func Create(rt *runtime.Runtime, s Spec) error {
	args := []string{"run", "-d", "--init", "--name", Name(s.Profile, s.Workdir), "--entrypoint", "sleep"}
	args = append(args, RunArgs(rt, s)...)
	args = append(args, profile.ImageName(s.Profile), "infinity")
	return rt.Quiet(args...)
}

// ExecArgs は exec コマンドの引数を返す。
func ExecArgs(name, workdir string, tty bool, cmd []string) []string {
	args := []string{"exec", "-i"}
	if tty {
		args = append(args, "-t")
		if term := os.Getenv("TERM"); term != "" {
			args = append(args, "-e", "TERM="+term)
		}
	}
	args = append(args, "-w", workdir, name)
	return append(args, cmd...)
}

// OneshotArgs は --rm 用の run コマンドの引数を返す（終了時にコンテナを消す）。
func OneshotArgs(rt *runtime.Runtime, s Spec, tty bool, cmd []string) []string {
	args := []string{"run", "--rm", "-i", "--init"}
	if tty {
		args = append(args, "-t")
		if term := os.Getenv("TERM"); term != "" {
			args = append(args, "-e", "TERM="+term)
		}
	}
	args = append(args, "--entrypoint", cmd[0])
	args = append(args, RunArgs(rt, s)...)
	args = append(args, profile.ImageName(s.Profile))
	return append(args, cmd[1:]...)
}
