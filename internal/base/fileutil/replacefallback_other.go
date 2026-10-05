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
