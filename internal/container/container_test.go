package container

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

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
		{"", []string{`C:\Program Files\PowerShell\7\pwsh.exe`, "-NoLogo"}, "pwsh"},
		{"", []string{"claude.EXE"}, "claude"},
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

func TestPsQuote(t *testing.T) {
	cases := map[string]string{
		"":            "''",
		"claude":      "'claude'",
		"it's":        "'it''s'",
		"a\u2019b":    "'a\u2019\u2019b'",
		"$x \"y\" `z": "'$x \"y\" `z'",
	}
	for in, want := range cases {
		if got := psQuote(in); got != want {
			t.Errorf("psQuote(%q) = %q, want %q", in, got, want)
		}
	}
	if got := psArray(nil); got != "@()" {
		t.Errorf("psArray(nil) = %q", got)
	}
	if got := psArray([]string{"claude", "--resume"}); got != "@('claude', '--resume')" {
		t.Errorf("psArray = %q", got)
	}
}

// decodeWinPwsh は winPwsh のコマンドからスクリプトを取り出す。
func decodeWinPwsh(t *testing.T, cmd []string) string {
	t.Helper()
	if len(cmd) != 5 || cmd[0] != "pwsh.exe" || cmd[3] != "-EncodedCommand" {
		t.Fatalf("unexpected command: %v", cmd)
	}
	b, err := base64.StdEncoding.DecodeString(cmd[4])
	if err != nil || len(b)%2 != 0 {
		t.Fatalf("decode: %v", err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func TestWinSessionCommand(t *testing.T) {
	script := decodeWinPwsh(t, SessionCommand(true, "claude", []string{"claude", "--dangerously-skip-permissions", "--"}))
	for _, want := range []string{
		"$name = 'claude'\n",
		"$cmd = @('claude', '--dangerously-skip-permissions', '--')\n",
		"new-session -A -D -s $name -- @cmd",
		"$app.Extension -eq '.exe'",
		`C:\ProgramData\koya\tmux.conf`,
		"セッション機能を使えません",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script に %q がない:\n%s", want, script)
		}
	}
	script = decodeWinPwsh(t, AttachCommand(true, "work"))
	if !strings.Contains(script, "$name = 'work'\n") || !strings.Contains(script, `attach-session -d -t "=$name"`) {
		t.Errorf("attach script:\n%s", script)
	}
	if got := SessionCommand(false, "x", nil); got[0] != "sh" {
		t.Errorf("linux: %v", got)
	}
}

func TestDNFCacheScript(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh がありません")
	}
	confdir := filepath.Join(t.TempDir(), "libdnf5.conf.d")
	run := func() {
		t.Helper()
		if out, err := exec.Command(sh, "-c", dnfCacheScript, "koya-dnf-cache", confdir, DNFCacheDir).CombinedOutput(); err != nil {
			t.Fatalf("スクリプトが失敗しました: %v\n%s", err, out)
		}
	}

	// 追加ファイルのディレクトリがない（dnf5 でない）なら何もしない
	run()
	if _, err := os.Stat(confdir); !os.IsNotExist(err) {
		t.Fatalf("dnf5 でないのにディレクトリを作りました: %v", err)
	}

	if err := os.Mkdir(confdir, 0o755); err != nil {
		t.Fatal(err)
	}
	run()
	run() // 2回書いても同じ内容になる
	b, err := os.ReadFile(filepath.Join(confdir, "90-koya-pkg-cache.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "[main]\nsystem_cachedir=/var/cache/koya/dnf\nkeepcache=True\n"; string(b) != want {
		t.Errorf("設定ファイルの内容が違います:\n%s", b)
	}
}

func TestRunArgsPkgCache(t *testing.T) {
	rt := &runtime.Runtime{Kind: "podman", Rootless: true}
	u := User{Name: "me", UID: 1000, GID: 1000, Home: "/home/me"}
	s := Spec{Profile: "python", Workdir: "/src/app", Hash: "h", User: u,
		Opts: Options{PkgCache: "/mnt/pool/koya-pkg-cache/python", Env: []string{"UV_LINK_MODE=hardlink"}}}
	args := RunArgs(rt, s)
	got := strings.Join(args, " ")
	for _, want := range []string{
		"-v /mnt/pool/koya-pkg-cache/python:/var/cache/koya" + mountSuffix(false),
		"-e XDG_CACHE_HOME=/var/cache/koya",
		"-e npm_config_cache=/var/cache/koya/npm",
		"-e GOMODCACHE=/var/cache/koya/go-mod",
		"pkg-cache=/mnt/pool/koya-pkg-cache/python",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RunArgs に %q がありません: %s", want, got)
		}
	}
	// 利用者の -e はキャッシュの環境変数より後ろに置き、上書きできるようにする
	if strings.Index(got, "-e UV_LINK_MODE=copy") > strings.Index(got, "-e UV_LINK_MODE=hardlink") {
		t.Errorf("利用者の -e がキャッシュの環境変数より前にあります: %s", got)
	}

	s.Opts.PkgCache = ""
	if got := strings.Join(RunArgs(rt, s), " "); strings.Contains(got, PkgCacheMount) || strings.Contains(got, "pkg-cache=") {
		t.Errorf("キャッシュを使わないのにマウントがあります: %s", got)
	}
}
