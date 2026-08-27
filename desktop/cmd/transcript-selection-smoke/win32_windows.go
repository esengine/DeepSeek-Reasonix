//go:build windows && reasonix_transcript_smoke

package main

import (
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32              = windows.NewLazySystemDLL("user32.dll")
	registerClassExW    = user32.NewProc("RegisterClassExW")
	createWindowExW     = user32.NewProc("CreateWindowExW")
	destroyWindow       = user32.NewProc("DestroyWindow")
	showWindow          = user32.NewProc("ShowWindow")
	updateWindow        = user32.NewProc("UpdateWindow")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
	setFocus            = user32.NewProc("SetFocus")
	setCursorPos        = user32.NewProc("SetCursorPos")
	clientToScreen      = user32.NewProc("ClientToScreen")
	peekMessageW        = user32.NewProc("PeekMessageW")
	translateMessage    = user32.NewProc("TranslateMessage")
	dispatchMessageW    = user32.NewProc("DispatchMessageW")
	defWindowProcW      = user32.NewProc("DefWindowProcW")
	sendInput           = user32.NewProc("SendInput")
)

const (
	wsOverlappedWindow  = 0x00CF0000
	swShow              = 5
	pmRemove            = 0x0001
	inputMouse          = 0
	mouseEventFLeftDown = 0x0002
	mouseEventFLeftUp   = 0x0004
)

type smokeWindowClass struct {
	cbSize     uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSmall  windows.Handle
}

type nativePoint struct{ x, y int32 }

type smokeMessageLoopEntry struct {
	hwnd    windows.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	point   nativePoint
	private uint32
}

type smokeMouseInput struct {
	dx, dy    int32
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type smokeInput struct {
	typeID uint32
	_      uint32
	mouse  smokeMouseInput
}

func createSmokeWindow(width, height int) (windows.Handle, error) {
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return 0, fmt.Errorf("get module handle: %w", err)
	}
	className, _ := windows.UTF16PtrFromString(fmt.Sprintf("ReasonixSelectionSmoke-%d", os.Getpid()))
	windowName, _ := windows.UTF16PtrFromString("Reasonix transcript selection smoke")
	windowClass := smokeWindowClass{
		cbSize: uint32(unsafe.Sizeof(smokeWindowClass{})),
		wndProc: windows.NewCallback(func(hwnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
			result, _, _ := defWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
			return result
		}),
		instance: instance, className: className,
	}
	if registered, _, err := registerClassExW.Call(uintptr(unsafe.Pointer(&windowClass))); registered == 0 {
		return 0, fmt.Errorf("register window class: %w", err)
	}
	hwnd, _, err := createWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)),
		wsOverlappedWindow, 0, 0, uintptr(width), uintptr(height), 0, 0, uintptr(instance), 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("create WebView2 host window: %w", err)
	}
	showWindow.Call(hwnd, swShow)
	updateWindow.Call(hwnd)
	setForegroundWindow.Call(hwnd)
	setFocus.Call(hwnd)
	return windows.Handle(hwnd), nil
}

func movePointerToClientPoint(hwnd windows.Handle, point smokePoint) {
	screenPoint := nativePoint{x: int32(point.X), y: int32(point.Y)}
	clientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&screenPoint)))
	setCursorPos.Call(uintptr(screenPoint.x), uintptr(screenPoint.y))
}

func sendNativeClicksBeforeFinal() error {
	for _, delay := range []time.Duration{400 * time.Millisecond, 320 * time.Millisecond, 180 * time.Millisecond} {
		if err := sendLeftButton(mouseEventFLeftDown); err != nil {
			return err
		}
		pumpFor(30 * time.Millisecond)
		if err := sendLeftButton(mouseEventFLeftUp); err != nil {
			return err
		}
		pumpFor(delay)
	}
	return nil
}

func sendLeftButton(flags uint32) error {
	input := smokeInput{typeID: inputMouse}
	input.mouse.flags = flags
	inserted, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input))
	if inserted != 1 {
		return fmt.Errorf("send WebView2 controller mouse input: %w", err)
	}
	return nil
}

func pumpFor(duration time.Duration) {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		pumpWindowsMessages()
		time.Sleep(time.Millisecond)
	}
}

func pumpWindowsMessages() {
	var message smokeMessageLoopEntry
	for {
		hasMessage, _, _ := peekMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, pmRemove)
		if hasMessage == 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&message)))
		dispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}
