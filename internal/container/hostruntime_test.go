package container

import (
	"reflect"
	"strings"
	"testing"

	"github.com/perorin0418/koya/internal/runtime"
)

func TestHostRuntimeArgs(t *testing.T) {
	label := []string{"--security-opt", "label=disable"}
	cases := []struct {
		name string
		h    *HostRuntime
		want []string
	}{
		{"rootless podman", &HostRuntime{Kind: "podman", Socket: "/run/user/1000/podman/podman.sock", GID: -1},
			append([]string{"-v", "/run/user/1000/podman/podman.sock:/run/podman/podman.sock",
				"-e", "DOCKER_HOST=unix:///run/podman/podman.sock",
				"-e", "CONTAINER_HOST=unix:///run/podman/podman.sock"}, label...)},
		{"rootful docker", &HostRuntime{Kind: "docker", Socket: "/var/run/docker.sock", GID: 999},
			append([]string{"-v", "/var/run/docker.sock:/var/run/docker.sock",
				"-e", "DOCKER_HOST=unix:///var/run/docker.sock",
				"--group-add", "999"}, label...)},
		{"group 0", &HostRuntime{Kind: "docker", Socket: "/var/run/docker.sock", GID: 0},
			append([]string{"-v", "/var/run/docker.sock:/var/run/docker.sock",
				"-e", "DOCKER_HOST=unix:///var/run/docker.sock",
				"--group-add", "0"}, label...)},
	}
	for _, c := range cases {
		if got := hostRuntimeArgs(c.h); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
	if hostRuntimeArgs(nil) != nil {
		t.Error("nil must give no args")
	}
}

func TestNeedsSocketGroup(t *testing.T) {
	u := User{Name: "u", UID: 1000, GID: 1000, Home: "/home/u"}
	root := User{Name: "root", UID: 0, GID: 0, Home: "/root"}
	cases := []struct {
		name string
		rt   runtime.Runtime
		u    User
		want bool
	}{
		{"rootless podman", runtime.Runtime{Kind: "podman", Rootless: true}, u, false},
		{"rootless via docker cli", runtime.Runtime{Kind: "docker", Rootless: true}, u, false},
		{"rootful docker", runtime.Runtime{Kind: "docker"}, u, true},
		{"rootful podman", runtime.Runtime{Kind: "podman"}, u, true},
		{"host root", runtime.Runtime{Kind: "docker"}, root, false},
	}
	for _, c := range cases {
		if got := needsSocketGroup(&c.rt, c.u); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDockerSocketFrom(t *testing.T) {
	cases := []struct {
		goos, host, want string
	}{
		{"linux", "", "/var/run/docker.sock"},
		{"linux", "unix:///var/run/docker.sock", "/var/run/docker.sock"},
		{"linux", "unix:///run/user/1000/docker.sock", "/run/user/1000/docker.sock"},
		{"linux", "unix:///run/user/1000/podman/podman.sock", "/run/user/1000/podman/podman.sock"},
		{"linux", "unix:///home/u/.docker/desktop/docker.sock", "/var/run/docker.sock"},
		{"darwin", "unix:///Users/u/.docker/run/docker.sock", "/var/run/docker.sock"},
		{"windows", "npipe:////./pipe/docker_engine", "/var/run/docker.sock"},
	}
	for _, c := range cases {
		got, err := dockerSocketFrom(c.goos, c.host)
		if err != nil || got != c.want {
			t.Errorf("%s %q: got %q, %v; want %q", c.goos, c.host, got, err, c.want)
		}
	}
	if _, err := dockerSocketFrom("linux", "tcp://10.0.0.1:2375"); err == nil || !strings.Contains(err.Error(), "tcp://") {
		t.Errorf("remote daemon must be rejected: %v", err)
	}
}

func TestDetectHostRuntimeWin(t *testing.T) {
	if _, _, err := DetectHostRuntime(&runtime.Runtime{Kind: "docker", Windows: true}, User{}, "base"); err == nil {
		t.Error("--win must be rejected")
	}
}

func TestOptionsKeyHostRuntime(t *testing.T) {
	if got := (Options{HostRT: true}).Key(); got != "dood" {
		t.Errorf("got %q", got)
	}
}

func TestRunArgsHostRuntime(t *testing.T) {
	rt := &runtime.Runtime{Kind: "podman", Rootless: true}
	s := Spec{Profile: "base", Workdir: "/w", User: User{Name: "u", UID: 1000, GID: 1000, Home: "/home/u"}}
	h := &HostRuntime{Kind: "podman", Socket: "/run/user/1000/podman/podman.sock", GID: -1}
	s.HostRT = h
	if strings.Contains(strings.Join(RunArgs(rt, s), " "), "podman.sock") {
		t.Error("socket must not be mounted unless Opts.HostRT")
	}
	s.Opts.HostRT = true
	if !strings.Contains(strings.Join(RunArgs(rt, s), " "), "/run/user/1000/podman/podman.sock:/run/podman/podman.sock") {
		t.Error("socket must be mounted with Opts.HostRT")
	}
}

func TestDetectHostRuntimeNerdctl(t *testing.T) {
	_, _, err := DetectHostRuntime(&runtime.Runtime{Kind: "nerdctl"}, User{}, "base")
	if err == nil || !strings.Contains(err.Error(), "nerdctl") {
		t.Errorf("nerdctl must be rejected: %v", err)
	}
}
