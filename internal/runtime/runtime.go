// Package runtime はコンテナランタイム（podman / docker）の検出と呼び出しを扱う。
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
	Bin      string // 実行ファイルのパス
	Kind     string // "podman" または "docker"
	Rootless bool   // rootless で動いているか
}

// Detect は KOYA_RUNTIME、なければ podman → docker の順にランタイムを探す。
func Detect() (*Runtime, error) {
	want := os.Getenv("KOYA_RUNTIME")
	candidates := []string{"podman", "docker"}
	if want != "" {
		candidates = []string{want}
	}
	for _, c := range candidates {
		p, err := exec.LookPath(c)
		if err != nil {
			continue
		}
		r := &Runtime{Bin: p, Kind: "docker"}
		if strings.Contains(filepath.Base(c), "podman") {
			r.Kind = "podman"
		}
		r.Rootless = r.detectRootless()
		return r, nil
	}
	if want != "" {
		return nil, fmt.Errorf("KOYA_RUNTIME=%s が見つかりません", want)
	}
	return nil, errors.New("podman も docker も見つかりません。どちらかをインストールしてください")
}

func (r *Runtime) detectRootless() bool {
	if r.Kind == "podman" {
		out, err := r.Output("info", "--format", "{{.Host.Security.Rootless}}")
		return err == nil && strings.TrimSpace(out) == "true"
	}
	// docker（rootless docker や、docker CLI から rootless podman につないでいる場合を含む）
	out, err := r.Output("info", "--format", "{{json .SecurityOptions}}")
	return err == nil && strings.Contains(out, "rootless")
}

// Output はコマンドを実行して標準出力を返す。失敗時は標準エラーをエラーに含める。
func (r *Runtime) Output(args ...string) (string, error) {
	cmd := exec.Command(r.Bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return string(out), fmt.Errorf("%s %s: %s", filepath.Base(r.Bin), args[0], msg)
	}
	return string(out), nil
}

// Run はコマンドを実行する。標準出力は stderr に流す（koya の出力と混ぜないため）。
func (r *Runtime) Run(args ...string) error {
	cmd := exec.Command(r.Bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s に失敗しました: %w", filepath.Base(r.Bin), args[0], err)
	}
	return nil
}

// Quiet はコマンドを出力なしで実行し、成否だけを返す。
func (r *Runtime) Quiet(args ...string) error {
	_, err := r.Output(args...)
	return err
}
