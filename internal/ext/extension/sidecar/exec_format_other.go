//go:build !windows

package sidecar

import (
	"errors"
	"syscall"
)

func notExecutableFormat(err error) bool {
	return errors.Is(err, syscall.ENOEXEC)
}
