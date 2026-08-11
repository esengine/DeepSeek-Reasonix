//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package mcplaunch

import "syscall"

func launchLockProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func launchLockContention(error) bool { return false }
