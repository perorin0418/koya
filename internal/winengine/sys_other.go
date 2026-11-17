//go:build !windows

package winengine

import "errors"

var errNotWindows = errors.New("--win（Windows コンテナ）は Windows 版の koya でだけ使えます（WSL の中の koya からは使えません）")

const (
	stateStopped = 1
	stateRunning = 4
)

func hostBuild() (int, error)                   { return 0, errNotWindows }
func isAdmin() bool                             { return false }
func serviceState(string) (uint32, bool, error) { return 0, false, errNotWindows }
func startService(string) error                 { return errNotWindows }
func stopService(string) error                  { return errNotWindows }
