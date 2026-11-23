// Package container はコンテナの名前付け、作成、一覧などを扱う。
package container

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	LabelOneshot = "koya.oneshot"
)

// DefaultShell はシェルを起動するコマンド。標準入力が端末なら対話シェル、
// パイプならスクリプトとして読む。bash がないイメージでも動くようにする。
var DefaultShell = []string{"sh", "-c",
	`if [ ! -t 0 ]; then if command -v bash >/dev/null 2>&1; then exec bash -s; else exec sh -s; fi; fi; ` +
		`if [ -f /etc/koya/bashrc ] && command -v bash >/dev/null 2>&1; then exec bash --rcfile /etc/koya/bashrc -i; ` +
		`elif command -v bash >/dev/null 2>&1; then exec bash -i; else exec sh -i; fi`}

// WinShell は Windows コンテナでシェルを起動するコマンド。
// PowerShell 7（pwsh.exe）があればそれを、なければ Windows PowerShell を使う。
// 標準入力が端末なら対話シェル、パイプならスクリプトとして読む。
func WinShell(tty bool) []string {
	args := "-NoLogo"
	if !tty {
		args = "-NoLogo -NoProfile -Command -"
	}
	return []string{"cmd.exe", "/d", "/c",
		"where /q pwsh.exe & if errorlevel 1 (powershell.exe " + args + ") else (pwsh.exe " + args + ")"}
}

// winKeepAlive は Windows コンテナを常駐させるコマンド（sleep infinity の代わり）。
var winKeepAlive = []string{"-NoLogo", "-NoProfile", "-Command", "while ($true) { Start-Sleep -Seconds 3600 }"}

