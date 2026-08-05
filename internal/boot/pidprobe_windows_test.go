//go:build windows

package boot

import (
	"errors"

	"golang.org/x/sys/windows"
)

// pidAlive reports whether pid still exists. ERROR_ACCESS_DENIED means the
// process exists but we lack query rights, so it counts as alive; every
// other OpenProcess failure (including ERROR_INVALID_PARAMETER for a dead
// pid) means gone.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}
