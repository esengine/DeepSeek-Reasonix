//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package mcptrust

import "syscall"

func trustLockProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func trustLockContention(error) bool { return false }
