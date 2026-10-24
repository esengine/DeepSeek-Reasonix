//go:build !windows

package worktree

import "syscall"

// processUmask is read once while package initialization is single-threaded:
// reading it means setting it, and that must not race file creation.
var processUmask = func() uint32 {
	mask := syscall.Umask(0)
	syscall.Umask(mask)
	return uint32(mask)
}()
