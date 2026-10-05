//go:build windows

// The application the Windows live tests operate: plain Win32 controls, which
// reach UI Automation through the system's own proxies like most desktop
// software. Every input it receives is appended to the log named by argv[1],
// and it logs where its button and canvas sit on screen so a test can aim.
package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	registerClass  = user32.NewProc("RegisterClassExW")
	createWindow   = user32.NewProc("CreateWindowExW")
	defWindowProc  = user32.NewProc("DefWindowProcW")
	getMessage     = user32.NewProc("GetMessageW")
	translate      = user32.NewProc("TranslateMessage")
	dispatch       = user32.NewProc("DispatchMessageW")
	showWindow     = user32.NewProc("ShowWindow")
	postQuit       = user32.NewProc("PostQuitMessage")
	getWindowText  = user32.NewProc("GetWindowTextW")
	clientToScreen = user32.NewProc("ClientToScreen")
	loadCursor     = user32.NewProc("LoadCursorW")
	dpiAware       = user32.NewProc("SetProcessDpiAwarenessContext")
)

const (
	wmDestroy     = 0x0002
	wmCommand     = 0x0111
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	mkLButton     = 0x0001
	enChange      = 0x0300
	bnClicked     = 0
	fieldID       = 1
	buttonID      = 2
)

type wndClass struct {
	size       uint32
	style      uint32
	proc       uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type msg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	private uint32
}

var (
	logPath string
	field   uintptr
	presses int
)

func logf(format string, args ...any) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format+"\n", args...)
}

func text(hwnd uintptr) string {
	buf := make([]uint16, 512)
	n, _, _ := getWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func register(name string, proc uintptr, background uintptr) *uint16 {
	class, _ := windows.UTF16PtrFromString(name)
	arrow, _, _ := loadCursor.Call(0, 32512)
	wc := wndClass{proc: proc, className: class, cursor: windows.Handle(arrow), background: windows.Handle(background)}
	wc.size = uint32(unsafe.Sizeof(wc))
	registerClass.Call(uintptr(unsafe.Pointer(&wc)))
	return class
}

func create(class, title string, style uint32, x, y, w, h int32, parent, id uintptr) uintptr {
	c, _ := windows.UTF16PtrFromString(class)
	t, _ := windows.UTF16PtrFromString(title)
	hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, id, 0, 0)
	return hwnd
}

// centre logs where a child's middle is on screen, for a test to click at.
func centre(name string, hwnd uintptr, w, h int32) {
	pt := struct{ x, y int32 }{w / 2, h / 2}
	clientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	logf("at %s %d %d", name, pt.x, pt.y)
}

func main() {
	runtime.LockOSThread()
	logPath = os.Args[1]
	dpiAware.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2

	mainProc := syscall.NewCallback(func(hwnd, m, wParam, lParam uintptr) uintptr {
		switch m {
		case wmCommand:
			id, code := wParam&0xFFFF, wParam>>16
			if id == buttonID && code == bnClicked {
				presses++
				logf("button pressed %d", presses)
			}
			if id == fieldID && code == enChange {
				logf("text %s", text(field))
			}
			return 0
		case wmDestroy:
			postQuit.Call(0)
			return 0
		}
		r, _, _ := defWindowProc.Call(hwnd, m, wParam, lParam)
		return r
	})
	canvasProc := syscall.NewCallback(func(hwnd, m, wParam, lParam uintptr) uintptr {
		switch m {
		case wmLButtonDown:
			logf("mouseDown")
		case wmRButtonDown:
			logf("rightMouseDown")
		case wmMouseMove:
			if wParam&mkLButton != 0 {
				logf("mouseDragged")
			}
		}
		r, _, _ := defWindowProc.Call(hwnd, m, wParam, lParam)
		return r
	})

	register("ReasonixProbe", mainProc, 16) // COLOR_BTNFACE + 1
	register("ProbeCanvas", canvasProc, 17) // COLOR_BTNSHADOW + 1

	const child, visible, border, tabstop = 0x40000000, 0x10000000, 0x00800000, 0x00010000
	top := create("ReasonixProbe", "Computer Target", 0x00CF0000, 120, 120, 460, 300, 0, 0)
	create("STATIC", "Probe field", child|visible, 10, 12, 100, 22, top, 0)
	field = create("EDIT", "", child|visible|border|tabstop|0x0080, 120, 10, 260, 26, top, fieldID)
	button := create("BUTTON", "Probe button", child|visible|tabstop, 120, 50, 150, 32, top, buttonID)
	canvas := create("ProbeCanvas", "", child|visible, 10, 100, 180, 120, top, 3)
	showWindow.Call(top, 4) // SW_SHOWNOACTIVATE
	centre("button", button, 150, 32)
	centre("canvas", canvas, 180, 120)
	logf("ready")

	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		translate.Call(uintptr(unsafe.Pointer(&m)))
		dispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}
