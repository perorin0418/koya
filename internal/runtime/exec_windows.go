//go:build windows

package runtime

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// Exec はランタイムのコマンドを子プロセスとして実行し、その終了コードで koya を終了する。
// Windows にはプロセスを置き換える仕組み（exec）がないため。成功すると戻らない。
func (r *Runtime) Exec(args ...string) error {
	cmd := exec.Command(r.Bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Ctrl-C は子プロセス（コンテナ内のプロセス）に任せ、koya 自身は終了しない
	signal.Ignore(os.Interrupt)
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		os.Exit(0)
	case errors.As(err, &ee):
		os.Exit(ee.ExitCode())
	}
	return err
}
