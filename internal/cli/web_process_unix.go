//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package cli

import (
	"errors"
	"syscall"
)

func webAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}

func webInstanceProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
