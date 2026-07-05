//go:build !windows

package agent

import "syscall"

// processAlive reports whether the given PID is still running.
func processAlive(pid int) bool {
	// kill(pid, 0) sends no signal but checks whether the process exists.
	return syscall.Kill(pid, 0) == nil
}
