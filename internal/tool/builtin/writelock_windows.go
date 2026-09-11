//go:build windows

package builtin

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// isWindowsFileLocked reports whether err is a Windows error class that a
// locked or unwritable target surfaces through the atomic rename. The rename
// step reports *os.LinkError, so a plain ERROR_ACCESS_DENIED (WinError 5) can
// be narrowed to the rename instead of any os.MkdirAll/CreateTemp path error.
func isWindowsFileLocked(err error) bool {
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true
	}
	var linkErr *os.LinkError
	return errors.As(err, &linkErr) && errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
