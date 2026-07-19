//go:build windows

package mcptrust

import (
	"errors"

	"golang.org/x/sys/windows"
)

const trustLockStillActiveExitCode = 259

func trustLockProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied still proves the process exists. Treating it as stale
		// would let a less-privileged waiter steal a live writer's lock.
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(handle)
	var code uint32
	return windows.GetExitCodeProcess(handle, &code) == nil && code == trustLockStillActiveExitCode
}

// trustLockContention reports transient Windows name/handle races while one
// owner removes the exclusive-create lock and another tries to create it.
// OpenFile can surface those races as access or sharing violations instead of
// os.ErrExist, so callers must retry them within the normal lock deadline.
func trustLockContention(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
