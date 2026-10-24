package sidecar

import (
	"errors"
	"syscall"
)

const errorBadExeFormat = syscall.Errno(193)

func notExecutableFormat(err error) bool {
	return errors.Is(err, errorBadExeFormat)
}
