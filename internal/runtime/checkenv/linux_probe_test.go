//go:build linux

package checkenv

import "syscall"

func detachProbe() error { _, err := syscall.Setsid(); return err }
