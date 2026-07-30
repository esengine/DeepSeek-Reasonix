//go:build windows

package main

var (
	isWindowVisibleProc = user32DLL.NewProc("IsWindowVisible")
	isIconicProc        = user32DLL.NewProc("IsIconic")
)

func windowRestoreDiagnosticsSupported() bool { return true }

func windowRestoreOwnerAlive(pid int) bool {
	return desktopProcessAlive(pid)
}

func windowRestoreConfirmed() bool {
	hwnd := currentProcessTopLevelWindow()
	if hwnd == 0 {
		return false
	}
	visible, _, _ := isWindowVisibleProc.Call(hwnd)
	iconic, _, _ := isIconicProc.Call(hwnd)
	return visible != 0 && iconic == 0
}
