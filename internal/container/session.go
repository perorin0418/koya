package container

import (
	"path"
	"strings"

	"github.com/perorin0418/koya/internal/runtime"
)

// セッション機能: コンテナ内の tmux でコマンドを動かし、端末が切れても動き続けるようにする。
// もう一度つなぐと、同じ画面に戻る。

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
// cmd が空なら対話シェル。
func SessionCommand(name string, cmd []string) []string {
	return append([]string{"sh", "-c", sessionNewScript, "koya-session", name}, cmd...)
}

// AttachCommand は既存のセッションにつなぐコマンドを返す。
func AttachCommand(name string) []string {
	return []string{"sh", "-c", sessionAttachScript, "koya-session", name}
}

// SessionName はセッション名を決める。指定がなければコマンド名（なければ shell）。
// tmux のセッション名に使えない文字（. と :）は - に置き換える。
func SessionName(given string, cmd []string) string {
	name := given
	if name == "" {
		name = "shell"
		if len(cmd) > 0 {
			name = path.Base(cmd[0])
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

// Sessions はコンテナ内のセッションを返す。tmux がない・セッションがない場合は空。
func Sessions(rt *runtime.Runtime, containerName string) []Session {
	out, err := rt.Output("exec", containerName, "sh", "-c",
		`command -v tmux >/dev/null 2>&1 && tmux ls -F '#{session_name}	#{session_attached}' 2>/dev/null`)
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
