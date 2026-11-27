// Package runtime はコンテナランタイム（podman / docker / nerdctl）の検出と呼び出しを扱う。
package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runtime は検出したコンテナランタイム。
type Runtime struct {
	Bin      string   // 実行ファイルのパス
	Kind     string   // "podman"・"docker"・"nerdctl" のいずれか
	Rootless bool     // rootless で動いているか
	Prefix   []string // すべてのコマンドの前に付ける引数（つなぐデーモンの指定など）
	Windows  bool     // Windows コンテナ用のデーモン（koya run --win）につないでいるか
}

// ForWindows は koya 専用の Windows コンテナ用デーモンにつなぐ Runtime を返す。
// bin は docker.exe、host はデーモンのアドレス（npipe:////./pipe/...）。
func ForWindows(bin, host string) *Runtime {
	return &Runtime{Bin: bin, Kind: "docker", Prefix: []string{"-H", host}, Windows: true}
}

// argv はコマンドの引数に Prefix を付ける。
func (r *Runtime) argv(args []string) []string {
	return append(append([]string(nil), r.Prefix...), args...)
}

// Detect は KOYA_RUNTIME、なければ podman → docker → nerdctl の順にランタイムを探す。
func Detect() (*Runtime, error) {
	want := os.Getenv("KOYA_RUNTIME")
	candidates := []string{"podman", "docker", "nerdctl"}
	if want != "" {
		candidates = []string{want}
	}
	for _, c := range candidates {
		p, err := exec.LookPath(c)
		if err != nil {
			continue
		}
		r := &Runtime{Bin: p, Kind: KindOf(c)}
		r.Rootless = r.detectRootless()
		return r, nil
	}
	if want != "" {
		return nil, fmt.Errorf("KOYA_RUNTIME=%s が見つかりません", want)
	}
	return nil, errors.New("podman・docker・nerdctl のどれも見つかりません。どれかをインストールしてください")
}

// KindOf はランタイムのコマンド名（パスでもよい）から種類を決める。
// podman・nerdctl 以外は docker の CLI として扱う。
func KindOf(bin string) string {
	base := filepath.Base(bin)
	switch {
	case strings.Contains(base, "podman"):
		return "podman"
	case strings.Contains(base, "nerdctl"):
		return "nerdctl"
	default:
		return "docker"
	}
}

func (r *Runtime) detectRootless() bool {
	if r.Kind == "podman" {
		out, err := r.Output("info", "--format", "{{.Host.Security.Rootless}}")
		return err == nil && strings.TrimSpace(out) == "true"
	}
	// docker（rootless docker や、docker CLI から rootless podman につないでいる場合を含む）と nerdctl。
	// rootless の nerdctl も SecurityOptions に name=rootless を返す
	out, err := r.Output("info", "--format", "{{json .SecurityOptions}}")
	return err == nil && strings.Contains(out, "rootless")
}

// Output はコマンドを実行して標準出力を返す。失敗時は標準エラーをエラーに含める。
func (r *Runtime) Output(args ...string) (string, error) {
	cmd := exec.Command(r.Bin, r.argv(args)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	end := r.trace(args)
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		err = fmt.Errorf("%s %s: %s", filepath.Base(r.Bin), args[0], msg)
	}
	end(err)
	return string(out), err
}

// Run はコマンドを実行する。標準出力は stderr に流す（koya の出力と混ぜないため）。
func (r *Runtime) Run(args ...string) error {
	cmd := exec.Command(r.Bin, r.argv(args)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	end := r.trace(args)
	err := cmd.Run()
	end(err)
	if err != nil {
		return fmt.Errorf("%s %s に失敗しました: %w", filepath.Base(r.Bin), args[0], err)
	}
	return nil
}

// Command は Prefix を付けたコマンドを返す（標準入出力の接続は呼び出し側で行う）。
// 詳細ログでは、作った時点で内容だけを表示する（実行するのは呼び出し側のため）。
func (r *Runtime) Command(args ...string) *exec.Cmd {
	if Verbose {
		logf("実行: %s", r.commandLine(args))
	}
	return exec.Command(r.Bin, r.argv(args)...)
}

// Quiet はコマンドを出力なしで実行し、成否だけを返す。
func (r *Runtime) Quiet(args ...string) error {
	_, err := r.Output(args...)
	return err
}
