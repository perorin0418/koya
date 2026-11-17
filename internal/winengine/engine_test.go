package winengine

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaseFor(t *testing.T) {
	t.Setenv("KOYA_WIN_BASE_TAG", "")
	t.Setenv("KOYA_WIN_ISOLATION", "")
	cases := []struct {
		build     int
		tag, iso  string
		wantError bool
	}{
		{26100, "ltsc2025", "process", false},
		{22631, "ltsc2022", "process", false},
		{20348, "ltsc2022", "process", false},
		{19045, "ltsc2019", "hyperv", false},
		{17763, "ltsc2019", "process", false},
		{14393, "", "", true},
	}
	for _, c := range cases {
		b, err := BaseFor(c.build)
		if (err != nil) != c.wantError || b.Tag != c.tag || b.Isolation != c.iso {
			t.Errorf("BaseFor(%d) = %+v, %v", c.build, b, err)
		}
	}
}

func TestBaseForOverride(t *testing.T) {
	t.Setenv("KOYA_WIN_BASE_TAG", "ltsc2019")
	t.Setenv("KOYA_WIN_ISOLATION", "hyperv")
	b, err := BaseFor(14393)
	if err != nil || b.Tag != "ltsc2019" || b.Isolation != "hyperv" {
		t.Fatalf("got %+v, %v", b, err)
	}
	t.Setenv("KOYA_WIN_ISOLATION", "bogus")
	if _, err := BaseFor(26100); err == nil {
		t.Fatal("invalid isolation must fail")
	}
}

func TestQuoteArg(t *testing.T) {
	cases := map[string]string{
		`abc`:                `abc`,
		``:                   `""`,
		`a b`:                `"a b"`,
		`C:\Program Files\x`: `"C:\Program Files\x"`,
		`a"b`:                `"a\"b"`,
		`dir\ `:              `"dir\ "`,
		`end\`:               `end\`,
		`sp ace\`:            `"sp ace\\"`,
	}
	for in, want := range cases {
		if got := QuoteArg(in); got != want {
			t.Errorf("QuoteArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestElevateScript(t *testing.T) {
	got := ElevateScript(`C:\Users\o'neil\koya.exe`, []string{"win", "_install", "--user", `DOM\taro`, "--log", `C:\T\a b.log`})
	want := `$p = Start-Process -FilePath 'C:\Users\o''neil\koya.exe' -ArgumentList 'win _install --user DOM\taro --log "C:\T\a b.log"' -Verb RunAs -Wait -PassThru; exit $p.ExitCode`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestDaemonArgs(t *testing.T) {
	p := PathsFor(`C:\Program Files`, `C:\ProgramData`)
	got := strings.Join(p.DaemonArgs(`DOM\taro`), " ")
	for _, want := range []string{
		"--register-service --service-name koya-docker",
		"-H npipe:////./pipe/koya_docker",
		"--data-root " + filepath.Join(`C:\ProgramData`, "koya", "docker"),
		"--pidfile " + filepath.Join(`C:\ProgramData`, "koya", "docker.pid"),
		"--config-file " + filepath.Join(`C:\ProgramData`, "koya", "daemon.json"),
		`--group DOM\taro`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("DaemonArgs に %q がありません: %s", want, got)
		}
	}
	if !strings.HasSuffix(p.Dockerd(), filepath.Join("koya", "docker-"+DockerVersion, "dockerd.exe")) {
		t.Errorf("unexpected dockerd path: %s", p.Dockerd())
	}
}

func TestVerify(t *testing.T) {
	if err := Verify([]byte("not the real zip")); err == nil {
		t.Fatal("wrong archive must fail verification")
	}
}

func makeZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("bin:" + n))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	z := makeZip(t, "docker/", "docker/dockerd.exe", "docker/docker.exe", "docker/README.md")
	if err := Extract(z, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "dockerd.exe"))
	if err != nil || string(b) != "bin:docker/dockerd.exe" {
		t.Fatalf("dockerd.exe: %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Fatal("only .exe files should be extracted")
	}
	if err := Extract(makeZip(t, "docker/docker.exe"), t.TempDir()); err == nil {
		t.Fatal("missing dockerd.exe must fail")
	}
}
