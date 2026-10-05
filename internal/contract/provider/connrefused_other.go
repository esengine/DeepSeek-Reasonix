//go:build !windows

package provider

import (
	"errors"
	"syscall"
)

func connectionRefused(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }
