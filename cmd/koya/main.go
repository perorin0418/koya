// koya はカレントディレクトリをマウントした用途別の隔離コンテナを起動して中に入るためのコマンド。
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	"github.com/perorin0418/koya"
	"github.com/perorin0418/koya/internal/container"
	"github.com/perorin0418/koya/internal/profile"
	"github.com/perorin0418/koya/internal/runtime"
)

// version はビルド時に -ldflags "-X main.version=..." で埋め込む。
var version = ""

const usage = `koya - カレントディレクトリをマウントした用途別の隔離コンテナ

使い方:
  koya run [プロファイル] [オプション] [-- コマンド...]   環境に入る / コマンドを実行する
  koya ls [-a]                   このディレクトリのコンテナ一覧（-a で全ディレクトリ）
  koya stop [プロファイル]        停止する（省略時はこのディレクトリの全プロファイル）
  koya rm [-f] [プロファイル]     削除する（省略時はこのディレクトリの全プロファイル）
  koya profiles                  使えるプロファイルの一覧
  koya build [プロファイル...]    イメージをビルドし直す（省略時は全プロファイル）
  koya version                   バージョンを表示する
  koya help [コマンド]            ヘルプを表示する

プロファイルを省略したときは、上の階層へたどって見つかる .koya ファイル、
環境変数 KOYA_PROFILE、base の順に決めます。

環境変数:
  KOYA_RUNTIME   使うランタイム（podman / docker）。既定は podman → docker の順に自動検出
  KOYA_PROFILE   既定のプロファイル
`

