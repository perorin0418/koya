package runtime

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTraceLogsCommandAndHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	oldV, oldOut, oldHB := Verbose, verboseOut, heartbeat
	Verbose, verboseOut, heartbeat = true, &buf, 20*time.Millisecond
	t.Cleanup(func() { Verbose, verboseOut, heartbeat = oldV, oldOut, oldHB })

	r := &Runtime{Bin: "/usr/bin/sleep", Kind: "podman"}
	if err := r.Run("0.1"); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"koya: [詳細] 実行: sleep 0.1", "実行中（", "完了（"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q が含まれていません:\n%s", want, got)
		}
	}
}

func TestTraceSilentWhenNotVerbose(t *testing.T) {
	var buf bytes.Buffer
	oldV, oldOut := Verbose, verboseOut
	Verbose, verboseOut = false, &buf
	t.Cleanup(func() { Verbose, verboseOut = oldV, oldOut })

	r := &Runtime{Bin: "/bin/true"}
	_, _ = r.Output("x")
	if buf.Len() != 0 {
		t.Errorf("詳細ログが出ています: %s", buf.String())
	}
}

func TestCommandLineQuotesAndShortens(t *testing.T) {
	r := &Runtime{Bin: "/usr/bin/podman", Prefix: []string{"-H", "x"}}
	got := r.commandLine([]string{"exec", "a b", "line1\nline2", ""})
	want := `podman -H x exec "a b" <11 文字の引数> ""`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestKindOf(t *testing.T) {
	for bin, want := range map[string]string{
		"podman":                 "podman",
		"/usr/bin/podman-remote": "podman",
		"docker":                 "docker",
		"nerdctl":                "nerdctl",
		`C:\bin\nerdctl.exe`:     "nerdctl",
		"/usr/local/bin/nerdctl": "nerdctl",
	} {
		if got := KindOf(bin); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", bin, got, want)
		}
	}
}
