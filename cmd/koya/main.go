// koya はカレントディレクトリをマウントした用途別の隔離コンテナを起動して中に入るためのコマンド。
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	goruntime "runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/perorin0418/koya"
	"github.com/perorin0418/koya/internal/config"
	"github.com/perorin0418/koya/internal/container"
	"github.com/perorin0418/koya/internal/profile"
	"github.com/perorin0418/koya/internal/runtime"
	"github.com/perorin0418/koya/internal/selfupdate"
	"github.com/perorin0418/koya/internal/winengine"
)

// version はビルド時に -ldflags "-X main.version=..." で埋め込む。
var version = ""

const usage = `koya - カレントディレクトリをマウントした用途別の隔離コンテナ

使い方:
  koya run [プロファイル] [オプション] [-- コマンド...]   環境に入る / コマンドを実行する
  koya attach [--win] [プロファイル] [セッション]   セッションにつなぎ直す
  koya ls [-a]                   このディレクトリのコンテナとセッションの一覧（-a で全ディレクトリ）
  koya stop [プロファイル]        停止する（省略時はこのディレクトリの全プロファイル）
  koya rm [-f] [プロファイル]     削除する（省略時はこのディレクトリの全プロファイル）
  koya profiles                  使えるプロファイルの一覧
  koya build [プロファイル...]    イメージをビルドし直す（省略時は全プロファイル）
  koya self-update [--check]     koya 本体を最新版に更新する（--check は確認だけ）
  koya win <status|setup|uninstall>   Windows コンテナ用のデーモンを管理する（Windows のみ）
  koya version                   バージョンを表示する
  koya help [コマンド]            ヘルプを表示する

プロファイルを省略したときは、上の階層へたどって見つかる .koya ファイル、
環境変数 KOYA_PROFILE、base の順に決めます。

run・ls・stop・rm・profiles・build は、--linux（Linux コンテナ。省略時の既定）か
--win（Windows コンテナ。Windows のみ。koya help win）で対象を選びます。

環境変数:
  KOYA_RUNTIME   使うランタイム（podman / docker）。既定は podman → docker の順に自動検出
  KOYA_PROFILE   既定のプロファイル
  KOYA_NO_UPDATE_CHECK  設定すると、起動時に新しい版を確かめない
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
  --home           永続ホームを使う（ホストのディレクトリをホームにマウントする）
  --no-home        永続ホームを使わない（設定やプロファイルで有効でも）
  --home-scope S   永続ホームの単位: shared（プロファイルごと）/ project（プロジェクトごと）
  --gpu            GPU を必ず使う（見つからなければエラー）
  --no-gpu         GPU を使わない
                   どちらもなければ、GPU（/dev/kfd、/dev/dri）があれば自動で使う（Linux のみ）

そのほかのオプション:
  --session[=名前] セッションの中で動かす。端末が切れても動き続け、同じコマンドか
                   koya attach でつなぎ直せる（名前の既定はコマンド名、なければ shell）。
                   プロファイルに LABEL koya.session="true" があれば、付けなくても有効
  --no-session     セッションを使わない（プロファイルで有効でも）
  --rm             使い捨て: 新しいコンテナで実行し、終了したら削除する
  --linux          Linux コンテナで動かす（既定）
  --win            Windows コンテナで動かす（Windows のみ。koya help win）

コンテナ内では koya を実行したユーザー（同じ名前・UID・ホームのパス）で動きます。
永続ホームの置き場所などは ~/.config/koya/config で設定します（koya help config）。
`

