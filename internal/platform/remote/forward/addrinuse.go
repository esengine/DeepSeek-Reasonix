//go:build !windows

package forward

import (
	"errors"
	"syscall"
)

// isAddrInUse reports whether err is an "address already in use" bind error.
// Unix reports EADDRINUSE.
func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
