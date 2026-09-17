//go:build !windows

package fileutil

import (
	"errors"
	"syscall"
)

// renameCrossesDevice reports whether a rename failed with EXDEV: the two
// paths sit on different filesystems, so no retry can make the rename succeed
// and ReplaceFile's copy fallback is the only option. AtomicWriteFile creates
// tmp next to dest, so this only happens when something (an overlay or bind
// mount boundary, or a filter driver on Windows) splits the directory across
// devices.
func renameCrossesDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

// replaceWithBackup is a no-op on non-Windows platforms: rename either
// succeeds immediately or fails structurally (EXDEV) and is handled by
// the copyOnto fallback in ReplaceFile. Transient locking that survives
// all retries does not occur on Linux/macOS because rename replaces the
// directory entry without touching the inode, so a concurrent reader
// holding the old inode handle is unaffected.
func replaceWithBackup(tmp, dest string, origErr error) error {
	return origErr
}
