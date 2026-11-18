package container

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"unicode/utf16"

	"github.com/perorin0418/koya/internal/runtime"
)

// セッション機能: コンテナ内の tmux でコマンドを動かし、端末が切れても動き続けるようにする。
// もう一度つなぐと、同じ画面に戻る。
// Windows コンテナでは tmux の代わりに psmux（tmux と同じコマンドを受け付ける Windows 用のもの）を使う。

// sessionPrelude は tmux を使う前の共通処理（tmux の有無の確認と TERM の調整）。
const sessionPrelude = `
if ! command -v tmux >/dev/null 2>&1; then
  echo "koya: このイメージには tmux がないため、セッション機能を使えません（プロファイルに tmux を入れてください）" >&2
  exit 127
fi
# 手元の端末の種類（TERM）の定義がコンテナにないと tmux が起動できないので、一般的なものに置き換える
if ! tput -T "$TERM" longname >/dev/null 2>&1; then export TERM=xterm-256color; fi
conf=
[ -f /etc/koya/tmux.conf ] && conf="-f /etc/koya/tmux.conf"
name=$1; shift
`

// sessionNewScript はセッションがあればつなぎ、なければ作る。引数: 名前 [コマンド...]
// つなぐときは、ほかのクライアントを切り離す（-D）。端末が切れた後もコンテナ内に残っている
// 古いクライアントを片付けるため。
const sessionNewScript = sessionPrelude + `
if [ $# -eq 0 ]; then exec tmux $conf new-session -A -D -s "$name"; fi
exec tmux $conf new-session -A -D -s "$name" -- "$@"
`

// sessionAttachScript は既存のセッションにつなぐ。引数: 名前
const sessionAttachScript = sessionPrelude + `
if ! tmux has-session -t "=$name" 2>/dev/null; then
  echo "koya: セッション $name はありません" >&2
  exit 1
fi
exec tmux $conf attach-session -d -t "=$name"
`

// SessionCommand は、セッション name の中で cmd を動かす（既にあればつなぐ）コマンドを返す。
// cmd が空なら対話シェル。win なら Windows コンテナ用（psmux）。
func SessionCommand(win bool, name string, cmd []string) []string {
	if win {
		return winPwsh(winSessionPrelude + "$name = " + psQuote(name) + "\n$cmd = " + psArray(cmd) + "\n" + winSessionNewScript)
	}
	return append([]string{"sh", "-c", sessionNewScript, "koya-session", name}, cmd...)
}

// AttachCommand は既存のセッションにつなぐコマンドを返す。win なら Windows コンテナ用（psmux）。
func AttachCommand(win bool, name string) []string {
	if win {
		return winPwsh(winSessionPrelude + "$name = " + psQuote(name) + "\n" + winSessionAttachScript)
	}
	return []string{"sh", "-c", sessionAttachScript, "koya-session", name}
}

// Windows コンテナ用（psmux）のスクリプト。PowerShell 7（pwsh.exe）で動かす。
// 名前とコマンドは、スクリプトの中に PowerShell の文字列として埋め込み、-EncodedCommand で渡す。
// pwsh -File に引数で渡すと、- で始まる引数や -- が PowerShell の引数として解釈されてしまうため。

// winTmuxConf は base イメージに入れる psmux の設定ファイル。
const winTmuxConf = `C:\ProgramData\koya\tmux.conf`

// winSessionPrelude は psmux を使う前の共通処理（psmux の有無の確認と設定ファイル）。
const winSessionPrelude = `$exe = Get-Command psmux.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $exe) {
  [Console]::Error.WriteLine('koya: このコンテナには psmux がないため、セッション機能を使えません（koya rm -f --win で作り直すか、プロファイルを localhost/koya/base から作ってください）')
  exit 127
}
$exe = $exe.Source
$conf = @()
if (Test-Path -LiteralPath '` + winTmuxConf + `') { $conf = @('-f', '` + winTmuxConf + `') }
`

