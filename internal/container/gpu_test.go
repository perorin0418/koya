package container

import (
	"reflect"
	"testing"

	"github.com/perorin0418/koya/internal/runtime"
)

func TestGPUArgs(t *testing.T) {
	g := &GPU{Devices: []string{"/dev/kfd", "/dev/dri"}, GIDs: []int{105, 39, 0}}
	u := User{Name: "u", UID: 1000, GID: 1000, Home: "/home/u"}
	dev := []string{"--device", "/dev/kfd", "--device", "/dev/dri"}
	label := []string{"--security-opt", "label=disable"}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	cases := []struct {
		name string
		rt   runtime.Runtime
		want []string
	}{
		{"rootless podman", runtime.Runtime{Kind: "podman", Rootless: true},
			cat(dev, []string{"--group-add", "keep-groups"}, label)},
		{"rootless via docker cli", runtime.Runtime{Kind: "docker", Rootless: true}, cat(dev, label)},
		{"rootful", runtime.Runtime{Kind: "docker"},
			cat(dev, []string{"--group-add", "39", "--group-add", "105"}, label)},
	}
	for _, c := range cases {
		if got := gpuArgs(&c.rt, u, g); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if gpuArgs(&runtime.Runtime{Kind: "podman"}, u, nil) != nil {
		t.Error("nil GPU must give no args")
	}
}

func TestGPUOf(t *testing.T) {
	if !GPUOf(Options{GPU: true, Net: "none"}.Key()) || GPUOf(Options{Net: "none"}.Key()) {
		t.Fatal("unexpected GPUOf")
	}
}
