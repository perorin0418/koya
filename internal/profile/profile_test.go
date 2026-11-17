package profile

import (
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
			"profiles/base/Dockerfile":  {Data: []byte(base)},
			"profiles/child/Dockerfile": {Data: []byte("FROM localhost/koya/base\n")},
		}
		s := NewStore(fsys)
		s.userDir = ""
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
		"profiles/a/Dockerfile": {Data: []byte("FROM localhost/koya/b\n")},
		"profiles/b/Dockerfile": {Data: []byte("FROM localhost/koya/a\n")},
	})
	s.userDir = ""
	if _, err := s.Hash("a"); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestWinStore(t *testing.T) {
	fsys := fstest.MapFS{
		"profiles/base/Dockerfile":     {Data: []byte("FROM fedora\n")},
		"profiles-win/base/Dockerfile": {Data: []byte("ARG BASE_TAG\nFROM mcr.microsoft.com/windows/servercore:${BASE_TAG}\n")},
		"profiles-win/dev/Dockerfile":  {Data: []byte("FROM localhost/koya/base\n")},
	}
	s := NewWinStore(fsys)
	s.userDir = ""
	if got := strings.Join(s.Names(), ","); got != "base,dev" {
		t.Fatalf("Names() = %s", got)
	}
	p, err := s.Get("base")
	if err != nil || !strings.Contains(string(p.Files["Dockerfile"]), "servercore") {
		t.Fatalf("Get(base) = %v, %v", p, err)
	}
	s.BuildArgs = []string{"BASE_TAG=ltsc2022"}
	h1, _ := s.Hash("dev")
	s2 := NewWinStore(fsys)
	s2.userDir = ""
	s2.BuildArgs = []string{"BASE_TAG=ltsc2025"}
	h2, _ := s2.Hash("dev")
	if h1 == "" || h1 == h2 {
		t.Fatalf("hash must change with build args: %s %s", h1, h2)
	}
}
