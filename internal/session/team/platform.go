package team

import (
	"errors"
	"runtime"
)

// ErrUnsupportedPlatform refuses seats without a protected host-state boundary.
var ErrUnsupportedPlatform = errors.New("team seats are disabled on Windows until roster and inbox authority are protected from child processes")

// CheckPlatform refuses team execution where disk-backed authority is unprotected.
func CheckPlatform() error { return checkPlatform(runtime.GOOS) }

func checkPlatform(goos string) error {
	if goos == "windows" {
		return ErrUnsupportedPlatform
	}
	return nil
}
