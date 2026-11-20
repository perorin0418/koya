package container

import (
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"

	"github.com/perorin0418/koya/internal/profile"
	"github.com/perorin0418/koya/internal/runtime"
)

// HostRuntime は、コンテナの中から使えるようにするホストのランタイム（API のソケット）。
//
// ソケットをコンテナにマウントし、docker / podman（podman-remote）のクライアントが
// 何も設定しなくてもつながるように DOCKER_HOST・CONTAINER_HOST を設定する。
// コンテナの中から作ったコンテナはホストのランタイムの上で動く（入れ子ではなく兄弟）。
// ソケットに触れるとホストのコンテナをすべて操作できる（rootful ならホストの root 相当）ので、隔離は弱まる。
type HostRuntime struct {
	Kind   string // podman / docker（ソケットの種類）
	Socket string // マウント元のソケットのパス（ランタイムが動いているマシンから見たパス）
	GID    int    // ソケットを開くために付け足す補助グループ。-1 なら付けない
}

// コンテナ内のソケットの置き場所（各クライアントの既定の場所）
const (
	dockerSocketTarget = "/var/run/docker.sock"
	podmanSocketTarget = "/run/podman/podman.sock"
)

// Target はコンテナ内のソケットのパス。
func (h *HostRuntime) Target() string {
	if h.Kind == "podman" {
		return podmanSocketTarget
	}
	return dockerSocketTarget
}

// hostRuntimeArgs はホストのランタイムのソケットをコンテナに渡すための引数を返す。
// podman のソケットは docker 互換の API も話すので、DOCKER_HOST も向けておく（docker CLI でも使える）。
// SELinux が有効なホストでは、ソケットを開けるようにラベルによる隔離を外す。
func hostRuntimeArgs(h *HostRuntime) []string {
	if h == nil {
		return nil
	}
	target := h.Target()
	args := []string{"-v", h.Socket + ":" + target, "-e", "DOCKER_HOST=unix://" + target}
	if h.Kind == "podman" {
		args = append(args, "-e", "CONTAINER_HOST=unix://"+target)
	}
	if h.GID >= 0 {
		args = append(args, "--group-add", strconv.Itoa(h.GID))
	}
	return append(args, "--security-opt", "label=disable")
}

// needsSocketGroup は、ソケットを開くために補助グループを足す必要があるかを返す。
//   - コンテナ内で UID 0 として動かす場合（rootless docker など）: root なので不要
//   - rootless の podman: ソケットの所有者がホストのユーザー自身（--userns=keep-id で同じ UID）なので不要
//   - それ以外（rootful の docker・Docker Desktop など）: ソケットの所有グループ（docker など）が必要
func needsSocketGroup(rt *runtime.Runtime, u User) bool {
	if runsAsRoot(rt, u) {
		return false
	}
	return !(rt.Kind == "podman" && rt.Rootless)
}

// DetectHostRuntime は、コンテナに渡すホストのランタイムのソケットを探す。
// prof はコンテナのプロファイル（ソケットの所有グループを調べるのに、そのイメージを使う）。
// 所有グループを調べられなかったときは GID を -1 にし、warn に理由を入れる（エラーにはしない）。
func DetectHostRuntime(rt *runtime.Runtime, u User, prof string) (h *HostRuntime, warn string, err error) {
	if rt.Windows {
		return nil, "", errors.New("--dood は --win（Windows コンテナ）では使えません")
	}
	h = &HostRuntime{Kind: rt.Kind, GID: -1}
	if rt.Kind == "podman" {
		h.Socket, err = podmanSocket(rt)
	} else {
		h.Socket, err = dockerSocket(rt)
	}
	if err != nil {
		return nil, "", err
	}
	if !needsSocketGroup(rt, u) {
		return h, "", nil
	}
	// ソケットはランタイムが動いているマシン（VM のこともある）にあるので、ホストから stat せず、
	// コンテナにマウントして所有グループを調べる
	out, perr := rt.Output("run", "--rm", "--security-opt", "label=disable",
		"-v", h.Socket+":/koya-probe.sock", "--entrypoint", "stat",
		profile.ImageName(prof), "-c", "%g", "/koya-probe.sock")
	gid, cerr := strconv.Atoi(strings.TrimSpace(out))
	if perr != nil || cerr != nil {
		reason := perr
		if reason == nil {
			reason = cerr
		}
		return h, fmt.Sprintf("ソケット %s の所有グループを調べられませんでした（%v）。コンテナ内では sudo が必要になるかもしれません", h.Socket, reason), nil
	}
	h.GID = gid
	return h, "", nil
}

// podmanSocket は podman の API のソケットのパスを返す。
// podman machine（macOS・Windows）でも、ランタイムが動く VM の中のパスが返る（マウント元はそのパスでよい）。
func podmanSocket(rt *runtime.Runtime) (string, error) {
	out, err := rt.Output("info", "--format", "{{.Host.RemoteSocket.Path}}\t{{.Host.RemoteSocket.Exists}}")
	if err != nil {
		return "", fmt.Errorf("podman の API のソケットの場所を調べられません: %w", err)
	}
	path, exists, _ := strings.Cut(strings.TrimSpace(out), "\t")
	path = strings.TrimPrefix(path, "unix://")
	if path == "" {
		return "", errors.New("podman の API のソケットの場所が分かりません")
	}
	if exists != "true" {
		enable := "systemctl --user enable --now podman.socket"
		if !rt.Rootless {
			enable = "sudo systemctl enable --now podman.socket"
		}
		return "", fmt.Errorf("podman の API のソケット（%s）がありません。%s で有効にしてください", path, enable)
	}
	return path, nil
}

// dockerSocket は docker のデーモンのソケットのパス（マウント元）を返す。
func dockerSocket(rt *runtime.Runtime) (string, error) {
	host := ""
	if goruntime.GOOS == "linux" {
		host = dockerHost(rt)
	}
	return dockerSocketFrom(goruntime.GOOS, host)
}

// dockerHost は docker CLI がつなぐデーモンのアドレス（DOCKER_HOST、なければ今のコンテキスト）を返す。
func dockerHost(rt *runtime.Runtime) string {
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		return h
	}
	out, err := rt.Output("context", "inspect", "--format", "{{.Endpoints.docker.Host}}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// dockerSocketFrom は OS とデーモンのアドレスから、マウント元のソケットのパスを決める。
//   - Linux 以外（Docker Desktop・Rancher Desktop など）: デーモンは VM の中にあり、
//     /var/run/docker.sock を指定すると VM の中のソケットがマウントされる
//   - Linux の Docker Desktop（~/.docker/desktop/docker.sock）: 同じく VM の中なので /var/run/docker.sock
//   - Linux: unix:// のアドレスならそのパス（rootless docker や、DOCKER_HOST で podman につないでいる場合を含む）。
//     アドレスがなければ既定の /var/run/docker.sock。tcp:// などのリモートのデーモンは使えない
func dockerSocketFrom(goos, host string) (string, error) {
	if goos != "linux" || host == "" {
		return dockerSocketTarget, nil
	}
	path, ok := strings.CutPrefix(host, "unix://")
	if !ok {
		return "", fmt.Errorf("--dood はソケットでつなぐデーモンでだけ使えます（今のつなぎ先: %s）", host)
	}
	if strings.Contains(path, "/.docker/desktop/") {
		return dockerSocketTarget, nil
	}
	return path, nil
}