// Options はコンテナ作成時の設定。
type Options struct {
	Net      string
	ReadOnly bool
	Env      []string
	Ports    []string
	Volumes  []string
	Home     string // 永続ホームとしてマウントするホストのディレクトリ（空なら使わない）
	PkgCache string // パッケージのキャッシュとしてマウントするホストのディレクトリ（空なら使わない。Linux コンテナのみ）
	GPU      bool   // GPU を使えるようにする（Linux コンテナのみ）
	HostRT   bool   // ホストのランタイム（docker / podman）をコンテナ内から使えるようにする（Linux コンテナのみ）
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
	if o.Home != "" {
		parts = append(parts, "home="+o.Home)
	}
	if o.PkgCache != "" {
		parts = append(parts, "pkg-cache="+o.PkgCache)
	}
	if o.GPU {
		parts = append(parts, "gpu")
	}
	if o.HostRT {
		parts = append(parts, "dood")
	}
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

// HomeOf は Key() の文字列から永続ホームの設定を取り出す。
func HomeOf(key string) string {
	for _, p := range strings.Split(key, ";") {
		if strings.HasPrefix(p, "home=") {
			return strings.TrimPrefix(p, "home=")
		}
	}
	return ""
}

// パッケージのキャッシュ
//
// uv・pip・npm・Go などのパッケージマネージャーのキャッシュを、プロファイルごとのディレクトリ
// （<pkg-cache.linux.dir>/<プロファイル>）に置き、コンテナ内の PkgCacheMount にマウントする。
// 全プロジェクト・使い捨てのコンテナ（--rm）で共有し、koya rm でも消さない。
// 永続ホームと同じくホストのディレクトリにするので、共有のディスクに置けばほかのホストとも共有できる。

// PkgCacheMount はコンテナ内のパッケージのキャッシュの置き場所。
const PkgCacheMount = "/var/cache/koya"

// pkgCacheEnv は、各種ツールのキャッシュを PkgCacheMount に向ける環境変数。
// XDG_CACHE_HOME で uv・pip・Go のビルドキャッシュ・corepack など多くのツールが向く。
// それを見ないものは個別に指定する。
func pkgCacheEnv() []string {
	return []string{
		"XDG_CACHE_HOME=" + PkgCacheMount,
		"npm_config_cache=" + PkgCacheMount + "/npm", // npm は ~/.npm を使う
		"GOMODCACHE=" + PkgCacheMount + "/go-mod",    // Go のモジュールは $GOPATH/pkg/mod を使う
		"UV_LINK_MODE=copy",                          // キャッシュと venv は別のマウントでハードリンクできないので、その警告を出さない
	}
}

// dnf のキャッシュ
//
// dnf は root で動き、キャッシュの置き場所を環境変数では変えられない（設定の system_cachedir で決まる。既定は /var/cache/libdnf5）。
// そこで、コンテナの作成時に dnf5 の設定の追加ファイル（drop-in）を書き、キャッシュを PkgCacheMount の下に向ける。
// 既定ではインストールの後に rpm を消すので、keepcache=True で残す（同じ版をもう一度入れるときにダウンロードしない）。
// dnf5 の追加ファイルのディレクトリ（libdnf5 が作る）がないイメージ（dnf4 や dnf のないイメージ）では何もしない。

// DNFCacheDir はコンテナ内の dnf のキャッシュの置き場所。
const DNFCacheDir = PkgCacheMount + "/dnf"

// dnfConfDir は dnf5 の設定の追加ファイルのディレクトリ。
const dnfConfDir = "/etc/dnf/libdnf5.conf.d"

// dnfCacheScript は dnf5 のキャッシュの設定を書く（root で実行する）。
// 引数: 追加ファイルのディレクトリ キャッシュの置き場所
const dnfCacheScript = `
confdir=$1; cachedir=$2
[ -d "$confdir" ] || exit 0
printf '[main]\nsystem_cachedir=%s\nkeepcache=True\n' "$cachedir" > "$confdir/90-koya-pkg-cache.conf"
`

// setupDNFCache は dnf のキャッシュを DNFCacheDir に向ける。
func setupDNFCache(rt *runtime.Runtime, name string) error {
	if err := rt.Quiet("exec", "-u", "0", name, "sh", "-c", dnfCacheScript, "koya-dnf-cache", dnfConfDir, DNFCacheDir); err != nil {
		return fmt.Errorf("dnf のキャッシュの設定に失敗しました: %w", err)
	}
	return nil
}

// GPUOf は Key() の文字列から GPU を使うかを取り出す。
func GPUOf(key string) bool {
	for _, p := range strings.Split(key, ";") {
		if p == "gpu" {
			return true
		}
	}
	return false
}

// User はコンテナ内で使うユーザー（koya を実行したホストのユーザー）。
type User struct {
	Name string
	UID  int
	GID  int
	Home string // コンテナ内のホームディレクトリ（Linux / macOS ではホストと同じパス）
}

// Spec はコンテナ作成に必要な情報。
type Spec struct {
	Profile string
	Workdir string
	Hash    string
	Opts    Options
	User    User
	GPU     *GPU         // Opts.GPU のときに渡すデバイス（DetectGPU で探す）
	HostRT  *HostRuntime // Opts.HostRT のときに渡すソケット（DetectHostRuntime で探す）

	Isolation string // Windows コンテナの分離の方式（process / hyperv）。Linux コンテナでは空
}

// RunArgs は run コマンドの共通引数（イメージ名の手前まで）を返す。
func RunArgs(rt *runtime.Runtime, s Spec) []string {
	if rt.Windows {
		return winRunArgs(s)
	}
	ctrWorkdir := ContainerPath(s.Workdir)
	mount := s.Workdir + ":" + ctrWorkdir + mountSuffix(s.Opts.ReadOnly)
	args := []string{
		"--label", LabelManaged + "=true",
		"--label", LabelProfile + "=" + s.Profile,
		"--label", LabelWorkdir + "=" + s.Workdir,
		"--label", LabelHash + "=" + s.Hash,
		"--label", LabelOpts + "=" + s.Opts.Key(),
		"--hostname", "koya-" + s.Profile,
		"-e", "KOYA_PROFILE=" + s.Profile,
		"-e", "HOME=" + s.User.Home,
		"-e", "USER=" + s.User.Name,
		"-e", "LOGNAME=" + s.User.Name,
	}
	// ホームを先にマウントし、作業ディレクトリがホームの下にあっても上から重ねられるようにする
	if s.Opts.Home != "" {
		args = append(args, "-v", s.Opts.Home+":"+s.User.Home+mountSuffix(false))
	}
	args = append(args, "-v", mount, "-w", ctrWorkdir)
	// キャッシュの環境変数は -e より前に置き、利用者が -e で上書きできるようにする
	if s.Opts.PkgCache != "" {
		args = append(args, "-v", s.Opts.PkgCache+":"+PkgCacheMount+mountSuffix(false))
		for _, e := range pkgCacheEnv() {
			args = append(args, "-e", e)
		}
	}
	args = append(args, userArgs(rt, s.User)...)
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
	if s.Opts.GPU {
		args = append(args, gpuArgs(rt, s.User, s.GPU)...)
	}
	if s.Opts.HostRT {
		args = append(args, hostRuntimeArgs(s.HostRT)...)
	}
	return args
}

// Windows コンテナの永続ホーム
//
// Windows コンテナでは、中身のあるディレクトリ（C:\Users\ContainerAdministrator など）にはマウントできない。
// また、コンテナ内にホストと同じ名前のユーザーを作っても、ホストのユーザーとは別人として扱われ
// （プロジェクトに書き込めない）、ホームも別の場所（<名前>.<ホスト名>）に作られてしまう。
// そこで、ユーザーは ContainerAdministrator のまま、イメージにない C:\Users\<ホストのユーザー名> に
// 永続ホームをマウントし、環境変数とユーザーのフォルダの設定（User Shell Folders）でホームをそこに向ける。

// WinHomeDirs は永続ホームの中に、マウントする前に作っておくディレクトリ。
var WinHomeDirs = []string{`AppData\Roaming`, `AppData\Local`, "Documents"}

// winReserved はイメージに元からある C:\Users の下の名前（ここにはマウントできない）。
var winReserved = []string{"administrator", "containeradministrator", "containeruser", "public", "default", "default user", "all users"}

// WinUser は Windows コンテナで永続ホームに使うユーザー（名前とホームのパス）を返す。
// username は DOMAIN\name の形でもよい。
func WinUser(username string) User {
	if i := strings.LastIndex(username, `\`); i >= 0 {
		username = username[i+1:]
	}
	name := strings.Trim(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, username), " .")
	if name == "" {
		name = "user"
	}
	for _, r := range winReserved {
		if strings.EqualFold(name, r) {
			name = "koya-" + name
			break
		}
	}
	return User{Name: name, Home: `C:\Users\` + name}
}

// WinHomeMountpoint は、作業ディレクトリが永続ホームの下にあるとき、永続ホームの中に作っておくべき
// ディレクトリ（永続ホームからの相対パス）を返す。下になければ空文字。
// Windows コンテナでは、マウントの内側に別のマウントを重ねても途中のディレクトリが作られないため、
// 途中のディレクトリ（例: C:\Users\me\src）を lstat すると「存在しない」になる（Node の fs.realpathSync が失敗する）。
// Linux の Docker と同じように、マウント先のディレクトリを永続ホームの中に作っておく。
func WinHomeMountpoint(home, workdir string) string {
	ctr := WinContainerPath(workdir)
	prefix := strings.TrimRight(home, `\`) + `\`
	if len(ctr) <= len(prefix) || !strings.EqualFold(ctr[:len(prefix)], prefix) {
		return ""
	}
	return ctr[len(prefix):]
}

// winHomeEnv は永続ホームを使うときに、ホームの場所を差し替える環境変数。
func winHomeEnv(home string) []string {
	return []string{
		"USERPROFILE=" + home,
		"HOME=" + home,
		`APPDATA=` + home + `\AppData\Roaming`,
		`LOCALAPPDATA=` + home + `\AppData\Local`,
	}
}

// winShellFolders は、Windows の API（SHGetKnownFolderPath）でフォルダの場所をたずねるツールのために、
// ContainerAdministrator の User Shell Folders を永続ホームに向けるコマンドを返す（コンテナ作成時に実行する）。
// cmd.exe を通さず reg.exe に引数を直接渡す（引用符の受け渡しで崩れないように）。
// ホームそのもの（FOLDERID_Profile）は差し替えられない。
func winShellFolders(home string) [][]string {
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`
	var cmds [][]string
	for _, f := range [][2]string{
		{"AppData", home + `\AppData\Roaming`},
		{"Local AppData", home + `\AppData\Local`},
		{"Personal", home + `\Documents`},
	} {
		cmds = append(cmds, []string{"reg.exe", "add", key, "/v", f[0], "/t", "REG_EXPAND_SZ", "/d", f[1], "/f"})
	}
	return cmds
}

// winRunArgs は Windows コンテナの run の共通引数。
// ユーザーはイメージの既定（ContainerAdministrator）のまま。永続ホームは s.User.Home にマウントする。
func winRunArgs(s Spec) []string {
	ctrWorkdir := WinContainerPath(s.Workdir)
	mount := s.Workdir + ":" + ctrWorkdir
	if s.Opts.ReadOnly {
		mount += ":ro"
	}
	args := []string{
		"--label", LabelManaged + "=true",
		"--label", LabelProfile + "=" + s.Profile,
		"--label", LabelWorkdir + "=" + s.Workdir,
		"--label", LabelHash + "=" + s.Hash,
		"--label", LabelOpts + "=" + s.Opts.Key(),
		"--hostname", "koya-" + s.Profile,
		"-e", "KOYA_PROFILE=" + s.Profile,
	}
	// ホームを先にマウントし、作業ディレクトリがホームの下にあっても上から重ねられるようにする
	if s.Opts.Home != "" {
		args = append(args, "-v", s.Opts.Home+":"+s.User.Home)
		for _, e := range winHomeEnv(s.User.Home) {
			args = append(args, "-e", e)
		}
	}
	args = append(args, "-v", mount, "-w", ctrWorkdir)
	if s.Isolation != "" {
		args = append(args, "--isolation", s.Isolation)
	}
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

// runsAsRoot は、ファイルの所有者をホストのユーザーに揃えるために
// コンテナ内で UID 0 として動かす必要があるかを返す。
// rootless のランタイムを docker CLI から使う場合、コンテナ内の root がホストのユーザーに対応する
// （docker CLI は --userns=keep-id を受け付けないため）。
func runsAsRoot(rt *runtime.Runtime, u User) bool {
	return u.UID == 0 || (rt.Rootless && rt.Kind != "podman")
}

// userArgs はマウントしたファイルの所有者をホストのユーザーのままにするための引数。
func userArgs(rt *runtime.Runtime, u User) []string {
	switch {
	case runsAsRoot(rt, u):
		return []string{"--user", "0:0"}
	case rt.Kind == "podman" && rt.Rootless:
		// ホストの UID/GID をコンテナ内でも同じ UID/GID に対応づける
		return []string{"--userns=keep-id", "--user", fmt.Sprintf("%d:%d", u.UID, u.GID)}
	default:
		return []string{"--user", fmt.Sprintf("%d:%d", u.UID, u.GID)}
	}
}

// setupScript はコンテナ内にホストのユーザーを登録する（root で実行する）。
// 引数: 名前 UID GID ホーム
// UID 0 で動かす場合は、同じ UID 0 の行を root より前に入れて、名前の解決でこのユーザーが返るようにする。
const setupScript = `
name=$1; uid=$2; gid=$3; home=$4
shell=/bin/sh
[ -x /bin/bash ] && shell=/bin/bash
line="$name:x:$uid:$gid:$name:$home:$shell"
tmp=$(mktemp)
{
  if [ "$uid" = 0 ]; then echo "$line"; fi
  while IFS= read -r l; do
    n=${l%%:*}; r=${l#*:}; r=${r#*:}; u=${r%%:*}
    if [ "$n" = "$name" ]; then continue; fi
    if [ "$u" = "$uid" ] && [ "$n" != root ]; then continue; fi
    echo "$l"
  done < /etc/passwd
  if [ "$uid" != 0 ]; then echo "$line"; fi
} > "$tmp"
cat "$tmp" > /etc/passwd
rm -f "$tmp"
if ! cut -d: -f3 /etc/group | grep -qx "$gid"; then echo "$name:x:$gid:" >> /etc/group; fi
if [ -f /etc/shadow ] && ! cut -d: -f1 /etc/shadow | grep -qx "$name"; then echo "$name:*:19000:0:99999:7:::" >> /etc/shadow; fi
if [ "$uid" != 0 ] && [ -d /etc/sudoers.d ]; then
  echo "$name ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/koya-user
  chmod 0440 /etc/sudoers.d/koya-user
fi
mkdir -p "$home"
chown "$uid:$gid" "$home" 2>/dev/null || true
`

// Setup はコンテナ内にホストのユーザーを登録する。
// コンテナ内で UID 0 として動かす場合は、UID/GID 0 でホストのユーザー名を登録する。
func Setup(rt *runtime.Runtime, name string, u User) error {
	uid, gid := u.UID, u.GID
	if runsAsRoot(rt, u) {
		uid, gid = 0, 0
	}
	err := rt.Quiet("exec", "-u", "0", name, "sh", "-c", setupScript, "koya-setup",
		u.Name, strconv.Itoa(uid), strconv.Itoa(gid), u.Home)
	if err != nil {
		return fmt.Errorf("コンテナ内のユーザーの設定に失敗しました: %w", err)
	}
	return nil
}

// Create は常駐用のコンテナを作って起動し、ユーザーを登録する。
func Create(rt *runtime.Runtime, name string, s Spec, labels ...string) error {
	if rt.Windows {
		return createWin(rt, name, s, labels)
	}
	args := []string{"run", "-d", "--init", "--name", name, "--entrypoint", "sleep"}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	args = append(args, RunArgs(rt, s)...)
	args = append(args, profile.ImageName(s.Profile), "infinity")
	if err := rt.Quiet(args...); err != nil {
		return err
	}
	if err := Setup(rt, name, s.User); err != nil {
		_ = rt.Quiet("rm", "-f", name)
		return err
	}
	// dnf のキャッシュは無くても困らないので、失敗しても警告だけにする
	if s.Opts.PkgCache != "" {
		if err := setupDNFCache(rt, name); err != nil {
			fmt.Fprintf(os.Stderr, "koya: 警告: %v\n", err)
		}
	}
	return nil
}

// createWin は Windows コンテナを作って起動する。ユーザーの登録はしない。
// 永続ホームを使う場合は、ユーザーのフォルダの設定を永続ホームに向ける。
func createWin(rt *runtime.Runtime, name string, s Spec, labels []string) error {
	args := []string{"run", "-d", "--name", name, "--entrypoint", "powershell.exe"}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	args = append(args, winRunArgs(s)...)
	args = append(args, profile.ImageName(s.Profile))
	if err := rt.Quiet(append(args, winKeepAlive...)...); err != nil {
		return err
	}
	if s.Opts.Home != "" {
		for _, c := range winShellFolders(s.User.Home) {
			if err := rt.Quiet(append([]string{"exec", name}, c...)...); err != nil {
				_ = rt.Quiet("rm", "-f", name)
				return fmt.Errorf("永続ホームのフォルダの設定に失敗しました: %w", err)
			}
		}
	}
	return nil
}

// ExecArgs は exec コマンドの引数を返す。workdir はホストのパス。
func ExecArgs(rt *runtime.Runtime, name, workdir string, tty bool, cmd []string) []string {
	args := []string{"exec", "-i"}
	if tty {
		args = append(args, "-t")
		if term := os.Getenv("TERM"); term != "" && !rt.Windows {
			args = append(args, "-e", "TERM="+term)
		}
	}
	args = append(args, "-w", containerPathFor(rt, workdir), name)
	return append(args, cmd...)
}

// OneshotName は使い捨てコンテナの名前を返す。
func OneshotName(prof, workdir string) string {
	return fmt.Sprintf("%s-tmp%d", Name(prof, workdir), os.Getpid())
}
