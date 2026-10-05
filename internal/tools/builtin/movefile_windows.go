//go:build windows

package builtin

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isCrossDeviceMove reads the code the operating system returned. The message
// Windows pairs with it is localized, so a rename across volumes is only
// recognizable by its text on an English install.
var crossDeviceErrno error = windows.ERROR_NOT_SAME_DEVICE

func isCrossDeviceMove(err error) bool {
	return errors.Is(err, crossDeviceErrno)
}
