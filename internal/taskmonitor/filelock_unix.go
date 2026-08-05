//go:build !windows

package taskmonitor

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockTaskFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_EX) }

func unlockTaskFile(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }
