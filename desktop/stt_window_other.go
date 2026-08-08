//go:build !windows

package main

// stt_window_other.go：非 Windows 平台的空实现。
// 窗口隐藏/缩放依赖 Win32 API，其他平台仅保留 --window-position 兜底。

// sttHideWindowsForPID 空操作（非 Windows）。
func sttHideWindowsForPID(pid uint32) bool { return false }

// sttShowWindowsForPID 空操作（非 Windows）。
func sttShowWindowsForPID(pid uint32) bool { return false }

// sttResizeWindowsForPID 空操作（非 Windows）。
func sttResizeWindowsForPID(pid uint32, width, height int) bool { return false }
