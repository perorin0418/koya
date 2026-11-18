package winengine

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/perorin0418/koya/internal/runtime"
)

// errNoContainers は Windows の「コンテナー」機能が有効になっていないときのエラー。
var errNoContainers = errors.New(`Windows の「コンテナー」機能が有効になっていません。
管理者の PowerShell で次を実行し、Windows を再起動してから、もう一度実行してください:

  Enable-WindowsOptionalFeature -Online -FeatureName Containers -All
  # Hyper-V 分離も使う場合（Windows 10 など）は、さらに次も:
  Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V -All

Windows の Home エディションでは使えません（Pro / Enterprise / Education が必要です）`)

// checkContainersFeature は「コンテナー」機能（Host Compute Service: vmcompute）があるかを確かめる。
func checkContainersFeature() error {
	_, exists, err := serviceState("vmcompute")
	if err != nil {
		return err
	}
	if !exists {
		return errNoContainers
	}
	return nil
}

// Ensure は koya 専用のデーモンを使える状態にして、そこにつなぐ Runtime と、使うベースイメージを返す。
// デーモンがなければ、管理者権限で koya win _install を実行して入れる（UAC の確認が出る）。
func Ensure() (*runtime.Runtime, Base, error) {
	base, err := HostBase()
	if err != nil {
		return nil, Base{}, err
	}
	if err := checkContainersFeature(); err != nil {
		return nil, Base{}, err
	}
	p := DefaultPaths()
	state, exists, err := serviceState(ServiceName)
	if err != nil {
		return nil, Base{}, err
	}
	_, statErr := os.Stat(p.DockerCLI())
	switch {
	case !exists || statErr != nil:
		fmt.Fprintf(os.Stderr, "koya: Windows コンテナ用のデーモン（Docker %s）を入れます。管理者権限の確認が出ます\n", DockerVersion)
		if err := elevate("_install", "--user", currentUser()); err != nil {
			return nil, Base{}, err
		}
	case state != stateRunning:
		fmt.Fprintln(os.Stderr, "koya: Windows コンテナ用のデーモンを起動します。管理者権限の確認が出ます")
		if err := elevate("_start"); err != nil {
			return nil, Base{}, err
		}
	}
	rt := runtime.ForWindows(p.DockerCLI(), Host)
	if err := waitReady(rt, 90*time.Second); err != nil {
		return nil, Base{}, err
	}
	return rt, base, nil
}

// HostBase はホストに合うベースイメージを返す。
func HostBase() (Base, error) {
	build, err := hostBuild()
	if err != nil {
		return Base{}, err
	}
	return BaseFor(build)
}

// RequestUninstall は Uninstall を実行する。管理者でなければ、管理者権限で koya win _uninstall を実行する。
func RequestUninstall() error {
	if _, err := hostBuild(); err != nil {
		return err
	}
	if isAdmin() {
		return Uninstall()
	}
	fmt.Fprintln(os.Stderr, "koya: Windows コンテナ用のデーモンを外します。管理者権限の確認が出ます")
	return elevate("_uninstall")
}

// Runtime は、入っていて起動しているデーモンにつなぐ Runtime を返す（入れたり起動したりはしない）。
func Runtime() (*runtime.Runtime, error) {
	p := DefaultPaths()
	state, exists, err := serviceState(ServiceName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("Windows コンテナ用のデーモンが入っていません（koya run --win か koya win setup で入れます）")
	}
	if state != stateRunning {
		return nil, errors.New("Windows コンテナ用のデーモンが止まっています（koya win setup で起動します）")
	}
	return runtime.ForWindows(p.DockerCLI(), Host), nil
}

// waitReady はデーモンが応答するまで待つ。
func waitReady(rt *runtime.Runtime, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		out, err := rt.Output("version", "--format", "{{.Server.Os}}")
		if err == nil {
			if kind := strings.TrimSpace(out); kind != "windows" {
				return fmt.Errorf("koya のデーモンが Windows コンテナ用になっていません（%s）", kind)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Windows コンテナ用のデーモンが応答しません: %w", err)
		}
		time.Sleep(time.Second)
	}
}

