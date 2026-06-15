//go:build windows

package cli

import "syscall"

// hideFileWindows sets the FILE_ATTRIBUTE_HIDDEN flag on the given path so
// stale .old binaries left behind by self-update don't clutter the directory.
// Best-effort: errors are silently ignored.
func hideFileWindows(path string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	// FILE_ATTRIBUTE_HIDDEN = 0x02
	syscall.SetFileAttributes(p, 0x02)
}
