package container

import "testing"

func TestWindowsToContainer(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\src\app`: "/c/Users/me/src/app",
		`d:\work\`:            "/d/work",
		`C:\`:                 "/c",
		`/already/unix`:       "/already/unix",
	}
	for in, want := range cases {
		if got := windowsToContainer(in); got != want {
			t.Errorf("windowsToContainer(%q) = %q, want %q", in, got, want)
		}
	}
}
