package main

import "testing"

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
	f.Set("true")
	if !f.enabled || f.name != "" {
		t.Fatalf("--session: %+v", f)
	}
	f.Set("work")
	if !f.enabled || f.name != "work" {
		t.Fatalf("--session=work: %+v", f)
	}
	f.Set("false")
	if f.enabled {
		t.Fatalf("--session=false: %+v", f)
	}
}
