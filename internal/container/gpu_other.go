//go:build !linux

package container

import "errors"

// DetectGPU は Linux 以外では使えない。
func DetectGPU() (*GPU, error) {
	return nil, errors.New("--gpu は Linux でだけ使えます")
}