const configUsage = `設定ファイル: ~/.config/koya/config（$XDG_CONFIG_HOME/koya/config、Windows は %APPDATA%\koya\config）

「キー = 値」の形で書きます。# 以降はコメントです。

  home.linux.dir = /mnt/pool/koya-home   永続ホームの置き場所（既定: ~/.local/share/koya/home）
                                       旧名の home.dir も同じ意味で読みます
  home.win.dir   = C:\koya\home-win      --win（Windows コンテナ）の永続ホームの置き場所
                                       （既定: <home.linux.dir>-win）
  home.enable = claude, python        既定で永続ホームを使うプロファイル（all で全部）
  home.scope  = shared                shared: プロファイルごとに1つ（全プロジェクトで共有）
                                      project: プロジェクトごとに分ける
  gpu         = auto                  auto: GPU があれば使う（既定）/ on: 必ず使う / off: 使わない

永続ホームを使うかどうかは、--home / --no-home、home.enable、
プロファイルの LABEL koya.home="true" の順に決まります。
GPU を使うかどうかは、--gpu / --no-gpu、gpu の順に決まります（Linux のホストの Linux コンテナのみ）。
`

const winUsage = `使い方: koya run --win [プロファイル] [オプション] [-- コマンド...]
       koya win <status|setup|uninstall>

--win を付けると、Linux コンテナの代わりに Windows コンテナ（プロセス分離）で動かします。
VM を挟まないので、カレントディレクトリのファイルをネイティブに近い速さで読み書きできます。
Windows 版の koya でだけ使えます（WSL の中の koya からは使えません）。

  - koya 専用のデーモン（Docker の静的バイナリの dockerd.exe）を使います。初めて使うときに自動で
    取ってきて（SHA-256 を照合）、Windows サービス koya-docker として登録します。管理者権限の確認が出ます。
    Rancher Desktop・Docker Desktop のデーモンとは別に動き、ぶつかりません。
  - Windows の「コンテナー」機能が必要です（有効でなければ、有効にする手順を表示します）。
  - プロファイルは Windows 用の別系統です（同梱: base・node・python・dotnet・claude。
    ユーザー定義: %APPDATA%\koya\profiles-win\<名前>\Dockerfile）。
    ベースイメージ（windows/server）のタグはホストのビルドに合わせて自動で選び、ビルド引数 BASE_TAG で渡します。
  - カレントディレクトリは、C: ならそのままのパス、ほかのドライブなら C:\mnt\<ドライブ>\... にマウントします。
  - ユーザーは ContainerAdministrator です。
  - --session・koya attach --win は、tmux の代わりに psmux（base に同梱）を使います。
  - 永続ホーム（--home）は C:\Users\<ホストのユーザー名> にマウントし、USERPROFILE・HOME・APPDATA・
    LOCALAPPDATA とユーザーのフォルダの設定をそこに向けます。置き場所は設定の home.win.dir
    （既定 <home.linux.dir>-win、つまり %LOCALAPPDATA%\koya\home-win）。Windows の API でホームそのものをたずねるツールや、
    レジストリ（HKCU）に書かれる設定は残りません。
  - プロファイルを省略したときは、環境変数 KOYA_WIN_PROFILE、なければ base です（.koya は見ません）。

koya win status      デーモンとホストの状態を表示する
koya win setup       デーモンを入れて起動する（koya run --win でも自動で行います）
koya win uninstall   サービスの登録を外し、dockerd.exe などを消す（イメージとコンテナは残します）

環境変数:
  KOYA_WIN_PROFILE    --win のときの既定のプロファイル
  KOYA_WIN_BASE_TAG   ベースイメージのタグを指定する（ltsc2022 など）
  KOYA_WIN_ISOLATION  分離の方式を指定する（process / hyperv）
`

func main() {
	selfupdate.CleanupOld()
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
	notifyUpdate(cmd, rest)
	switch cmd {
	case "run":
		return cmdRun(rest)
	case "attach":
		return cmdAttach(rest)
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
	case "self-update":
		return cmdSelfUpdate(rest)
	case "win":
		return cmdWin(rest)
	case "version", "--version":
		fmt.Println("koya", getVersion())
		return nil
	case "help", "-h", "--help":
		if len(rest) > 0 && rest[0] == "run" {
			fmt.Print(runUsage)
		} else if len(rest) > 0 && rest[0] == "config" {
			fmt.Print(configUsage)
		} else if len(rest) > 0 && rest[0] == "win" {
			fmt.Print(winUsage)
		} else {
			fmt.Print(usage)
		}
		return nil
	default:
		return fmt.Errorf("%q というコマンドはありません（koya help で一覧を表示）", cmd)
	}
}

