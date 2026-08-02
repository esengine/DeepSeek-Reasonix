//go:build !windows

package repair

import "syscall"

// processAlive reports whether a process with the given PID is still running.
// Used to distinguish an in-flight macOS update handoff (wait) from a stale
// prepared transaction (safe to cancel).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
