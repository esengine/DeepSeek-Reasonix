package config

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func init() {
	removalRaceErrors = append(removalRaceErrors, windows.ERROR_SHARING_VIOLATION, windows.ERROR_ACCESS_DENIED)
}

func TestVanishedMidWalkKnowsWindowsDeleteErrnos(t *testing.T) {
	gone := t.TempDir() + `\gone`
	for _, errno := range []syscall.Errno{windows.ERROR_SHARING_VIOLATION, windows.ERROR_ACCESS_DENIED} {
		err := &fs.PathError{Op: "open", Path: gone, Err: errno}
		if !errors.Is(err, errno) || !vanishedMidWalk(gone, err) {
			t.Errorf("%v on a removed directory must count as vanished", errno)
		}
	}
}
