//go:build windows

package fileutil

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// renameCrossesDevice reports whether a rename failed because the filesystem
// treats the two paths as different devices. Encryption-software filter
// drivers report ERROR_NOT_SAME_DEVICE even for a same-directory rename
// (#2696); such a rename fails identically on every retry, so ReplaceFile
// goes straight to its copy fallback instead of burning the retry backoff.
func renameCrossesDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) || errors.Is(err, syscall.EXDEV)
}
