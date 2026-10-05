//go:build !windows

package neterr

import "syscall"

var resetErrors = []error{syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE}
