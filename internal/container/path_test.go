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

func TestWinContainerPath(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\src`:    `C:\Users\me\src`,
		`c:\src\app\`:        `C:\src\app`,
		`D:\workspace\git\x`: `C:\mnt\d\workspace\git\x`,
		`E:/data/proj`:       `C:\mnt\e\data\proj`,
	}
	for in, want := range cases {
		if got := WinContainerPath(in); got != want {
			t.Errorf("WinContainerPath(%q) = %q, want %q", in, got, want)
		}
	}
}
