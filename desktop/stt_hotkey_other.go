//go:build !windows

package main

// sttHotkeyManager 的非 Windows 空实现：全局热键仅 Windows 支持，
// 其他平台注册为空操作（识别仍可经由输入框按钮/设置触发）。

// sttHotkeyManager 占位类型（Windows 实现见 stt_hotkey_windows.go）。
type sttHotkeyManager struct{}

// newSTTHotkeyManager 创建空的热键管理器。
func newSTTHotkeyManager(startFn, stopFn func()) *sttHotkeyManager {
	return &sttHotkeyManager{}
}

// start 空操作：非 Windows 平台不注册全局热键。
func (m *sttHotkeyManager) start(startHotkey, stopHotkey string) error { return nil }

// stop 空操作。
func (m *sttHotkeyManager) stop() {}
