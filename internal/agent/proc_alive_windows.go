//go:build windows

package agent

import "golang.org/x/sys/windows"

// STILL_ACTIVE is the exit code returned by GetExitCodeProcess when a process
// has not yet terminated. Defined in the Windows API as 259 (0x103).
const stillActive = 259

// processAlive reports whether the given PID is still running.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	return exitCode == stillActive
}