// notifyUpdate は、新しい版が出ていれば stderr にお知らせする。
// GitHub への問い合わせは selfupdate.CheckInterval ごと（1 日に数回）で、それ以外は前回の結果を使う。
// 端末でないとき（スクリプトから呼ばれたとき）や、KOYA_NO_UPDATE_CHECK が設定されているときは何もしない。
func notifyUpdate(cmd string, rest []string) {
	switch cmd {
	case "self-update", "version", "--version", "help", "-h", "--help":
		return
	case "win":
		// 管理者権限で自分自身を実行するとき（_install など）は対象外
		if len(rest) > 0 && strings.HasPrefix(rest[0], "_") {
			return
		}
	}
	if os.Getenv("KOYA_NO_UPDATE_CHECK") != "" || !isTTY(os.Stderr) {
		return
	}
	if msg := selfupdate.Notice(getVersion(), selfupdate.StatePath()); msg != "" {
		fmt.Fprintln(os.Stderr, "koya:", msg)
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
	var rm, home, noHome, noSession, win, linux, gpu, noGPU bool
	var homeScope string
	fs.StringVar(&opts.Net, "net", "", "")
	fs.BoolVar(&opts.ReadOnly, "ro", false, "")
	fs.Var(&env, "e", "")
	fs.Var(&ports, "p", "")
	fs.Var(&vols, "v", "")
	fs.BoolVar(&rm, "rm", false, "")
	fs.BoolVar(&home, "home", false, "")
	fs.BoolVar(&noHome, "no-home", false, "")
	fs.StringVar(&homeScope, "home-scope", "", "")
	fs.BoolVar(&gpu, "gpu", false, "")
	fs.BoolVar(&noGPU, "no-gpu", false, "")
	fs.BoolVar(&win, "win", false, "")
	fs.BoolVar(&linux, "linux", false, "")
	var session sessionFlag
	fs.Var(&session, "session", "")
	fs.BoolVar(&noSession, "no-session", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, runUsage)
	}
	if len(pos) > 1 {
		return fmt.Errorf("プロファイルは1つだけ指定できます（コマンドは -- の後ろに書きます）: %s", strings.Join(pos, " "))
	}
	if session.enabled && noSession {
		return errors.New("--session と --no-session は同時に指定できません")
	}
	if home && noHome {
		return errors.New("--home と --no-home は同時に指定できません")
	}
	if gpu && noGPU {
		return errors.New("--gpu と --no-gpu は同時に指定できません")
	}
	if gpu && win {
		return errors.New("--gpu は --win（Windows コンテナ）では使えません")
	}
	if homeScope != "" && homeScope != "shared" && homeScope != "project" {
		return errors.New("--home-scope は shared か project です")
	}
	opts.Env, opts.Ports, opts.Volumes = env, ports, vols
	optsGiven := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name != "rm" && f.Name != "session" && f.Name != "no-session" && f.Name != "win" && f.Name != "linux" {
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
	if err := checkTarget(win, linux); err != nil {
		return err
	}
	prof := ""
	switch {
	case len(pos) == 1:
		prof = pos[0]
	case win:
		prof = defaultWinProfile()
	default:
		prof = defaultProfile(wd)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	user, err := hostUser()
	if err != nil {
		return err
	}
	if win {
		user = container.WinUser(hostUsername())
	}

	var rt *runtime.Runtime
	var store *profile.Store
	isolation := ""
	if win {
		var base winengine.Base
		if rt, base, err = winengine.Ensure(); err != nil {
			return err
		}
		store = winStore(base)
		isolation = base.Isolation
	} else {
		if rt, err = runtime.Detect(); err != nil {
			return err
		}
		store = profile.NewStore(koya.Profiles)
	}
	if err := store.Ensure(rt, prof, false); err != nil {
		return err
	}
	hash, err := store.Hash(prof)
	if err != nil {
		return err
	}

	// 永続ホーム: --home / --no-home → 設定ファイル → プロファイルの LABEL koya.home の順に決める
	useHome := cfg.HomeEnabled(prof) || profile.ImageLabel(rt, prof, "koya.home") == "true"
	if home {
		useHome = true
	} else if noHome {
		useHome = false
	}
	if useHome {
		scope := cfg.HomeScope
		if homeScope != "" {
			scope = homeScope
		}
		root := cfg.HomeRoot(win)
		if root == "" {
			key := "home.linux.dir"
			if win {
				key = "home.win.dir"
			}
			return fmt.Errorf("永続ホームの置き場所が決められません（設定ファイルに %s を書いてください）", key)
		}
		dir := filepath.Join(root, prof)
		if scope == "project" {
			dir = filepath.Join(root, container.Name(prof, wd))
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("永続ホーム %s を作れません: %w", dir, err)
		}
		if win {
			// Windows コンテナはマウント元がないと起動しないため、差し替える先のフォルダを先に作る
			subs := append([]string(nil), container.WinHomeDirs...)
			if rel := container.WinHomeMountpoint(user.Home, wd); rel != "" {
				subs = append(subs, rel)
			}
			for _, sub := range subs {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
					return fmt.Errorf("永続ホーム %s を作れません: %w", dir, err)
				}
			}
		}
		opts.Home = dir
	}

	name := container.Name(prof, wd)
	info, exists := container.Inspect(rt, name)

	// GPU: --gpu / --no-gpu → 設定ファイルの gpu（auto / on / off）の順に決める。
	// デバイスはコンテナを作るときだけ探す。既存のコンテナは作成時の設定に従う
	gpuMode := cfg.GPU
	if gpu {
		gpuMode = "on"
	} else if noGPU || win {
		gpuMode = "off"
	}
	var gpuDev *container.GPU
	if exists && !rm {
		opts.GPU = gpuMode == "on" || (gpuMode == "auto" && container.GPUOf(info.Opts))
	} else if gpuMode != "off" {
		if gpuDev, err = detectGPU(gpuMode); err != nil {
			return err
		}
		opts.GPU = gpuDev != nil
		if gpuDev != nil {
			fmt.Fprintf(os.Stderr, "koya: GPU（%s）を渡します\n", strings.Join(gpuDev.Devices, ", "))
		}
	}

	spec := container.Spec{Profile: prof, Workdir: wd, Hash: hash, Opts: opts, User: user, GPU: gpuDev, Isolation: isolation}
	tty := isTTY(os.Stdin) && isTTY(os.Stdout)
	// セッション: --session / --no-session → プロファイルの LABEL koya.session の順に決める。
	// ラベルで有効になる場合は、使えない状況（--rm・端末なし）では黙ってセッションなしで動かす。
	// 既存のコンテナがイメージより古いときも使わない（ラベルは新しいイメージのもので、
	// 古いコンテナには tmux・psmux がないことがあるため）。
	stale := exists && info.Hash != "" && info.Hash != hash
	if !session.set && !noSession && !rm && tty && !stale &&
		profile.ImageLabel(rt, prof, "koya.session") == "true" {
		session.enabled = true
	}
	if session.enabled {
		if rm {
			return errors.New("--session と --rm は同時に指定できません")
		}
		if !tty {
			return errors.New("--session は端末から実行してください（ssh なら -t を付けてください）")
		}
		command = container.SessionCommand(win, container.SessionName(session.name, command), command)
	}
	if len(command) == 0 {
		command = container.DefaultShell
		if win {
			command = container.WinShell(tty)
		}
	}

	if rm {
		return runOneshot(rt, spec, tty, command)
	}

	if !exists {
		fmt.Fprintf(os.Stderr, "koya: コンテナ %s を作成します\n", name)
		if opts.Home != "" {
			fmt.Fprintf(os.Stderr, "koya: 永続ホーム %s を %s にマウントします\n", opts.Home, user.Home)
		}
		if err := container.Create(rt, name, spec); err != nil {
			return err
		}
	} else {
		if (optsGiven && info.Opts != opts.Key()) || container.HomeOf(info.Opts) != opts.Home ||
			container.GPUOf(info.Opts) != opts.GPU {
			created := info.Opts
			if created == "" {
				created = "オプションなし"
			}
			fmt.Fprintf(os.Stderr, "koya: 警告: 既存のコンテナは別のオプションで作られているため、今回のオプションは効きません（作成時: %s）。\n"+
				"      反映するには koya rm -f %s で削除してから作り直してください。\n", created, prof)
		}
		if stale {
			fmt.Fprintf(os.Stderr, "koya: イメージが更新されています。反映するには koya rm -f %s で削除してから作り直してください。\n", prof)
		}
		if !info.Running() {
			if err := rt.Quiet("start", name); err != nil {
				return err
			}
		}
	}
	return rt.Exec(container.ExecArgs(rt, name, wd, tty, command)...)
}

// detectGPU は GPU のデバイスを探す。mode は auto か on。
// auto では、GPU がない・今のユーザーで開けないときは nil を返す（エラーにしない）。
func detectGPU(mode string) (*container.GPU, error) {
	g, err := container.DetectGPU()
	switch {
	case err != nil && mode == "on":
		return nil, err
	case err != nil:
		return nil, nil
	case len(g.Denied) > 0 && mode == "auto":
		fmt.Fprintf(os.Stderr, "koya: GPU がありますが、今のユーザーでは %s を開けないため使いません"+
			"（render・video グループに入るか、--gpu で強制できます）\n", strings.Join(g.Denied, ", "))
		return nil, nil
	case len(g.Denied) > 0:
		fmt.Fprintf(os.Stderr, "koya: 警告: 今のユーザーでは %s を開けません。コンテナ内でも GPU を使えない可能性があります"+
			"（sudo usermod -aG render,video $USER でグループに入り、ログインし直してください）\n", strings.Join(g.Denied, ", "))
	}
	return g, nil
}

// runOneshot は使い捨てのコンテナを作ってコマンドを実行し、終わったら削除する。
func runOneshot(rt *runtime.Runtime, spec container.Spec, tty bool, command []string) error {
	name := container.OneshotName(spec.Profile, spec.Workdir)
	if err := container.Create(rt, name, spec, container.LabelOneshot+"=true"); err != nil {
		return err
	}
	defer func() { _ = rt.Quiet("rm", "-f", name) }()
	// Ctrl-C はコンテナ内のプロセスに届くので、koya 自身は終了しないようにする
	signal.Ignore(os.Interrupt)
	cmd := rt.Command(container.ExecArgs(rt, name, spec.Workdir, tty, command)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return exitError(ee.ExitCode())
		}
		return err
	}
	return nil
}

