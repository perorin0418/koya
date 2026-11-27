package container

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/perorin0418/koya/internal/runtime"
)

// fakeRuntime は呼ばれた引数を 1 行ずつ記録し、--init を付けた run だけ tini がないと言って失敗するランタイムを作る。
func fakeRuntime(t *testing.T, kind string) (*runtime.Runtime, string) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("シェルスクリプトで偽のランタイムを作るため")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	bin := filepath.Join(dir, kind)
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" +
		"case \" $* \" in *' --init '*) echo 'exec: \"tini\": executable file not found in $PATH' >&2; exit 1;; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &runtime.Runtime{Bin: bin, Kind: kind}, log
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestRunInitFallsBackWithoutTiniOnNerdctl(t *testing.T) {
	rt, log := fakeRuntime(t, "nerdctl")
	if err := runInit(rt, "c1", []string{"run", "-d", "--name", "c1", "img"}); err != nil {
		t.Fatalf("must succeed without --init: %v", err)
	}
	got := readLines(t, log)
	want := []string{"run --init -d --name c1 img", "rm -f c1", "run -d --name c1 img"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRunInitKeepsErrorOnOtherRuntimes(t *testing.T) {
	rt, log := fakeRuntime(t, "docker")
	if err := runInit(rt, "c1", []string{"run", "-d", "img"}); err == nil {
		t.Fatal("docker must not fall back")
	}
	if got := readLines(t, log); len(got) != 1 {
		t.Errorf("must run once: %q", got)
	}
}
