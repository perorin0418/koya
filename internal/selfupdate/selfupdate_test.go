package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.3.0", "v0.4.0", -1, true},
		{"v0.10.0", "v0.9.9", 1, true},
		{"v1.2.3", "1.2.3", 0, true},
		{"v0.4.0-dev+abc", "v0.4.0", 0, true},
		{"dev", "v0.4.0", 0, false},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d,%v want %d,%v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("v0.4.0", "linux", "amd64"); got != "koya_0.4.0_linux_amd64.tar.gz" {
		t.Error(got)
	}
	if got := AssetName("v0.4.0", "windows", "arm64"); got != "koya_0.4.0_windows_arm64.zip" {
		t.Error(got)
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte("abc123  koya_0.4.0_linux_amd64.tar.gz\nDEF456 *koya_0.4.0_windows_amd64.zip\n")
	if s, ok := checksumFor(sums, "koya_0.4.0_linux_amd64.tar.gz"); !ok || s != "abc123" {
		t.Error(s, ok)
	}
	if s, ok := checksumFor(sums, "koya_0.4.0_windows_amd64.zip"); !ok || s != "def456" {
		t.Error(s, ok)
	}
	if _, ok := checksumFor(sums, "nope"); ok {
		t.Error("unexpected match")
	}
}

func TestExtract(t *testing.T) {
	var tgz bytes.Buffer
	gz := gzip.NewWriter(&tgz)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"d/README.md": "readme", "d/koya": "BIN"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	if b, err := extract(tgz.Bytes(), "x.tar.gz"); err != nil || string(b) != "BIN" {
		t.Fatalf("tar: %q %v", b, err)
	}

	var zb bytes.Buffer
	zw := zip.NewWriter(&zb)
	w, _ := zw.Create("d/koya.exe")
	w.Write([]byte("EXE"))
	zw.Close()
	if b, err := extract(zb.Bytes(), "x.zip"); err != nil || string(b) != "EXE" {
		t.Fatalf("zip: %q %v", b, err)
	}
}

func TestReplace(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "koya")
	os.WriteFile(exe, []byte("old"), 0o755)
	if err := Replace(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(exe)
	st, _ := os.Stat(exe)
	if string(b) != "new" || st.Mode().Perm()&0o111 == 0 {
		t.Fatalf("content %q mode %v", b, st.Mode())
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".koya-update-*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