// hostUser は koya を実行したホストのユーザーを返す。
func hostUser() (container.User, error) {
	u, err := user.Current()
	if err != nil {
		return container.User{}, fmt.Errorf("ユーザー情報を取得できません: %w", err)
	}
	if goruntime.GOOS == "windows" {
		return windowsUser(u.Username), nil
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return container.User{}, fmt.Errorf("UID/GID が数値ではありません: %s/%s", u.Uid, u.Gid)
	}
	home := u.HomeDir
	if home == "" || home == "/" {
		home = "/home/" + u.Username
	}
	return container.User{Name: u.Username, UID: uid, GID: gid, Home: home}, nil
}

// hostUsername はホストのユーザー名をそのまま返す（Windows では DOMAIN\name の形）。
func hostUsername() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USERNAME")
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
	if filepath.Dir(wd) == wd {
		return fmt.Errorf("%s（ドライブやファイルシステムの最上位）はマウントできません。プロジェクトのディレクトリで実行してください", wd)
	}
	if goruntime.GOOS == "windows" && strings.HasPrefix(wd, `\\`) {
		return errors.New("ネットワーク上のパス（\\\\server\\share）はマウントできません。ローカルのドライブで実行してください")
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
	const help = "使い方: koya ls [-a] [--linux | --win]\n\n  -a        全ディレクトリのコンテナを表示する\n  --linux   Linux コンテナを表示する（既定）\n  --win     Windows コンテナを表示する\n"
	fs := newFlagSet("ls")
	all := fs.Bool("a", false, "")
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	if _, err := parseInterspersed(fs, args); err != nil {
		return flagErr(err, help)
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	rt, err := detect(*win)
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
		fmt.Fprintln(tw, "PROFILE\tSTATUS\tSESSIONS\tWORKDIR\tNAME")
	} else {
		fmt.Fprintln(tw, "PROFILE\tSTATUS\tSESSIONS\tNAME")
	}
	for _, i := range infos {
		sess := "-"
		if i.Running() {
			var names []string
			for _, ss := range container.Sessions(rt, i.Name) {
				n := ss.Name
				if ss.Attached {
					n += "*"
				}
				names = append(names, n)
			}
			if len(names) > 0 {
				sess = strings.Join(names, ",")
			}
		}
		if *all {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", i.Profile, i.Status, sess, i.Workdir, i.Name)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", i.Profile, i.Status, sess, i.Name)
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
	const help = "使い方: koya stop [--linux | --win] [プロファイル...]\n"
	fs := newFlagSet("stop")
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	rt, err := detect(*win)
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
	const help = "使い方: koya rm [-f] [--linux | --win] [プロファイル...]\n\n  -f        起動中でも削除する\n  --linux   Linux コンテナを対象にする（既定）\n  --win     Windows コンテナを対象にする\n"
	fs := newFlagSet("rm")
	force := fs.Bool("f", false, "")
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	rt, err := detect(*win)
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
	const help = "使い方: koya profiles [--linux | --win]\n"
	fs := newFlagSet("profiles")
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	if len(pos) > 0 {
		return errors.New(help)
	}
	var store *profile.Store
	var rt *runtime.Runtime
	var rtErr error
	if *win {
		base, err := winengine.HostBase()
		if err != nil {
			return err
		}
		store = winStore(base)
		rt, rtErr = winengine.Runtime()
	} else {
		store = profile.NewStore(koya.Profiles)
		rt, rtErr = runtime.Detect()
	}
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
	fmt.Fprintf(os.Stderr, "\nユーザー定義プロファイルの置き場所: %s/<名前>/Dockerfile\n", store.UserDir())
	return nil
}

func cmdBuild(args []string) error {
	const help = "使い方: koya build [--linux | --win] [プロファイル...]\n"
	fs := newFlagSet("build")
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	var rt *runtime.Runtime
	var store *profile.Store
	if *win {
		var base winengine.Base
		if rt, base, err = winengine.Ensure(); err != nil {
			return err
		}
		store = winStore(base)
	} else {
		if rt, err = runtime.Detect(); err != nil {
			return err
		}
		store = profile.NewStore(koya.Profiles)
	}
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

// windowsUser は Windows のユーザーから、コンテナ内で使うユーザーを作る。
// Windows には UID/GID がないため 1000:1000 を使い、名前は Linux で使える形に直す
// （例: DOMAIN\Taro.Yamada → taro.yamada、ホームは /home/taro.yamada）。
func windowsUser(username string) container.User {
	if i := strings.LastIndex(username, `\`); i >= 0 {
		username = username[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(username) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := b.String()
	if strings.Trim(name, "_-.") == "" {
		// 日本語の名前など、使える文字が残らない場合
		name = "user"
	}
	if !(name[0] >= 'a' && name[0] <= 'z' || name[0] == '_') {
		name = "u" + name
	}
	if len(name) > 32 {
		name = name[:32]
	}
	return container.User{Name: name, UID: 1000, GID: 1000, Home: "/home/" + name}
}

// sessionFlag は --session（名前なし）と --session=名前 の両方を受け付けるフラグ。
// set は --session / --session=… がコマンドラインで指定されたかどうか（--session=false を含む）。
type sessionFlag struct {
	enabled bool
	name    string
	set     bool
}

func (f *sessionFlag) String() string   { return f.name }
func (f *sessionFlag) IsBoolFlag() bool { return true }
func (f *sessionFlag) Set(v string) error {
	f.set = true
	switch v {
	case "true":
		f.enabled, f.name = true, ""
	case "false":
		f.enabled, f.name = false, ""
	default:
		f.enabled, f.name = true, v
	}
	return nil
}

func cmdAttach(args []string) error {
	const help = "使い方: koya attach [--win] [プロファイル] [セッション]\n\n" +
		"このディレクトリのコンテナのセッションにつなぎ直します。\n" +
		"セッションを省略したときは、セッションが1つだけならそれにつなぎます。\n" +
		"--win を付けると Windows コンテナのセッションにつなぎます。\n"
	fs := newFlagSet("attach")
	win := fs.Bool("win", false, "")
	linux := fs.Bool("linux", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	if len(pos) > 2 {
		return errors.New(help)
	}
	wd, err := workdir()
	if err != nil {
		return err
	}
	if err := checkTarget(*win, *linux); err != nil {
		return err
	}
	prof := defaultProfile(wd)
	if *win {
		prof = defaultWinProfile()
	}
	if len(pos) >= 1 {
		prof = pos[0]
	}
	rt, err := detect(*win)
	if err != nil {
		return err
	}
	name := container.Name(prof, wd)
	info, exists := container.Inspect(rt, name)
	if !exists || !info.Running() {
		return fmt.Errorf("このディレクトリに起動中の %s のコンテナはありません（koya ls で確認できます）", prof)
	}
	if !(isTTY(os.Stdin) && isTTY(os.Stdout)) {
		return errors.New("koya attach は端末から実行してください（ssh なら -t を付けてください）")
	}
	sess := ""
	if len(pos) == 2 {
		sess = pos[1]
	} else {
		ss := container.Sessions(rt, name)
		switch len(ss) {
		case 0:
			return fmt.Errorf("%s のコンテナにセッションはありません", prof)
		case 1:
			sess = ss[0].Name
		default:
			var names []string
			for _, x := range ss {
				names = append(names, x.Name)
			}
			return fmt.Errorf("セッションが複数あります。名前を指定してください: %s", strings.Join(names, ", "))
		}
	}
	return rt.Exec(container.ExecArgs(rt, name, wd, true, container.AttachCommand(rt.Windows, sess))...)
}

func cmdSelfUpdate(args []string) error {
	const help = "使い方: koya self-update [--check] [--force]\n\n" +
		"GitHub Releases から koya の最新版を取ってきて、今の実行ファイルと入れ替えます。\n\n" +
		"  --check   新しい版があるかを確かめるだけで、入れ替えない\n" +
		"  --force   最新版と同じ版や、版が分からない（開発版の）場合も入れ替える\n"
	fs := newFlagSet("self-update")
	check := fs.Bool("check", false, "")
	force := fs.Bool("force", false, "")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return flagErr(err, help)
	}
	if len(pos) > 0 {
		return errors.New(help)
	}
	cur := getVersion()
	rel, err := selfupdate.Latest()
	if err != nil {
		return err
	}
	cmp, known := selfupdate.Compare(cur, rel.Tag)
	newer := known && cmp < 0
	if *check {
		switch {
		case !known:
			fmt.Printf("今の版: %s（開発版のため比べられません）、最新版: %s\n", cur, rel.Tag)
		case newer:
			fmt.Printf("新しい版があります: %s → %s（koya self-update で更新できます）\n", cur, rel.Tag)
		default:
			fmt.Printf("最新です（%s）\n", cur)
		}
		return nil
	}
	if !*force {
		if !known {
			return fmt.Errorf("今の koya の版（%s）が分からないため、更新するか判断できません。最新版 %s に入れ替えるなら --force を付けてください", cur, rel.Tag)
		}
		if !newer {
			fmt.Printf("最新です（%s）\n", cur)
			return nil
		}
	}
	exe, err := selfupdate.Executable()
	if err != nil {
		return fmt.Errorf("今の実行ファイルの場所が分かりません: %w", err)
	}
	fmt.Fprintf(os.Stderr, "koya: %s を取ってきます\n", rel.Tag)
	bin, err := selfupdate.Download(rel)
	if err != nil {
		return err
	}
	if err := selfupdate.Replace(exe, bin); err != nil {
		return err
	}
	fmt.Printf("koya を更新しました: %s → %s（%s）\n", cur, rel.Tag, exe)
	return nil
}

// detect はランタイムを返す。win なら koya 専用の Windows コンテナ用デーモンにつなぐ。
func detect(win bool) (*runtime.Runtime, error) {
	if win {
		return winengine.Runtime()
	}
	return runtime.Detect()
}

// winStore は Windows コンテナ用のプロファイルの Store を、ホストに合わせたビルド引数付きで作る。
func winStore(base winengine.Base) *profile.Store {
	s := profile.NewWinStore(koya.Profiles)
	s.BuildArgs = []string{"BASE_TAG=" + base.Tag}
	s.BuildFlags = []string{"--isolation", base.Isolation}
	return s
}

// defaultWinProfile は --win のときの既定のプロファイル（KOYA_WIN_PROFILE、なければ base）。
func defaultWinProfile() string {
	if p := os.Getenv("KOYA_WIN_PROFILE"); p != "" {
		return p
	}
	return "base"
}

// cmdWin は Windows コンテナ用のデーモンを管理する。
// _install・_start・_uninstall は、koya が管理者権限で自分自身を実行するときに使う（--log に出力を書く）。
func cmdWin(args []string) (err error) {
	if len(args) == 0 {
		fmt.Print(winUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	fs := newFlagSet("win " + sub)
	logPath := fs.String("log", "", "")
	user := fs.String("user", "", "")
	pos, err := parseInterspersed(fs, rest)
	if err != nil {
		return flagErr(err, winUsage)
	}
	if len(pos) > 0 {
		return errors.New(winUsage)
	}
	if *logPath != "" {
		// 管理者権限で実行されたとき: 出力とエラーをログに書き、呼び出し元の koya に表示させる
		f, err := os.OpenFile(*logPath, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		os.Stderr = f
		defer func() {
			if err != nil {
				fmt.Fprintln(f, "koya:", err)
			}
		}()
	}
	switch sub {
	case "status":
		fmt.Print(winengine.Status())
	case "setup":
		if _, _, err = winengine.Ensure(); err == nil {
			fmt.Fprintln(os.Stderr, "koya: Windows コンテナ用のデーモンを使えます")
		}
	case "uninstall":
		err = winengine.RequestUninstall()
	case "_install":
		err = winengine.Install(*user)
	case "_start":
		err = winengine.Start()
	case "_uninstall":
		err = winengine.Uninstall()
	default:
		err = fmt.Errorf("koya win %s というコマンドはありません（koya help win）", sub)
	}
	return err
}

// checkTarget は --win と --linux が同時に指定されていないかを確かめる。
// どちらもなければ --linux（Linux コンテナ）として扱う。
func checkTarget(win, linux bool) error {
	if win && linux {
		return errors.New("--win と --linux は同時に指定できません")
	}
	return nil
}
