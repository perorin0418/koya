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

func TestWinHomeMountpoint(t *testing.T) {
	cases := []struct{ home, wd, want string }{
		{`C:\Users\gcom0154`, `C:\Users\gcom0154\src\koya-b`, `src\koya-b`},
		{`C:\Users\gcom0154`, `c:\users\GCOM0154\src\app\`, `src\app`},
		{`C:\Users\gcom0154`, `C:\Users\gcom01540\src`, ``},
		{`C:\Users\gcom0154`, `C:\tmp`, ``},
		{`C:\Users\gcom0154`, `D:\src\app`, ``},
	}
	for _, c := range cases {
		if got := WinHomeMountpoint(c.home, c.wd); got != c.want {
			t.Errorf("WinHomeMountpoint(%q, %q) = %q, want %q", c.home, c.wd, got, c.want)
		}
	}
}
