package container

import (
	"strings"
	"testing"

	"github.com/perorin0418/koya/internal/runtime"
)

func TestName(t *testing.T) {
	a := Name("python", "/home/u/src/My App")
	if !strings.HasPrefix(a, "koya-python-my-app-") || len(a) != len("koya-python-my-app-")+8 {
		t.Fatalf("unexpected name: %s", a)
	}
	if a == Name("python", "/other/My App") {
		t.Fatal("same basename in different dirs must differ")
	}
	if a != Name("python", "/home/u/src/My App") {
		t.Fatal("name must be stable")
	}
}

func TestOptionsKeyIsOrderIndependent(t *testing.T) {
	a := Options{Env: []string{"B=2", "A=1"}, Net: "none"}.Key()
	b := Options{Env: []string{"A=1", "B=2"}, Net: "none"}.Key()
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if (Options{}).Key() != "" {
		t.Fatal("empty options must give empty key")
	}
}

func TestParseInfoMissingTrailingField(t *testing.T) {
	info, ok := parseInfo("/koya-base-x-1\tbase\t/w\trunning\tabc")
	if !ok || info.Name != "koya-base-x-1" || info.Hash != "abc" || info.Opts != "" || !info.Running() {
		t.Fatalf("unexpected: %+v %v", info, ok)
	}
}

func TestHomeOf(t *testing.T) {
	k := Options{Net: "none", Home: "/data/home/claude"}.Key()
	if HomeOf(k) != "/data/home/claude" || HomeOf(Options{}.Key()) != "" {
		t.Fatalf("unexpected: %q", k)
	}
}

func TestSessionName(t *testing.T) {
	cases := []struct {
		given string
		cmd   []string
		want  string
	}{
		{"", nil, "shell"},
		{"", []string{"/usr/bin/claude", "--resume"}, "claude"},
		{"my.work:1", nil, "my-work-1"},
		{"work", []string{"claude"}, "work"},
	}
	for _, c := range cases {
		if got := SessionName(c.given, c.cmd); got != c.want {
			t.Errorf("SessionName(%q, %v) = %q, want %q", c.given, c.cmd, got, c.want)
		}
	}
}

func TestWinRunArgs(t *testing.T) {
	rt := runtime.ForWindows(`C:\koya\docker.exe`, "npipe:////./pipe/koya_docker")
	s := Spec{Profile: "base", Workdir: `D:\src\app`, Hash: "h", Isolation: "process",
		Opts: Options{ReadOnly: true, Net: "none"}}
	got := strings.Join(RunArgs(rt, s), " ")
	for _, want := range []string{
		`-v D:\src\app:C:\mnt\d\src\app:ro`,
		`-w C:\mnt\d\src\app`,
		"--isolation process",
		"--network none",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RunArgs に %q がありません: %s", want, got)
		}
	}
	for _, bad := range []string{"--user", "--userns", "HOME="} {
		if strings.Contains(got, bad) {
			t.Errorf("Windows コンテナの RunArgs に %q があります: %s", bad, got)
		}
	}
}

func TestExecArgsWindows(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	rt := runtime.ForWindows("docker.exe", "npipe:////./pipe/koya_docker")
	got := strings.Join(ExecArgs(rt, "n", `C:\src\app`, true, WinShell(true)), " ")
	if got != `exec -i -t -w C:\src\app n powershell.exe -NoLogo` {
		t.Fatalf("got %s", got)
	}
}
