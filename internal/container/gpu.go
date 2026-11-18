package container

import (
	"fmt"
	"sort"

	"github.com/perorin0418/koya/internal/runtime"
)

// GPU はコンテナに渡す GPU のデバイス。
type GPU struct {
	Devices []string // 渡すデバイス（/dev/kfd、/dev/dri）
	GIDs    []int    // デバイスの所有グループ（ホストの GID）
	Denied  []string // 今のユーザーでは開けないデバイス
}

// gpuArgs は GPU のデバイスをコンテナに渡すための引数を返す。
//
// デバイスを開くには、所有グループ（render・video）に入っている必要がある。
//   - rootless の podman: --group-add keep-groups でホストのユーザーの補助グループをそのまま引き継ぐ
//     （ユーザー名前空間の中ではホストの GID を指定できないため）
//   - rootless のランタイムを docker CLI から使う場合: 補助グループは引き継がれるので何もしない
//   - rootful: デバイスの所有グループの GID を追加する
//
// SELinux が有効なホスト（Fedora など）では、コンテナからデバイスを開けるように
// ラベルによる隔離を外す（--security-opt label=disable）。
func gpuArgs(rt *runtime.Runtime, u User, g *GPU) []string {
	if g == nil {
		return nil
	}
	var args []string
	for _, d := range g.Devices {
		args = append(args, "--device", d)
	}
	switch {
	case rt.Rootless && rt.Kind == "podman" && !runsAsRoot(rt, u):
		args = append(args, "--group-add", "keep-groups")
	case rt.Rootless:
	default:
		gids := append([]int(nil), g.GIDs...)
		sort.Ints(gids)
		for _, gid := range gids {
			if gid != 0 {
				args = append(args, "--group-add", fmt.Sprint(gid))
			}
		}
	}
	return append(args, "--security-opt", "label=disable")
}
