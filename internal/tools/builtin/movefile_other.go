//go:build !windows

package builtin

import (
	"errors"
	"syscall"
)

// isCrossDeviceMove reads the code the operating system returned; the message
// paired with it is locale-dependent and cannot be matched reliably.
var crossDeviceErrno error = syscall.EXDEV

func isCrossDeviceMove(err error) bool {
	return errors.Is(err, crossDeviceErrno)
}
