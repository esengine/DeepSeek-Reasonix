//go:build windows

package main

// Win32 窗口控制：按 PID 查找 Edge 识别页窗口并真正隐藏（ShowWindow SW_HIDE）。
// 参考 edge-stt-bridge 的做法——--window-position/-size 参数会被专用 profile
// 记住的旧值覆盖，只有等窗口出现后用 Win32 强制隐藏/缩放才可靠。

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	sttUser32              = syscall.NewLazyDLL("user32.dll")
	sttEnumWindows         = sttUser32.NewProc("EnumWindows")
	sttGetWindowThreadPid  = sttUser32.NewProc("GetWindowThreadProcessId")
	sttGetClassNameW       = sttUser32.NewProc("GetClassNameW")
	sttGetWindowTextW      = sttUser32.NewProc("GetWindowTextW")
	sttShowWindow          = sttUser32.NewProc("ShowWindow")
	sttIsWindowVisible     = sttUser32.NewProc("IsWindowVisible")
	sttSetWindowPos        = sttUser32.NewProc("SetWindowPos")
	sttGetWindowRect       = sttUser32.NewProc("GetWindowRect")
)

const (
	sttSWHide      = 0
	sttSWShow      = 5
	sttSWPNosize   = 0x0001
	sttSWPNomove   = 0x0002
)

// sttHWNDTopmost 是 HWND_TOPMOST（(HWND)-1）的 uintptr 表示。
var sttHWNDTopmost = ^uintptr(0)

type sttRect struct {
	left, top, right, bottom int32
}

// sttEnumWindowsProc 是 EnumWindows 回调（C 函数指针包装）。
type sttEnumWindowsProc func(hwnd syscall.Handle, lparam uintptr) uintptr

// sttFindWindowsForPID 枚举所有顶层窗口，返回属于 pid 的 Edge 识别页窗口句柄。
// Edge 是多进程架构：--app 窗口可能由子进程创建（PID 不匹配主进程），
// 且类名可能带版本后缀，因此放宽匹配：
//   - 类名以 Chrome_WidgetWin_ 开头，且
//   - （窗口标题包含识别页标题 "Reasonix 语音识别"）或（PID 匹配）
func sttFindWindowsForPID(pid uint32) []syscall.Handle {
	var hwnds []syscall.Handle
	callback := syscall.NewCallback(func(hwnd syscall.Handle, lparam uintptr) uintptr {
		// 类名前缀匹配（Chrome_WidgetWin_0/1/...）。
		var clsBuf [256]uint16
		cn, _, _ := sttGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&clsBuf[0])), uintptr(len(clsBuf)))
		cls := syscall.UTF16ToString(clsBuf[:cn])
		if !strings.HasPrefix(cls, "Chrome_WidgetWin_") {
			return 1 // 继续枚举
		}
		// 标题包含识别页标题则直接命中（--app 窗口标题=页面 title）。
		var titleBuf [512]uint16
		tn, _, _ := sttGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&titleBuf[0])), uintptr(len(titleBuf)))
		title := syscall.UTF16ToString(titleBuf[:tn])
		if strings.Contains(title, "Reasonix 语音识别") {
			hwnds = append(hwnds, hwnd)
			return 1
		}
		// 否则按 PID 匹配。
		var winPid uint32
		sttGetWindowThreadPid.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&winPid)))
		if winPid == pid {
			hwnds = append(hwnds, hwnd)
		}
		return 1 // 继续枚举
	})
	sttEnumWindows.Call(callback, 0)
	return hwnds
}

// sttHideWindowsForPID 隐藏 pid 的所有 Edge 窗口。窗口可能尚未创建
// （Edge 启动有延迟），调用方应在延迟后调用。
func sttHideWindowsForPID(pid uint32) bool {
	hidden := false
	for _, hwnd := range sttFindWindowsForPID(pid) {
		sttShowWindow.Call(uintptr(hwnd), sttSWHide)
		hidden = true
	}
	return hidden
}

// sttShowWindowsForPID 显示 pid 的所有 Edge 窗口（并置前）。
func sttShowWindowsForPID(pid uint32) bool {
	shown := false
	for _, hwnd := range sttFindWindowsForPID(pid) {
		sttShowWindow.Call(uintptr(hwnd), sttSWShow)
		shown = true
	}
	return shown
}

// sttResizeWindowsForPID 把 pid 的 Edge 窗口强制缩到指定尺寸（保留位置/层级）。
// 专用 profile 会记住旧窗口大小，--window-size 参数会被覆盖，必须 Win32 强改。
func sttResizeWindowsForPID(pid uint32, width, height int) bool {
	resized := false
	for _, hwnd := range sttFindWindowsForPID(pid) {
		sttSetWindowPos.Call(
			uintptr(hwnd), uintptr(sttHWNDTopmost),
			0, 0, uintptr(width), uintptr(height),
			uintptr(sttSWPNomove),
		)
		resized = true
	}
	return resized
}
