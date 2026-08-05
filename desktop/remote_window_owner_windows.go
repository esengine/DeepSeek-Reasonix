//go:build windows

package main

import (
	"context"
	"errors"

	"golang.org/x/sys/windows"
)

func waitForRemoteWindowOwnerExit(ctx context.Context, pid int) bool {
	if pid <= 0 {
		return true
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(handle)
	for {
		result, err := windows.WaitForSingleObject(handle, 250)
		if err != nil {
			return false
		}
		switch result {
		case windows.WAIT_OBJECT_0:
			return true
		case uint32(windows.WAIT_TIMEOUT):
			select {
			case <-ctx.Done():
				return false
			default:
			}
		default:
			return false
		}
	}
}
