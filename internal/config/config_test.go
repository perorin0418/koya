package config

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", "/home/u")
	if err := os.MkdirAll(filepath.Join(dir, "koya"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "# comment\nhome.dir = ~/koya-home  # trailing\nhome.enable = claude, python\nhome.scope = project\ngpu = off\npkg-cache = off\npkg-cache.linux.dir = ~/koya-pkg-cache\n"
	if err := os.WriteFile(filepath.Join(dir, "koya", "config"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HomeDir != "/home/u/koya-home" || c.HomeScope != "project" {
		t.Fatalf("unexpected: %+v", c)
	}
	if !c.HomeEnabled("python") || c.HomeEnabled("go") {
		t.Fatalf("unexpected enable: %v", c.HomeEnable)
	}
	if c.GPU != "off" {
		t.Fatalf("unexpected gpu: %q", c.GPU)
	}
	if c.PkgCache || c.PkgCacheDir != "/home/u/koya-pkg-cache" {
		t.Fatalf("unexpected cache: %v %q", c.PkgCache, c.PkgCacheDir)
	}
}

func TestLoadMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c, err := Load()
	if err != nil || c.HomeScope != "shared" || c.GPU != "auto" || !c.PkgCache || c.PkgCacheDir != DefaultPkgCacheDir() || c.Path != "" {
		t.Fatalf("missing file must give defaults: %+v %v", c, err)
	}
	os.MkdirAll(filepath.Join(dir, "koya"), 0o755)
	os.WriteFile(filepath.Join(dir, "koya", "config"), []byte("nope = 1\n"), 0o644)
	if _, err := Load(); err == nil {
		t.Fatal("unknown key must be an error")
	}
}

func writeConf(t *testing.T, conf string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "koya"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "koya", "config"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestHomeRoot(t *testing.T) {
	writeConf(t, "home.linux.dir = /pool/home\n")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.HomeRoot(false) != "/pool/home" || c.HomeRoot(true) != "/pool/home-win" {
		t.Fatalf("linux=%q win=%q", c.HomeRoot(false), c.HomeRoot(true))
	}

	writeConf(t, "home.dir = /old/home\nhome.win.dir = /fast/win\n")
	if c, err = Load(); err != nil {
		t.Fatal(err)
	}
	if c.HomeRoot(false) != "/old/home" || c.HomeRoot(true) != "/fast/win" {
		t.Fatalf("linux=%q win=%q", c.HomeRoot(false), c.HomeRoot(true))
	}

	writeConf(t, "home.dir = /a\nhome.linux.dir = /b\n")
	if _, err := Load(); err == nil {
		t.Fatal("home.dir と home.linux.dir を両方書いたらエラーにすること")
	}
}

func TestLoadInvalidPkgCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.MkdirAll(filepath.Join(dir, "koya"), 0o755)
	os.WriteFile(filepath.Join(dir, "koya", "config"), []byte("pkg-cache = yes\n"), 0o644)
	if _, err := Load(); err == nil {
		t.Fatal("pkg-cache must be on or off")
	}
}

func TestDefaultPkgCacheDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/cache")
	if got := DefaultPkgCacheDir(); goruntime.GOOS == "linux" && got != filepath.Join("/cache", "koya", "pkg-cache") {
		t.Fatalf("DefaultPkgCacheDir() = %q", got)
	}
}
