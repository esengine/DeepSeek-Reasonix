//go:build !windows

package main

import "syscall"

func isFileInUseErrno(errno syscall.Errno) bool {
	return errno == syscall.EBUSY || errno == syscall.ETXTBSY
}

func isAccessDeniedErrno(errno syscall.Errno) bool {
	return errno == syscall.EACCES || errno == syscall.EPERM
}

func isDiskFullErrno(errno syscall.Errno) bool {
	return errno == syscall.ENOSPC || errno == syscall.EDQUOT
}