const runUsage = `使い方: koya run [プロファイル] [オプション] [-- コマンド...]

カレントディレクトリをマウントしたコンテナに入ります。なければ作ります。
-- の後ろにコマンドを書くと、シェルに入らずにそのコマンドだけ実行します。

オプション（コンテナを作るときだけ効きます）:
  --net <モード>   ネットワーク（none で切断）
  --ro             カレントディレクトリを読み取り専用でマウントする
  -e KEY=VAL       環境変数（複数可）
  -p HOST:CTR      ポートを公開する（複数可）
  -v SRC:DST       追加でマウントする（複数可）
  --rm             使い捨て: 新しいコンテナで実行し、終了したら削除する
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			os.Exit(int(ee))
		}
		fmt.Fprintln(os.Stderr, "koya:", err)
		os.Exit(1)
	}
}

type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "run":
		return cmdRun(rest)
	case "ls", "list":
		return cmdLs(rest)
	case "stop":
		return cmdStop(rest)
	case "rm":
		return cmdRm(rest)
	case "profiles":
		return cmdProfiles(rest)
	case "build":
		return cmdBuild(rest)
	case "version", "--version":
		fmt.Println("koya", getVersion())
		return nil
	case "help", "-h", "--help":
		if len(rest) > 0 && rest[0] == "run" {
			fmt.Print(runUsage)
		} else {
			fmt.Print(usage)
		}
		return nil
	default:
		return fmt.Errorf("%q というコマンドはありません（koya help で一覧を表示）", cmd)
	}
}

func getVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// multiFlag は複数回指定できるフラグ。
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parseInterspersed はフラグと位置引数が混ざった引数を解析し、位置引数を返す。
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func flagErr(err error, help string) error {
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print(help)
		return exitError(0)
	}
	return fmt.Errorf("%v\n\n%s", err, help)
}

func cmdRun(args []string) error {
	var command []string
	for i, a := range args {
		if a == "--" {
			command = args[i+1:]
			args = args[:i]
			break
		}
	}
	fs := newFlagSet("run")
	var opts container.Options
	var env, ports, vols multiFlag
	var rm bool
	fs.StringVar(&opts.Net, "net", "", "")
	fs.BoolVar(&opts.ReadOnly, "ro", false, "")
	fs.Var(&env, "e", "")
	fs.Var(&ports, "p", "")
	fs.Var(&vols, "v", "")
	fs.BoolVar(&rm, "rm", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, runUsage)
	}
	if len(pos) > 1 {
		return fmt.Errorf("プロファイルは1つだけ指定できます（コマンドは -- の後ろに書きます）: %s", strings.Join(pos, " "))
	}
	opts.Env, opts.Ports, opts.Volumes = env, ports, vols
	optsGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name != "rm" {
			optsGiven = true
		}
	})

	wd, err := workdir()
	if err != nil {
		return err
	}
	if err := checkWorkdir(wd); err != nil {
		return err
	}
	prof := ""
	if len(pos) == 1 {
		prof = pos[0]
	} else {
		prof = defaultProfile(wd)
	}

	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	store := profile.NewStore(koya.Profiles)
	if err := store.Ensure(rt, prof, false); err != nil {
		return err
	}
	hash, err := store.Hash(prof)
	if err != nil {
		return err
	}
	spec := container.Spec{Profile: prof, Workdir: wd, Hash: hash, Opts: opts}
	tty := isTTY(os.Stdin) && isTTY(os.Stdout)
	if len(command) == 0 {
		command = container.DefaultShell
	}

	if rm {
		return rt.Exec(container.OneshotArgs(rt, spec, tty, command)...)
	}

	name := container.Name(prof, wd)
	info, exists := container.Inspect(rt, name)
	if !exists {
		fmt.Fprintf(os.Stderr, "koya: コンテナ %s を作成します\n", name)
		if err := container.Create(rt, spec); err != nil {
			return err
		}
	} else {
		if optsGiven && info.Opts != opts.Key() {
			created := info.Opts
			if created == "" {
				created = "オプションなし"
			}
			fmt.Fprintf(os.Stderr, "koya: 警告: 既存のコンテナは別のオプションで作られているため、今回のオプションは効きません（作成時: %s）。\n"+
				"      反映するには koya rm -f %s で削除してから作り直してください。\n", created, prof)
		}
		if info.Hash != "" && info.Hash != hash {
			fmt.Fprintf(os.Stderr, "koya: イメージが更新されています。反映するには koya rm -f %s で削除してから作り直してください。\n", prof)
		}
		if !info.Running() {
			if err := rt.Quiet("start", name); err != nil {
				return err
			}
		}
	}
	return rt.Exec(container.ExecArgs(name, wd, tty, command)...)
}

// workdir はカレントディレクトリの絶対パスを返す。
func workdir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(wd)
}

// checkWorkdir はマウントすると危ないディレクトリを拒否する。
func checkWorkdir(wd string) error {
	if wd == "/" {
		return errors.New("/ はマウントできません。プロジェクトのディレクトリで実行してください")
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == wd {
		return errors.New("ホームディレクトリそのものはマウントできません（隔離にならないため）。プロジェクトのディレクトリで実行してください")
	}
	return nil
}

// defaultProfile は .koya ファイル → KOYA_PROFILE → base の順に既定のプロファイルを決める。
func defaultProfile(wd string) string {
	for dir := wd; ; dir = filepath.Dir(dir) {
		if name := readProfileFile(filepath.Join(dir, ".koya")); name != "" {
			return name
		}
		if parent := filepath.Dir(dir); parent == dir {
			break
		}
	}
	if p := os.Getenv("KOYA_PROFILE"); p != "" {
		return p
	}
	return "base"
}

func readProfileFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return ""
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func cmdLs(args []string) error {
	const help = "使い方: koya ls [-a]\n\n  -a   全ディレクトリのコンテナを表示する\n"
	fs := newFlagSet("ls")
	all := fs.Bool("a", false, "")
	if _, err := parseInterspersed(fs, args); err != nil {
		return flagErr(err, help)
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	wd := ""
	if !*all {
		if wd, err = workdir(); err != nil {
			return err
		}
	}
	infos, err := container.List(rt, wd, "")
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		if *all {
			fmt.Fprintln(os.Stderr, "koya のコンテナはありません")
		} else {
			fmt.Fprintln(os.Stderr, "このディレクトリのコンテナはありません（koya ls -a で全ディレクトリ）")
		}
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if *all {
		fmt.Fprintln(tw, "PROFILE\tSTATUS\tWORKDIR\tNAME")
		for _, i := range infos {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", i.Profile, i.Status, i.Workdir, i.Name)
		}
	} else {
		fmt.Fprintln(tw, "PROFILE\tSTATUS\tNAME")
		for _, i := range infos {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", i.Profile, i.Status, i.Name)
		}
	}
	return tw.Flush()
}

// targets はこのディレクトリのコンテナのうち、指定プロファイル（省略時は全部）のものを返す。
func targets(rt *runtime.Runtime, profs []string) ([]container.Info, error) {
	wd, err := workdir()
	if err != nil {
		return nil, err
	}
	infos, err := container.List(rt, wd, "")
	if err != nil {
		return nil, err
	}
	if len(profs) == 0 {
		if len(infos) == 0 {
			return nil, errors.New("このディレクトリのコンテナはありません")
		}
		return infos, nil
	}
	var out []container.Info
	for _, p := range profs {
		found := false
		for _, i := range infos {
			if i.Profile == p {
				out = append(out, i)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("このディレクトリに %s のコンテナはありません", p)
		}
	}
	return out, nil
}

func cmdStop(args []string) error {
	const help = "使い方: koya stop [プロファイル...]\n"
	fs := newFlagSet("stop")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	ts, err := targets(rt, pos)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if !t.Running() {
			continue
		}
		if err := rt.Quiet("stop", "-t", "2", t.Name); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "停止しました: %s (%s)\n", t.Profile, t.Name)
	}
	return nil
}

func cmdRm(args []string) error {
	const help = "使い方: koya rm [-f] [プロファイル...]\n\n  -f   起動中でも削除する\n"
	fs := newFlagSet("rm")
	force := fs.Bool("f", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	ts, err := targets(rt, pos)
	if err != nil {
		return err
	}
	if !*force {
		var running []string
		for _, t := range ts {
			if t.Running() {
				running = append(running, t.Profile)
			}
		}
		if len(running) > 0 {
			return fmt.Errorf("起動中のコンテナがあります: %s（koya stop で止めるか、-f を付けてください）", strings.Join(running, ", "))
		}
	}
	for _, t := range ts {
		if err := rt.Quiet("rm", "-f", t.Name); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "削除しました: %s (%s)\n", t.Profile, t.Name)
	}
	return nil
}

func cmdProfiles(args []string) error {
	if len(args) > 0 {
		return errors.New("使い方: koya profiles")
	}
	store := profile.NewStore(koya.Profiles)
	rt, rtErr := runtime.Detect()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tBASE\tIMAGE")
	for _, n := range store.Names() {
		p, err := store.Get(n)
		if err != nil {
			fmt.Fprintf(tw, "%s\t(エラー: %v)\t\t\n", n, err)
			continue
		}
		src := string(p.Source)
		if p.Overrode {
			src += "（同梱を上書き）"
		}
		parent := p.Parent
		if parent == "" {
			parent = "-"
		}
		state := "-"
		if rtErr == nil {
			want, _ := store.Hash(n)
			switch have := profile.ImageHash(rt, n); {
			case have == "":
				state = "未ビルド"
			case have == want:
				state = "ビルド済み"
			default:
				state = "要再ビルド"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", n, src, parent, state)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nユーザー定義プロファイルの置き場所: %s/<名前>/Dockerfile\n", profile.UserDir())
	return nil
}

func cmdBuild(args []string) error {
	const help = "使い方: koya build [プロファイル...]\n"
	fs := newFlagSet("build")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	rt, err := runtime.Detect()
	if err != nil {
		return err
	}
	store := profile.NewStore(koya.Profiles)
	if len(pos) == 0 {
		pos = store.Names()
	}
	built := map[string]bool{}
	for _, n := range pos {
		if built[n] {
			continue
		}
		if err := store.Ensure(rt, n, true); err != nil {
			return err
		}
		built[n] = true
	}
	return nil
}
