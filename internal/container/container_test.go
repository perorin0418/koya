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
	if !strings.HasPrefix(got, `exec -i -t -w C:\src\app n cmd.exe /d /c where /q pwsh.exe`) || !strings.HasSuffix(got, "(pwsh.exe -NoLogo)") {
		t.Fatalf("got %s", got)
	}
}

func TestWinUser(t *testing.T) {
	cases := map[string]User{
		`GYOSEIQ\gcom0154`:          {Name: "gcom0154", Home: `C:\Users\gcom0154`},
		`Taro.Yamada`:               {Name: "Taro.Yamada", Home: `C:\Users\Taro.Yamada`},
		`PC\山田`:                     {Name: "山田", Home: `C:\Users\山田`},
		`PC\Administrator`:          {Name: "koya-Administrator", Home: `C:\Users\koya-Administrator`},
		`PC\ContainerAdministrator`: {Name: "koya-ContainerAdministrator", Home: `C:\Users\koya-ContainerAdministrator`},
		`a:b*c`:                     {Name: "a_b_c", Home: `C:\Users\a_b_c`},
		``:                          {Name: "user", Home: `C:\Users\user`},
	}
	for in, want := range cases {
		if got := WinUser(in); got != want {
			t.Errorf("WinUser(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestWinRunArgsHome(t *testing.T) {
	rt := runtime.ForWindows("docker.exe", "npipe:////./pipe/koya_docker")
	s := Spec{Profile: "claude", Workdir: `C:\Users\gcom0154\src\app`, Hash: "h",
		User: WinUser(`DOM\gcom0154`), Opts: Options{Home: `C:\Users\gcom0154\AppData\Local\koya\home-win\claude`}}
	args := RunArgs(rt, s)
	got := strings.Join(args, " ")
	home := strings.Index(got, `-v C:\Users\gcom0154\AppData\Local\koya\home-win\claude:C:\Users\gcom0154`)
	work := strings.Index(got, `-v C:\Users\gcom0154\src\app:C:\Users\gcom0154\src\app`)
	if home < 0 || work < 0 || home > work {
		t.Fatalf("永続ホームを作業ディレクトリより先にマウントすること: %s", got)
	}
	for _, want := range []string{
		`USERPROFILE=C:\Users\gcom0154`,
		`HOME=C:\Users\gcom0154`,
		`APPDATA=C:\Users\gcom0154\AppData\Roaming`,
		`LOCALAPPDATA=C:\Users\gcom0154\AppData\Local`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RunArgs に %q がありません", want)
		}
	}
	s.Opts.Home = ""
	if strings.Contains(strings.Join(RunArgs(rt, s), " "), "USERPROFILE") {
		t.Error("永続ホームを使わないときはホームを差し替えないこと")
	}
}

func TestWinShellFolders(t *testing.T) {
	cmds := winShellFolders(`C:\Users\me`)
	if len(cmds) != 3 {
		t.Fatalf("got %d commands", len(cmds))
	}
	if got := strings.Join(cmds[1], "|"); got != `reg.exe|add|HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders|/v|Local AppData|/t|REG_EXPAND_SZ|/d|C:\Users\me\AppData\Local|/f` {
		t.Fatalf("got %s", got)
	}
}
