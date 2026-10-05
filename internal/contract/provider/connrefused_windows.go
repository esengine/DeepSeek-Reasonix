package provider

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// Winsock reports a refused connect as WSAECONNREFUSED, which syscall's
// portable ECONNREFUSED does not match.
func connectionRefused(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, syscall.ECONNREFUSED)
}
