package main

import (
	"strings"
	"testing"

	"github.com/perorin0418/koya/internal/runtime"
)

func TestWindowsUser(t *testing.T) {
	cases := map[string]string{
		`DOMAIN\Taro.Yamada`: "taro.yamada",
		`PC\山田 太郎`:           "user",
		`me`:                 "me",
		`CORP\1st-user`:      "u1st-user",
	}
	for in, want := range cases {
		u := windowsUser(in)
		if u.Name != want || u.UID != 1000 || u.GID != 1000 || u.Home != "/home/"+want {
			t.Errorf("windowsUser(%q) = %+v, want name %q", in, u, want)
		}
	}
}

func TestSessionFlag(t *testing.T) {
	var f sessionFlag
	if f.set {
		t.Fatalf("未指定: %+v", f)
	}
	f.Set("true")
	if !f.enabled || f.name != "" {
		t.Fatalf("--session: %+v", f)
	}
	f.Set("work")
	if !f.enabled || f.name != "work" {
		t.Fatalf("--session=work: %+v", f)
	}
	f.Set("false")
	if f.enabled || !f.set {
		t.Fatalf("--session=false: %+v", f)
	}
}

func TestStripVerbose(t *testing.T) {
	old := runtime.Verbose
	t.Cleanup(func() { runtime.Verbose = old })
	runtime.Verbose = false
	got := stripVerbose([]string{"run", "--rm", "--verbose", "mycc", "--", "echo", "--verbose"})
	want := []string{"run", "--rm", "mycc", "--", "echo", "--verbose"}
	if strings.Join(got, " ") != strings.Join(want, " ") || !runtime.Verbose {
		t.Errorf("got %v (Verbose=%v), want %v", got, runtime.Verbose, want)
	}
}
