package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", "/home/u")
	if err := os.MkdirAll(filepath.Join(dir, "koya"), 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "# comment\nhome.dir = ~/koya-home  # trailing\nhome.enable = claude, python\nhome.scope = project\n"
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
}

func TestLoadMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	c, err := Load()
	if err != nil || c.HomeScope != "shared" || c.Path != "" {
		t.Fatalf("missing file must give defaults: %+v %v", c, err)
	}
	os.MkdirAll(filepath.Join(dir, "koya"), 0o755)
	os.WriteFile(filepath.Join(dir, "koya", "config"), []byte("nope = 1\n"), 0o644)
	if _, err := Load(); err == nil {
		t.Fatal("unknown key must be an error")
	}
}
