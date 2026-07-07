//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func isFileInUseErrno(errno syscall.Errno) bool {
	return errno == windows.ERROR_SHARING_VIOLATION || errno == windows.ERROR_LOCK_VIOLATION
}

func isAccessDeniedErrno(errno syscall.Errno) bool {
	return errno == windows.ERROR_ACCESS_DENIED
}

func isDiskFullErrno(errno syscall.Errno) bool {
	return errno == windows.ERROR_DISK_FULL || errno == windows.ERROR_HANDLE_DISK_FULL
}
