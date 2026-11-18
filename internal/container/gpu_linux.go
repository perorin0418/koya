package container

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// DetectGPU はホストの GPU のデバイスを探す。
// AMD の GPU（ROCm）は /dev/kfd と /dev/dri を、そのほか（Intel など）は /dev/dri を使う。
// 今のユーザーで開けないデバイスは GPU.Denied に入れる。
func DetectGPU() (*GPU, error) {
	g := &GPU{}
	gids := map[int]bool{}
	check := func(path string) {
		var st syscall.Stat_t
		if syscall.Stat(path, &st) != nil {
			return
		}
		gids[int(st.Gid)] = true
		// R_OK|W_OK
		if syscall.Access(path, 0x4|0x2) != nil {
			g.Denied = append(g.Denied, path)
		}
	}
	if _, err := os.Stat("/dev/kfd"); err == nil {
		g.Devices = append(g.Devices, "/dev/kfd")
		check("/dev/kfd")
	}
	if st, err := os.Stat("/dev/dri"); err == nil && st.IsDir() {
		nodes, _ := filepath.Glob("/dev/dri/renderD*")
		if len(nodes) > 0 {
			g.Devices = append(g.Devices, "/dev/dri")
			for _, n := range nodes {
				check(n)
			}
		}
	}
	if len(g.Devices) == 0 {
		return nil, errors.New("GPU のデバイス（/dev/kfd、/dev/dri）が見つかりません。ドライバ（amdgpu など）が読み込まれているか確かめてください")
	}
	for gid := range gids {
		g.GIDs = append(g.GIDs, gid)
	}
	return g, nil
}
