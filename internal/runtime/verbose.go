package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Verbose が true なら、ランタイムのコマンドを実行するたびに、その内容と結果を標準エラーに表示する
// （koya --verbose、または環境変数 KOYA_VERBOSE）。
var Verbose = os.Getenv("KOYA_VERBOSE") != ""

// verboseOut は詳細ログの出力先（テストで差し替える）。
var verboseOut io.Writer = os.Stderr

// heartbeat は、終わらないコマンドについて「実行中」と表示する間隔。
var heartbeat = 10 * time.Second

// logf は詳細ログを1行表示する。
func logf(format string, a ...any) {
	fmt.Fprintf(verboseOut, "koya: [詳細] "+format+"\n", a...)
}

// commandLine は表示用のコマンドライン。空白などを含む引数は引用符で囲み、長い引数（スクリプトなど）は省略する。
func (r *Runtime) commandLine(args []string) string {
	parts := []string{filepath.Base(r.Bin)}
	for _, a := range r.argv(args) {
		switch {
		case strings.Contains(a, "\n") || len(a) > 200:
			a = fmt.Sprintf("<%d 文字の引数>", len(a))
		case a == "" || strings.ContainsAny(a, " \t\"'"):
			a = fmt.Sprintf("%q", a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// trace はコマンドを実行する前に呼ぶ。返した関数を実行後に呼ぶと、かかった時間と結果を表示する。
// 実行中は heartbeat ごとに経過時間を表示する（どのコマンドで止まっているかを分かるようにするため）。
// Verbose でなければ何もしない。
func (r *Runtime) trace(args []string) func(error) {
	if !Verbose {
		return func(error) {}
	}
	line := r.commandLine(args)
	logf("実行: %s", line)
	start := time.Now()
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				logf("実行中（%s 経過）: %s", time.Since(start).Round(time.Second), line)
			}
		}
	}()
	return func(err error) {
		close(done)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			logf("失敗（%s）: %v", elapsed, err)
			return
		}
		logf("完了（%s）", elapsed)
	}
}
