//go:build windows

package main

// Windows 全局热键注册（user32 RegisterHotKey + 消息循环）。
// 解析 "alt+s" / "ctrl+shift+f10" 这类格式：修饰键 + 单字符/功能键。
// 收到 WM_HOTKEY 后回调对应的开始/停止识别动作。

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
)

const (
	wmHotKey = 0x0312
	wmQuit   = 0x0012

	modAlt     = 0x0001
	modControl = 0x0002
	modShift   = 0x0004
	modWin     = 0x0008
)

// sttHotkeyManager 注册两个全局热键（开始/停止）并运行消息循环。
type sttHotkeyManager struct {
	mu       sync.Mutex
	threadID uint32
	startID  int
	stopID   int
	startFn  func()
	stopFn   func()
	started  bool
	// done 是消息循环 goroutine 的退出信号：stop() 发 WM_QUIT 后等待它关闭，
	// 确保旧线程真正退出（Windows 在线程退出时自动释放该线程注册的热键），
	// 之后 start() 才能在新线程重新注册，避免"热键被旧线程占用"注册失败。
	done chan struct{}
}

type msg struct {
	hwnd    uintptr
	message uint32
	wparam  uintptr
	lparam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// newSTTHotkeyManager 创建热键管理器。
func newSTTHotkeyManager(startFn, stopFn func()) *sttHotkeyManager {
	return &sttHotkeyManager{startFn: startFn, stopFn: stopFn}
}

// start 在独立 goroutine 里注册热键并进入消息循环。返回错误表示注册失败
// （热键被占用/格式非法）。
func (m *sttHotkeyManager) start(startHotkey, stopHotkey string) error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}
	// 若旧消息循环线程仍在退出中（stop 已发 WM_QUIT 但未完成），等待其退出，
	// 避免新线程注册时旧线程仍占用热键（Windows 在线程退出时才释放其热键）。
	prevDone := m.done
	m.done = make(chan struct{})
	done := m.done
	m.started = true
	m.mu.Unlock()
	if prevDone != nil {
		<-prevDone
	}

	go func() {
		// RegisterHotKey 注册到"当前线程"，GetMessage 也必须在同一线程收消息；
		// Go 的 goroutine 会在 OS 线程间迁移，必须锁住当前线程避免热键消息丢失。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		// 当前线程 ID（消息循环所在线程）。
		tid, _, _ := procGetCurrentThreadId.Call()
		m.mu.Lock()
		m.threadID = uint32(tid)
		m.mu.Unlock()

		// id 从 1 开始：1 = 开始识别，2 = 停止识别。
		m.startID = 1
		m.stopID = 2
		startMod, startVK := parseHotkey(startHotkey)
		stopMod, stopVK := parseHotkey(stopHotkey)
		if startVK != 0 {
			if startMod == 0 && hotkeyRequiresModifier(startVK) {
				fmt.Printf("[STT] 开始快捷键 %q 无效：必须包含修饰键（如 alt+s）\n", startHotkey)
			} else if !registerHotKey(0, m.startID, startMod, startVK) {
				fmt.Printf("[STT] 注册开始快捷键失败（组合键被占用或无效）: %s\n", startHotkey)
			}
		}
		if stopVK != 0 {
			if stopMod == 0 && hotkeyRequiresModifier(stopVK) {
				fmt.Printf("[STT] 停止快捷键 %q 无效：必须包含修饰键（如 alt+w）\n", stopHotkey)
			} else if !registerHotKey(0, m.stopID, stopMod, stopVK) {
				fmt.Printf("[STT] 注册停止快捷键失败（组合键被占用或无效）: %s\n", stopHotkey)
			}
		}

		for {
			var msgVal msg
			ret, _, _ := procGetMessageW.Call(
				uintptr(unsafe.Pointer(&msgVal)),
				0, 0, 0,
			)
			if ret == 0 { // WM_QUIT
				return
			}
			if msgVal.message == wmHotKey {
				switch int(msgVal.wparam) {
				case m.startID:
					if m.startFn != nil {
						m.startFn()
					}
				case m.stopID:
					if m.stopFn != nil {
						m.stopFn()
					}
				}
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msgVal)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msgVal)))
		}
	}()
	return nil
}

