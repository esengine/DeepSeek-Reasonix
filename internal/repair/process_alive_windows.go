//go:build windows

package repair

import "golang.org/x/sys/windows"

const processStillActiveExitCode = 259 // STILL_ACTIVE

// processAlive reports whether a process with the given PID is still running.
// Used to distinguish an in-flight update handoff (wait) from a stale prepared
// transaction (safe to cancel).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	return windows.GetExitCodeProcess(handle, &code) == nil && code == processStillActiveExitCode
}
