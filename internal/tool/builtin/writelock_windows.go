//go:build windows

package builtin

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isWindowsSharingViolation reports whether err is a Windows sharing or lock
// violation (another process holds the file open). The atomic rename in
// fileutil surfaces these as *os.LinkError, so errors.Is must unwrap.
func isWindowsSharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
