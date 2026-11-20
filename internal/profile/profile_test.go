package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestParentOf(t *testing.T) {
	cases := map[string]string{
		"FROM localhost/koya/base\n":                                   "base",
		"FROM koya/python:latest AS b\n":                               "python",
		"FROM docker.io/library/node AS n\nFROM localhost/koya/base\n": "base",
		"FROM --platform=linux/amd64 localhost/koya/go\n":              "go",
		"FROM debian:stable-slim\n":                                    "",
	}
	for in, want := range cases {
		if got := parentOf([]byte(in)); got != want {
			t.Errorf("parentOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashChangesWithParent(t *testing.T) {
	mk := func(base string) *Store {
		fsys := fstest.MapFS{
			"profiles-linux/base/Dockerfile":  {Data: []byte(base)},
			"profiles-linux/child/Dockerfile": {Data: []byte("FROM localhost/koya/base\n")},
		}
		s := NewStore(fsys)
		s.userDirs = nil
		return s
	}
	h1, err := mk("FROM debian\n").Hash("child")
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := mk("FROM debian\nRUN true\n").Hash("child")
	if h1 == h2 {
		t.Fatal("child hash must change when parent changes")
	}
}

func TestCycle(t *testing.T) {
	s := NewStore(fstest.MapFS{
		"profiles-linux/a/Dockerfile": {Data: []byte("FROM localhost/koya/b\n")},
		"profiles-linux/b/Dockerfile": {Data: []byte("FROM localhost/koya/a\n")},
	})
	s.userDirs = nil
	if _, err := s.Hash("a"); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestWinStore(t *testing.T) {
	fsys := fstest.MapFS{
		"profiles-linux/base/Dockerfile": {Data: []byte("FROM fedora\n")},
		"profiles-win/base/Dockerfile":   {Data: []byte("ARG BASE_TAG\nFROM mcr.microsoft.com/windows/server:${BASE_TAG}\n")},
		"profiles-win/dev/Dockerfile":    {Data: []byte("FROM localhost/koya/base\n")},
	}
	s := NewWinStore(fsys)
	s.userDirs = nil
	if got := strings.Join(s.Names(), ","); got != "base,dev" {
		t.Fatalf("Names() = %s", got)
	}
	p, err := s.Get("base")
	if err != nil || !strings.Contains(string(p.Files["Dockerfile"]), "windows/server") {
		t.Fatalf("Get(base) = %v, %v", p, err)
	}
	s.BuildArgs = []string{"BASE_TAG=ltsc2022"}
	h1, _ := s.Hash("dev")
	s2 := NewWinStore(fsys)
	s2.userDirs = nil
	s2.BuildArgs = []string{"BASE_TAG=ltsc2025"}
	h2, _ := s2.Hash("dev")
	if h1 == "" || h1 == h2 {
		t.Fatalf("hash must change with build args: %s %s", h1, h2)
	}
}

func TestUsedBuildArgs(t *testing.T) {
	args := []string{"BASE_TAG=ltsc2025", "OTHER=1"}
	base := []byte("# escape=`\nARG BASE_TAG=ltsc2022\nFROM mcr.microsoft.com/windows/server:${BASE_TAG}\n")
	if got := strings.Join(usedBuildArgs(base, args), ","); got != "BASE_TAG=ltsc2025" {
		t.Errorf("base: %q", got)
	}
	child := []byte("FROM localhost/koya/base\nRUN echo BASE_TAG\n")
	if got := usedBuildArgs(child, args); len(got) != 0 {
		t.Errorf("child: %q", got)
	}
	lower := []byte("from x\n  arg OTHER\n")
	if got := strings.Join(usedBuildArgs(lower, args), ","); got != "OTHER=1" {
		t.Errorf("lower: %q", got)
	}
}

func TestUserDirsLegacy(t *testing.T) {
	cur, legacy := t.TempDir(), t.TempDir()
	write := func(dir, name, body string) {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "Dockerfile"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(cur, "both", "FROM localhost/koya/base\n# cur\n")
	write(legacy, "both", "FROM localhost/koya/base\n# legacy\n")
	write(legacy, "old", "FROM localhost/koya/base\n")

	s := NewStore(fstest.MapFS{"profiles-linux/base/Dockerfile": {Data: []byte("FROM fedora\n")}})
	s.userDirs = []string{cur, legacy}
	if got := strings.Join(s.Names(), ","); got != "base,both,old" {
		t.Fatalf("Names() = %s", got)
	}
	p, err := s.Get("both")
	if err != nil || p.Source != User || !strings.Contains(string(p.Files["Dockerfile"]), "# cur") {
		t.Fatalf("Get(both) must prefer the current dir: %v, %v", p, err)
	}
	p, err = s.Get("old")
	if err != nil || p.Source != User || p.Path != filepath.Join(legacy, "old") {
		t.Fatalf("Get(old) must read the legacy dir: %v, %v", p, err)
	}
	if s.UserDir() != cur {
		t.Fatalf("UserDir() = %s", s.UserDir())
	}
}

func TestExternalImages(t *testing.T) {
	cases := []struct {
		name string
		df   string
		args []string
		want string
	}{
		{"multi-stage", "FROM docker.io/library/golang:latest AS golang\n" +
			"FROM docker.io/library/maven:3 AS maven\n" +
			"FROM localhost/koya/claude\n" +
			"COPY --from=maven /opt/java /opt/java\n" +
			"COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/\n" +
			"COPY --chown=1:1 --from=golang /usr/local/go /usr/local/go\n",
			nil, "docker.io/library/golang:latest,docker.io/library/maven:3,ghcr.io/astral-sh/uv:latest"},
		{"build arg", "# escape=`\nARG BASE_TAG=ltsc2022\nFROM mcr.microsoft.com/windows/server:${BASE_TAG}\n",
			[]string{"BASE_TAG=ltsc2025"}, "mcr.microsoft.com/windows/server:ltsc2025"},
		{"arg default", "ARG V=3.13\nFROM python:$V-slim\n", nil, "python:3.13-slim"},
		{"skip", "FROM --platform=linux/amd64 koya/base\nFROM scratch AS out\nFROM ${UNKNOWN}\nCOPY --from=0 /a /b\nCOPY --from=out /a /b\n",
			nil, ""},
		{"continuation", "FROM \\\n  fedora:latest\n# comment \\\nRUN echo\n", nil, "fedora:latest"},
		{"dedupe", "FROM a:1 AS x\nFROM a:1\nCOPY --from=a:1 /a /b\n", nil, "a:1"},
	}
	for _, c := range cases {
		if got := strings.Join(ExternalImages([]byte(c.df), c.args), ","); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
