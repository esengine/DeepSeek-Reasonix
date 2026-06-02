//go:build windows

package main

import "golang.org/x/sys/windows"

// init hides the console window as early as possible on Windows, so a CMD
// window never appears when the desktop app is launched via auto-start or
// from Explorer with a console-mode binary (wails dev).
func init() { hideConsoleWindow() }

// hideConsoleWindow hides the console window attached to this process, if any.
//
// On Windows, a binary compiled without -H windowsgui (e.g. wails dev) runs as a
// console application and displays a CMD window on startup. When the app is
// launched via the auto-start registry key this CMD window is distracting.
// This call hides it immediately so the user sees only the GUI.
//
// For binaries compiled with -H windowsgui (wails build), GetConsoleWindow
// returns 0 and this is a no-op.
func hideConsoleWindow() {
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	getConsoleWindow := k32.NewProc("GetConsoleWindow")

	u32 := windows.NewLazySystemDLL("user32.dll")
	showWindow := u32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	// SW_HIDE = 0
	showWindow.Call(hwnd, 0)
}
