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
