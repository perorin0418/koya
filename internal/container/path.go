package container

import (
	goruntime "runtime"
	"strings"

	"github.com/perorin0418/koya/internal/runtime"
)

// ContainerPath はホストのパスを、コンテナ内で使うパスに変換する。
// Linux / macOS ではそのまま（ホストと同じ絶対パスにマウントする）。
// Windows では C:\Users\me\src → /c/Users/me/src のように変換する。
func ContainerPath(p string) string {
	if goruntime.GOOS == "windows" {
		return windowsToContainer(p)
	}
	return p
}

func windowsToContainer(p string) string {
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		rest := strings.TrimRight(strings.ReplaceAll(p[2:], `\`, "/"), "/")
		return "/" + strings.ToLower(p[:1]) + rest
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// WinContainerPath はホストのパスを、Windows コンテナ（koya run --win）内で使うパスに変換する。
// Windows コンテナの中には C: ドライブしかないため、C: のパスはそのまま、
// ほかのドライブは C:\mnt\<ドライブ名>\... に置く（例: D:\src\app → C:\mnt\d\src\app）。
func WinContainerPath(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, "/", `\`), `\`)
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		drive := strings.ToLower(p[:1])
		if drive == "c" {
			return "C:" + p[2:]
		}
		return `C:\mnt\` + drive + p[2:]
	}
	return p
}

// containerPathFor はランタイムに合わせてコンテナ内のパスを返す。
func containerPathFor(rt *runtime.Runtime, p string) string {
	if rt != nil && rt.Windows {
		return WinContainerPath(p)
	}
	return ContainerPath(p)
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// mountSuffix はバインドマウントのオプションを返す。
// SELinux のラベル付け替え（z）は Linux のホストでだけ付ける
// （Windows・macOS ではランタイムの VM が仲介するため不要で、付けると失敗する場合がある）。
func mountSuffix(ro bool) string {
	var opts []string
	if ro {
		opts = append(opts, "ro")
	}
	if goruntime.GOOS == "linux" {
		opts = append(opts, "z")
	}
	if len(opts) == 0 {
		return ""
	}
	return ":" + strings.Join(opts, ",")
}
