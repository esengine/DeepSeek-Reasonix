//go:build windows

package agent

import "golang.org/x/sys/windows"

// processAlive reports whether the given PID is still running.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		windows.CloseHandle(h)
		return false
	}
	windows.CloseHandle(h)
	// STILL_ACTIVE (259) means the process has not exited.
	return exitCode == windows.STILL_ACTIVE
}
