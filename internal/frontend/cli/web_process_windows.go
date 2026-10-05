//go:build windows

package cli

import (
	"errors"

	"golang.org/x/sys/windows"
)

const webInstanceStillActiveExitCode = 259

func webAddressInUse(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE)
}

func webInstanceProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(handle)
	var code uint32
	return windows.GetExitCodeProcess(handle, &code) == nil && code == webInstanceStillActiveExitCode
}
