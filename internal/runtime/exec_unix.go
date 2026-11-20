//go:build !windows

package runtime

import (
	"os"
	"syscall"
)

// Exec は現在のプロセスをランタイムのコマンドに置き換える（TTY やシグナルをそのまま渡すため）。
// 成功すると戻らない。
func (r *Runtime) Exec(args ...string) error {
	if Verbose {
		logf("実行（ここからはコンテナの中）: %s", r.commandLine(args))
	}
	argv := append([]string{r.Bin}, r.argv(args)...)
	return syscall.Exec(r.Bin, argv, os.Environ())
}
