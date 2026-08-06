//go:build windows

package main

func windowIsFrontmost() bool {
	hwnd := currentProcessTopLevelWindow()
	if hwnd == 0 {
		return false
	}
	fg, _, _ := getForegroundWindowProc.Call()
	if fg == 0 || fg != hwnd {
		return false
	}
	visible, _, _ := isWindowVisibleProc.Call(hwnd)
	iconic, _, _ := isIconicProc.Call(hwnd)
	return visible != 0 && iconic == 0
}

var getForegroundWindowProc = user32DLL.NewProc("GetForegroundWindow")
