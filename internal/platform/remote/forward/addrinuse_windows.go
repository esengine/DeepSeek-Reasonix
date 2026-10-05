//go:build windows

package forward

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isAddrInUse reports whether err is an "address already in use" bind error.
// Windows reports WSAEADDRINUSE from the Winsock layer; the portable
// EADDRINUSE is checked too for defensiveness.
func isAddrInUse(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE) || errors.Is(err, syscall.EADDRINUSE)
}
