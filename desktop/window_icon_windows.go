//go:build windows

package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	windowIconUser32   = windows.NewLazySystemDLL("user32.dll")
	windowIconShell32  = windows.NewLazySystemDLL("shell32.dll")
	windowIconKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	windowIconSendMessageProc  = windowIconUser32.NewProc("SendMessageW")
	windowIconSetClassLongProc = windowIconUser32.NewProc("SetClassLongPtrW")
	windowIconExtractIcons     = windowIconShell32.NewProc("ExtractIconExW")
	windowIconGetModuleFn      = windowIconKernel32.NewProc("GetModuleFileNameW")

	// Injectable seams for tests (mirrors icon_repair_windows.go's pattern).
	windowIconFindWindow   = currentProcessTopLevelWindow
	windowIconLoadIcons    = loadExecutableIcons
	windowIconAssignWindow = assignWindowIcons
	windowIconSendMessage  = sendWindowIconMessage
	windowIconSetClassLong = setWindowClassIcon
)

const (
	wmSetIcon = 0x0080
	iconBig   = 1
	iconSmall = 0

	// GCLP_HICON / GCLP_HICONSM as signed int32 index values; must be passed
	// as uintptr after the two's-complement wrap for 64-bit SetClassLongPtrW.
	gclpHicon   = uintptr(^uint32(13)) // -14
	gclpHiconSm = uintptr(^uint32(33)) // -34
)

// applyWindowIconsFromExecutable restores the brand icon on the Wails main
// window. Wails v2 registers its window class ("wailsWindow") without an
// hIcon/hIconSm, so the window answers WM_GETICON with NULL. On Windows 10
// Explorer then falls back to the executable's first embedded icon, but on
// Windows 11, once the process carries an explicit AppUserModelID (PR #7925:
// SetCurrentProcessExplicitAppUserModelID("Reasonix")), the taskbar prefers
// resources registered for that AUMID and, finding none, renders the
// blank-document glyph.
//
// The fix loads the first icon group from this executable and assigns it both
// to the window (WM_SETICON big+small) and to the window class (class icons),
// which also covers Alt+Tab and window-switcher rendering.
func applyWindowIconsFromExecutable() {
	// The Wails window may not exist yet at OnStartup; retry briefly until
	// it does rather than racing window creation.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if hwnd := windowIconFindWindow(); hwnd != 0 {
			windowIconAssignWindow(hwnd)
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func assignWindowIcons(hwnd uintptr) {
	large, small := windowIconLoadIcons()
	if large == 0 && small == 0 {
		return
	}
	if large != 0 {
		windowIconSendMessage(hwnd, wmSetIcon, iconBig, large)
		windowIconSetClassLong(hwnd, gclpHicon, large)
	}
	if small != 0 {
		windowIconSendMessage(hwnd, wmSetIcon, iconSmall, small)
		windowIconSetClassLong(hwnd, gclpHiconSm, small)
	}
}

func sendWindowIconMessage(hwnd, message, iconType, icon uintptr) {
	windowIconSendMessageProc.Call(hwnd, message, iconType, icon)
}

func setWindowClassIcon(hwnd, index, icon uintptr) {
	windowIconSetClassLongProc.Call(hwnd, index, icon)
}

// loadExecutableIcons extracts the first icon group embedded in this
// executable via ExtractIconExW. The window and its class retain references,
// not ownership, so the handles must remain valid for the Wails process
// lifetime. This startup path runs once and retains at most two handles; the
// operating system reclaims them when the process exits.
func loadExecutableIcons() (large, small uintptr) {
	var exePath [2048]uint16
	if _, _, _ = windowIconGetModuleFn.Call(
		0,
		uintptr(unsafe.Pointer(&exePath[0])),
		uintptr(len(exePath)),
	); exePath[0] == 0 {
		return 0, 0
	}
	n, _, _ := windowIconExtractIcons.Call(
		uintptr(unsafe.Pointer(&exePath[0])),
		0,
		uintptr(unsafe.Pointer(&large)),
		uintptr(unsafe.Pointer(&small)),
		1,
	)
	if int32(n) <= 0 {
		return 0, 0
	}
	return large, small
}