func currentUser() string {
	// USERDOMAIN\USERNAME。パイプにつなげるユーザーとして dockerd の --group に渡す
	d, u := os.Getenv("USERDOMAIN"), os.Getenv("USERNAME")
	if d != "" && u != "" {
		return d + `\` + u
	}
	return u
}

// elevate は koya 自身を管理者として実行する（koya win <args...>）。
// 昇格したプロセスの出力は別のウィンドウに出て消えるため、ログファイルに書かせて後で表示する。
func elevate(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.CreateTemp("", "koya-win-*.log")
	if err != nil {
		return err
	}
	logPath := log.Name()
	log.Close()
	defer os.Remove(logPath)

	full := append([]string{"win"}, args...)
	full = append(full, "--log", logPath)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ElevateScript(exe, full))
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	runErr := cmd.Run()
	if b, err := os.ReadFile(logPath); err == nil && len(b) > 0 {
		os.Stderr.Write(b)
	}
	if runErr != nil {
		return fmt.Errorf("管理者権限での実行に失敗しました（確認を拒否した場合もこうなります）: %w", runErr)
	}
	return nil
}

func requireAdmin() error {
	if !isAdmin() {
		return errors.New("管理者として実行してください")
	}
	return nil
}

// Install はデーモンを入れて、サービスとして登録し、起動する（管理者権限で実行する）。
// user はパイプにつなげるユーザー。
func Install(user string) error {
	if err := requireAdmin(); err != nil {
		return err
	}
	if err := checkContainersFeature(); err != nil {
		return err
	}
	p := DefaultPaths()
	fresh := false
	if _, err := os.Stat(p.Dockerd()); err != nil {
		fresh = true
		fmt.Fprintf(os.Stderr, "koya: %s を取ってきます\n", DownloadURL())
		zip, err := Download()
		if err != nil {
			return err
		}
		if err := Extract(zip, p.BinDir); err != nil {
			return fmt.Errorf("%s に展開できません: %w", p.BinDir, err)
		}
		fmt.Fprintf(os.Stderr, "koya: %s に展開しました\n", p.BinDir)
	}
	if err := os.MkdirAll(filepath.Dir(p.Config), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(p.Config); os.IsNotExist(err) {
		if err := os.WriteFile(p.Config, []byte("{}\n"), 0o644); err != nil {
			return err
		}
	}
	_, exists, err := serviceState(ServiceName)
	if err != nil {
		return err
	}
	if exists && fresh {
		// 別の版の dockerd.exe を指しているサービスを登録し直す（koya の Docker の版を上げたとき）
		if err := removeService(); err != nil {
			return err
		}
		exists = false
	}
	if !exists {
		cmd := exec.Command(p.Dockerd(), p.DaemonArgs(user)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("サービス %s を登録できません: %v\n%s", ServiceName, err, out)
		}
		fmt.Fprintf(os.Stderr, "koya: サービス %s を登録しました（パイプにつなげるユーザー: %s）\n", ServiceName, user)
	}
	return Start()
}

// Start はサービスを起動して、起動するまで待つ（管理者権限で実行する）。
func Start() error {
	if err := requireAdmin(); err != nil {
		return err
	}
	if err := startService(ServiceName); err != nil {
		return fmt.Errorf("サービス %s を起動できません: %w", ServiceName, err)
	}
	return waitState(stateRunning, 60*time.Second)
}

// Uninstall はサービスを止めて登録を外し、実行ファイルを消す（管理者権限で実行する）。
// イメージやコンテナ（データの置き場所）は消さない。
func Uninstall() error {
	if err := requireAdmin(); err != nil {
		return err
	}
	p := DefaultPaths()
	_, exists, err := serviceState(ServiceName)
	if err != nil {
		return err
	}
	if exists {
		if err := removeService(); err != nil {
			return err
		}
	}
	// どの版の dockerd.exe も消す（Program Files\koya\docker-<版>）
	root := filepath.Dir(p.BinDir)
	olds, _ := filepath.Glob(filepath.Join(root, "docker-*"))
	for _, d := range olds {
		if err := os.RemoveAll(d); err != nil {
			return err
		}
	}
	_ = os.Remove(root) // 空なら消す
	fmt.Fprintf(os.Stderr, "koya: %s の下の dockerd.exe などを消しました。イメージとコンテナは %s に残っています\n", root, p.DataRoot)
	return nil
}

// removeService はサービスを止めて登録を外す。
// dockerd.exe --unregister-service ではなく sc.exe を使い、dockerd.exe が消えていても外せるようにする。
func removeService() error {
	if err := stopService(ServiceName); err != nil {
		return fmt.Errorf("サービス %s を止められません: %w", ServiceName, err)
	}
	if err := waitState(stateStopped, 60*time.Second); err != nil {
		return err
	}
	out, err := exec.Command("sc.exe", "delete", ServiceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("サービス %s の登録を外せません: %v\n%s", ServiceName, err, out)
	}
	fmt.Fprintf(os.Stderr, "koya: サービス %s の登録を外しました\n", ServiceName)
	return nil
}

func waitState(want uint32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		st, _, err := serviceState(ServiceName)
		if err != nil {
			return err
		}
		if st == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("サービス %s の状態が変わりません（状態: %d）", ServiceName, st)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Status は koya win status で表示する内容を返す。
func Status() string {
	var b strings.Builder
	p := DefaultPaths()
	fmt.Fprintf(&b, "Docker の版（固定）: %s\n", DockerVersion)
	if build, err := hostBuild(); err != nil {
		fmt.Fprintf(&b, "ホスト: %v\n", err)
		return b.String()
	} else if base, err := BaseFor(build); err != nil {
		fmt.Fprintf(&b, "ホストのビルド: %d（%v）\n", build, err)
	} else {
		fmt.Fprintf(&b, "ホストのビルド: %d → ベースイメージ windows/server:%s、分離: %s\n", build, base.Tag, base.Isolation)
	}
	if err := checkContainersFeature(); err != nil {
		fmt.Fprintln(&b, "コンテナー機能: 無効")
	} else {
		fmt.Fprintln(&b, "コンテナー機能: 有効")
	}
	switch st, exists, err := serviceState(ServiceName); {
	case err != nil:
		fmt.Fprintf(&b, "サービス %s: %v\n", ServiceName, err)
	case !exists:
		fmt.Fprintf(&b, "サービス %s: 未登録\n", ServiceName)
	case st == stateRunning:
		fmt.Fprintf(&b, "サービス %s: 起動中（%s）\n", ServiceName, Host)
	default:
		fmt.Fprintf(&b, "サービス %s: 停止中\n", ServiceName)
	}
	fmt.Fprintf(&b, "実行ファイル: %s\nデータ: %s\n", p.BinDir, p.DataRoot)
	return b.String()
}