// winSessionNewScript はセッションがあればつなぎ、なければ作る。ほかのクライアントは切り離す（-D）。
// コマンドがなければ、koya のプロンプト付きの PowerShell 7 にする。
// psmux はコマンドを .exe として直接起動するため、npm の claude（claude.cmd・claude.ps1）などは起動できず、
// セッションがすぐ消える。.exe に見つからないコマンドは、PowerShell 7 を通して起動する
// （PowerShell で打ったときと同じ探し方になる）。
const winSessionNewScript = `if ($cmd.Count -eq 0) {
  $cmd = @('pwsh.exe', '-NoLogo')
} else {
  $app = Get-Command -Name $cmd[0] -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
  if (-not ($app -and $app.Extension -eq '.exe')) {
    $q = foreach ($a in $cmd) { "'" + ($a -replace "['‘’‚‛]", '$0$0') + "'" }
    $inner = '& ' + ($q -join ' ') + '; exit $LASTEXITCODE'
    $cmd = @('pwsh.exe', '-NoLogo', '-NoProfile', '-EncodedCommand', [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($inner)))
  }
}
& $exe @conf new-session -A -D -s $name -- @cmd
exit $LASTEXITCODE
`

// winSessionAttachScript は既存のセッションにつなぐ。
const winSessionAttachScript = `& $exe has-session -t "=$name" 2>$null
if ($LASTEXITCODE -ne 0) {
  [Console]::Error.WriteLine("koya: セッション $name はありません")
  exit 1
}
& $exe @conf attach-session -d -t "=$name"
exit $LASTEXITCODE
`

// winSessionListScript はセッションの一覧を「名前<TAB>つながっている数」の行で出す。psmux がなければ何も出さない。
const winSessionListScript = `$exe = Get-Command psmux.exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if ($exe) { & $exe.Source ls -F "#{session_name}` + "`t" + `#{session_attached}" 2>$null }
exit 0
`

// winPwsh は PowerShell のスクリプトを pwsh.exe で動かすコマンドを返す。
func winPwsh(script string) []string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return []string{"pwsh.exe", "-NoLogo", "-NoProfile", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)}
}

// psQuote は s を PowerShell の単一引用符の文字列にする。
// PowerShell は ' のほかに、‘ ’ ‚ ‛ も単一引用符として扱うので、どれも2つ重ねる。
func psQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '\u2018', '\u2019', '\u201a', '\u201b':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// psArray は ss を PowerShell の配列の式にする。
func psArray(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = psQuote(s)
	}
	return "@(" + strings.Join(q, ", ") + ")"
}

// SessionName はセッション名を決める。指定がなければコマンド名（なければ shell）。
// コマンド名は、ディレクトリ（/ と \ の前）と末尾の .exe を除いたもの。
// tmux のセッション名に使えない文字（. と :）は - に置き換える。
func SessionName(given string, cmd []string) string {
	name := given
	if name == "" {
		name = "shell"
		if len(cmd) > 0 {
			name = strings.TrimRight(cmd[0], `/\`)
			name = name[strings.LastIndexAny(name, `/\`)+1:]
			if strings.HasSuffix(strings.ToLower(name), ".exe") {
				name = name[:len(name)-len(".exe")]
			}
		}
	}
	name = strings.NewReplacer(".", "-", ":", "-", " ", "-").Replace(name)
	if name == "" {
		name = "shell"
	}
	return name
}

// Session はコンテナ内の tmux セッション。
type Session struct {
	Name     string
	Attached bool
}

// Sessions はコンテナ内のセッションを返す。tmux（Windows コンテナでは psmux）がない・セッションがない場合は空。
func Sessions(rt *runtime.Runtime, containerName string) []Session {
	args := []string{"exec", containerName, "sh", "-c",
		`command -v tmux >/dev/null 2>&1 && tmux ls -F '#{session_name}	#{session_attached}' 2>/dev/null`}
	if rt.Windows {
		args = append([]string{"exec", containerName}, winPwsh(winSessionListScript)...)
	}
	out, err := rt.Output(args...)
	if err != nil {
		return nil
	}
	var ss []Session
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name, attached, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || name == "" {
			continue
		}
		ss = append(ss, Session{Name: name, Attached: attached != "0"})
	}
	return ss
}
