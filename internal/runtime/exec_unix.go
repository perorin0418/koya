//go:build !windows

package runtime

import (
	"os"
	"syscall"
)

// Exec は現在のプロセスをランタイムのコマンドに置き換える（TTY やシグナルをそのまま渡すため）。
// 成功すると戻らない。
func (r *Runtime) Exec(args ...string) error {
	argv := append([]string{r.Bin}, args...)
	return syscall.Exec(r.Bin, argv, os.Environ())
}
