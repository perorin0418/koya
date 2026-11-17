//go:build windows

package winengine

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	ntdll    = syscall.NewLazyDLL("ntdll.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRtlGetVersion      = ntdll.NewProc("RtlGetVersion")
	procOpenSCManagerW     = advapi32.NewProc("OpenSCManagerW")
	procOpenServiceW       = advapi32.NewProc("OpenServiceW")
	procQueryServiceStatus = advapi32.NewProc("QueryServiceStatus")
	procStartServiceW      = advapi32.NewProc("StartServiceW")
	procControlService     = advapi32.NewProc("ControlService")
	procCloseServiceHandle = advapi32.NewProc("CloseServiceHandle")
)

const (
	scManagerConnect     = 0x0001
	serviceQueryStatus   = 0x0004
	serviceStart         = 0x0010
	serviceStop          = 0x0020
	serviceControlStop   = 0x00000001
	errorServiceNotExist = syscall.Errno(1060)
	errorServiceRunning  = syscall.Errno(1056)
	errorServiceInactive = syscall.Errno(1062)
	tokenElevation       = 20
)

// サービスの状態（SERVICE_STATUS.dwCurrentState）
const (
	stateStopped = 1
	stateRunning = 4
)

type osVersionInfo struct {
	size, major, minor, build, platform uint32
	csd                                 [128]uint16
	spMajor, spMinor, suite             uint16
	productType, reserved               byte
}

// hostBuild はホストの OS のビルド番号を返す（互換モードの影響を受けない RtlGetVersion を使う）。
func hostBuild() (int, error) {
	var v osVersionInfo
	v.size = uint32(unsafe.Sizeof(v))
	if r, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v))); r != 0 {
		return 0, fmt.Errorf("OS のバージョンを取得できません（0x%x）", r)
	}
	return int(v.build), nil
}

// isAdmin は管理者として（昇格して）動いているかを返す。
func isAdmin() bool {
	p, err := syscall.GetCurrentProcess()
	if err != nil {
		return false
	}
	var t syscall.Token
	if err := syscall.OpenProcessToken(p, syscall.TOKEN_QUERY, &t); err != nil {
		return false
	}
	defer t.Close()
	var elevated uint32
	var n uint32
	if err := syscall.GetTokenInformation(t, tokenElevation, (*byte)(unsafe.Pointer(&elevated)), 4, &n); err != nil {
		return false
	}
	return elevated != 0
}

func openService(name string, access uint32) (uintptr, func(), error) {
	m, _, e := procOpenSCManagerW.Call(0, 0, scManagerConnect)
	if m == 0 {
		return 0, nil, fmt.Errorf("サービス管理につなげません: %w", e)
	}
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		procCloseServiceHandle.Call(m)
		return 0, nil, err
	}
	s, _, e := procOpenServiceW.Call(m, uintptr(unsafe.Pointer(n)), uintptr(access))
	if s == 0 {
		procCloseServiceHandle.Call(m)
		return 0, nil, e
	}
	return s, func() { procCloseServiceHandle.Call(s); procCloseServiceHandle.Call(m) }, nil
}

// serviceState はサービスの状態を返す。サービスがなければ exists=false。
func serviceState(name string) (state uint32, exists bool, err error) {
	s, done, err := openService(name, serviceQueryStatus)
	if errors.Is(err, errorServiceNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer done()
	var st [7]uint32 // SERVICE_STATUS
	if r, _, e := procQueryServiceStatus.Call(s, uintptr(unsafe.Pointer(&st[0]))); r == 0 {
		return 0, true, e
	}
	return st[1], true, nil
}

// startService はサービスを起動する（管理者権限が必要）。起動済みなら何もしない。
func startService(name string) error {
	s, done, err := openService(name, serviceStart)
	if err != nil {
		return err
	}
	defer done()
	if r, _, e := procStartServiceW.Call(s, 0, 0); r == 0 && !errors.Is(e, errorServiceRunning) {
		return e
	}
	return nil
}

// stopService はサービスを止める（管理者権限が必要）。止まっていれば何もしない。
func stopService(name string) error {
	s, done, err := openService(name, serviceStop)
	if err != nil {
		return err
	}
	defer done()
	var st [7]uint32
	if r, _, e := procControlService.Call(s, serviceControlStop, uintptr(unsafe.Pointer(&st[0]))); r == 0 && !errors.Is(e, errorServiceInactive) {
		return e
	}
	return nil
}
