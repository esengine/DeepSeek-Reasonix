//go:build !windows

package boot

import (
	"errors"
	"syscall"
)

// pidAlive reports whether pid still exists (zombies count as alive: the
// process table entry only disappears once the host reaps it).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return !errors.Is(err, syscall.ESRCH)
}