// stop 注销热键并退出消息循环。热键由注册线程持有，跨线程 UnregisterHotKey
// 无效——这里只发 WM_QUIT，等待消息循环线程退出（Windows 在线程退出时自动
// 释放该线程注册的热键）。
func (m *sttHotkeyManager) stop() {
	m.mu.Lock()
	threadID := m.threadID
	done := m.done
	m.started = false
	m.mu.Unlock()

	if threadID != 0 {
		procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
	}
	if done != nil {
		<-done
	}
}

// isStarted 报告热键是否已注册并运行消息循环。
func (m *sttHotkeyManager) isStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// registerHotKey 注册全局热键；返回 false 表示失败（组合键被占用或无效，
// RegisterHotKey 返回 0）。
func registerHotKey(hwnd uintptr, id int, modifiers, vk uintptr) bool {
	ret, _, _ := procRegisterHotKey.Call(hwnd, uintptr(id), modifiers, vk)
	return ret != 0
}

func unregisterHotKey(hwnd uintptr, id int) {
	procUnregisterHotKey.Call(hwnd, uintptr(id))
}

// parseHotkey 解析 "alt+s" / "ctrl+shift+f10" / "f11" 等格式，
// 返回修饰键位掩码与虚拟键码（0 表示无法解析/空串）。
// 修饰键名做常见拼写容错（alt/atl、ctrl/control、win/cmd/meta 等），
// 避免用户手误输入 "ATL+S" 时被当成裸键而注册失败。
func parseHotkey(s string) (uintptr, uintptr) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0
	}
	parts := strings.Split(strings.ToLower(s), "+")
	var mods uintptr
	var key string
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch p {
		case "alt", "option", "atl", "opt": // "atl" 是 alt 的常见手误
			mods |= modAlt
		case "ctrl", "control":
			mods |= modControl
		case "shift":
			mods |= modShift
		case "win", "cmd", "meta", "super":
			mods |= modWin
		default:
			key = p
		}
		if i == len(parts)-1 && key == "" {
			key = p
		}
	}
	vk := hotkeyVK(key)
	if vk == 0 {
		return 0, 0
	}
	return mods, vk
}

// hotkeyRequiresModifier 报告该虚拟键是否必须搭配修饰键才能注册为全局热键。
// Windows RegisterHotKey 只允许 F1-F24 等少量键不带修饰键裸注册；普通字母/
// 数字/符号键必须带 alt/ctrl/shift/win，否则注册失败（用户配置的裸键 "a"
// 正是这个原因）。
func hotkeyRequiresModifier(vk uintptr) bool {
	if vk >= 0x70 && vk <= 0x87 { // VK_F1 (0x70) – VK_F24 (0x87)
		return false
	}
	return true
}

// hotkeyVK 把单字符/常见功能键名映射为 Windows 虚拟键码。
func hotkeyVK(key string) uintptr {
	if key == "" {
		return 0
	}
	// 功能键 F1-F24。
	if len(key) >= 2 && (key[0] == 'f' || key[0] == 'F') {
		var n int
		if _, err := fmt.Sscanf(key[1:], "%d", &n); err == nil && n >= 1 && n <= 24 {
			return uintptr(0x70 - 1 + n) // VK_F1 = 0x70
		}
	}
	switch key {
	case "space":
		return 0x20 // VK_SPACE
	case "enter", "return":
		return 0x0D
	case "esc", "escape":
		return 0x1B
	case "tab":
		return 0x09
	case "backspace":
		return 0x08
	case "delete", "del":
		return 0x2E
	case "insert", "ins":
		return 0x2D
	case "home":
		return 0x24
	case "end":
		return 0x23
	case "pageup", "pgup":
		return 0x21
	case "pagedown", "pgdn":
		return 0x22
	case "up":
		return 0x26
	case "down":
		return 0x28
	case "left":
		return 0x25
	case "right":
		return 0x27
	}
	// 单字符：A-Z / 0-9 直接映射为对应 VK。
	if len(key) == 1 {
		c := key[0]
		if c >= 'a' && c <= 'z' {
			return uintptr(c - 'a' + 0x41) // VK_A = 0x41
		}
		if c >= '0' && c <= '9' {
			return uintptr(c - '0' + 0x30) // VK_0 = 0x30
		}
	}
	return 0
}
