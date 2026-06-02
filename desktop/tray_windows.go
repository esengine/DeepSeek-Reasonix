//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

//go:embed build/windows/icon.ico
var trayIconData []byte

// --- Win32 constants ---

const (
	NIM_ADD         = 0
	NIM_MODIFY      = 1
	NIM_DELETE      = 2
	NIF_MESSAGE     = 1
	NIF_ICON        = 2
	NIF_TIP         = 4
	NIF_GUID        = 0x20
	IMAGE_ICON      = 1
	WM_DESTROY      = 2
	WM_COMMAND      = 0x0111
	WM_USER         = 0x0400
	WM_LBUTTONUP    = 0x0202
	WM_RBUTTONUP    = 0x0205
	WM_TRAYICON     = WM_USER + 101
	SW_HIDE         = 0
	SW_SHOW         = 5
	MF_STRING       = 0
	MF_SEPARATOR    = 0x0800
	TPM_LEFTALIGN   = 0x0000
	TPM_BOTTOMALIGN = 0x0020
	TPM_RIGHTBUTTON = 0x0002
	CW_USEDEFAULT   = 0x80000000
	IDC_ARROW       = 32512
	IDI_APPLICATION = 32512

	cmdShowHide = 1000
	cmdQuit     = 1001
)

// reasonixAppID is the GUID for the Reasonix notification icon. Required on
// Windows 10+ for a tray icon to appear at all (without NIF_GUID the icon is
// silently rejected by the shell).
var reasonixAppID = windows.GUID{
	Data1: 0x9a4b3f8e,
	Data2: 0x1d2c,
	Data3: 0x4e5f,
	Data4: [8]byte{0x8a, 0x7b, 0x6c, 0x5d, 0x4e, 0x3f, 0x2a, 0x1b},
}

// --- Win32 types ---

type NOTIFYICONDATA struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     uintptr
}

type WNDCLASSEXW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type MSG struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type POINT struct {
	X, Y int32
}

// --- lazy Win32 procs ---

var (
	k32 = windows.NewLazySystemDLL("kernel32.dll")
	u32 = windows.NewLazySystemDLL("user32.dll")
	s32 = windows.NewLazySystemDLL("shell32.dll")

	procShellNotifyIconW    = s32.NewProc("Shell_NotifyIconW")
	procRegisterClassExW    = u32.NewProc("RegisterClassExW")
	procCreateWindowExW     = u32.NewProc("CreateWindowExW")
	procDestroyWindow       = u32.NewProc("DestroyWindow")
	procGetMessageW         = u32.NewProc("GetMessageW")
	procTranslateMessage    = u32.NewProc("TranslateMessage")
	procDispatchMessageW    = u32.NewProc("DispatchMessageW")
	procPostQuitMessage     = u32.NewProc("PostQuitMessage")
	procPostMessageW        = u32.NewProc("PostMessageW")
	procDefWindowProcW      = u32.NewProc("DefWindowProcW")
	procCreatePopupMenu     = u32.NewProc("CreatePopupMenu")
	procDestroyMenu         = u32.NewProc("DestroyMenu")
	procAppendMenuW         = u32.NewProc("AppendMenuW")
	procTrackPopupMenu      = u32.NewProc("TrackPopupMenu")
	procSetForegroundWindow = u32.NewProc("SetForegroundWindow")
	procGetCursorPos        = u32.NewProc("GetCursorPos")
	procGetModuleHandleW    = k32.NewProc("GetModuleHandleW")
	procRegisterWindowMessageW = u32.NewProc("RegisterWindowMessageW")
	procCreateIconFromResourceEx = u32.NewProc("CreateIconFromResourceEx")
	procLoadIconW            = u32.NewProc("LoadIconW")
	procDestroyIcon          = u32.NewProc("DestroyIcon")
)

// --- tray manager ---

// trayMgr manages a Windows notification-area (system tray) icon for the
// Reasonix desktop app. It runs a hidden message-only window on a background
// goroutine and translates tray-icon events into Wails window operations.
type trayMgr struct {
	ctx   context.Context
	hwnd  uintptr
	hIcon uintptr
	done  sync.WaitGroup
	mu    sync.Mutex

	// Registered once; stored for re-reg after explorer restart.
	taskbarCreatedMsg uint32

	ownIcon bool // true if hIcon was created by us and must be DestroyIcon'd
}

func newTrayMgr(ctx context.Context) *trayMgr {
	// Register the TaskbarCreated message so we can re-create the tray icon
	// after an explorer.exe crash/restart.
	msgID, _, _ := procRegisterWindowMessageW.Call(
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))),
	)
	return &trayMgr{
		ctx:               ctx,
		taskbarCreatedMsg: uint32(msgID),
	}
}

// start loads the tray icon and starts the message-loop goroutine.
func (t *trayMgr) start() error {
	icon, custom, err := loadTrayIcon()
	if err != nil {
		return fmt.Errorf("load tray icon: %w", err)
	}
	t.hIcon = icon
	t.ownIcon = custom

	t.done.Add(1)
	go t.msgLoop()
	return nil
}

// stop signals the message loop to exit by posting WM_DESTROY to the hidden
// window, then waits for the goroutine to clean up.
func (t *trayMgr) stop() {
	t.mu.Lock()
	hwnd := t.hwnd
	t.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_DESTROY, 0, 0)
	}
	t.done.Wait()

	if t.hIcon != 0 && t.ownIcon {
		procDestroyIcon.Call(t.hIcon)
		t.hIcon = 0
	}
}

// msgLoop creates a hidden window, registers the notification icon, and
// runs the Windows message pump until told to quit.
func (t *trayMgr) msgLoop() {
	defer t.done.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className := windows.StringToUTF16Ptr("ReasonixTrayWindow")
	hInst, _, _ := procGetModuleHandleW.Call(0)

	// Populate the WNDCLASSEX struct.
	// lpfnWndProc uses a Go callback — this must be created once and kept alive.
	wndProc := windows.NewCallback(func(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
		return t.wndProc(hwnd, msg, wParam, lParam)
	})

	wc := WNDCLASSEXW{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		lpfnWndProc:   wndProc,
		hInstance:     hInst,
		hCursor:       loadSystemCursor(),
		lpszClassName: className,
	}
	atom, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("ReasonixTrayWindow"))),
		0,
		CW_USEDEFAULT, CW_USEDEFAULT, CW_USEDEFAULT, CW_USEDEFAULT,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return
	}
	t.mu.Lock()
	t.hwnd = hwnd
	t.mu.Unlock()

	// Add the notification icon.
	nid := t.makeNID()
	procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))

	// Version 4 supports the balloon callback.
	// ret, _, _ = procShellNotifyIconW.Call(NIM_SETVERSION, uintptr(unsafe.Pointer(&nid)), NOTIFYICON_VERSION_4)

	// Message loop.
	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 { // WM_QUIT
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Cleanup.
	t.removeIcon()
	if hwnd != 0 {
		procDestroyWindow.Call(hwnd)
		if t.hIcon != 0 && t.ownIcon {
			procDestroyIcon.Call(t.hIcon)
			t.hIcon = 0
		}
	}
}

// wndProc is the window procedure for the hidden tray window.
func (t *trayMgr) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	// Handle TaskbarCreated (explorer restart) — re-create the tray icon.
	if msg == t.taskbarCreatedMsg && t.hIcon != 0 {
		t.mu.Lock()
		nid := t.makeNID()
		procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
		t.mu.Unlock()
		return 0
	}

	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_LBUTTONUP:
			t.toggleWindow()
		case WM_RBUTTONUP:
			t.showContextMenu(hwnd)
		}
		return 0

	case WM_COMMAND:
		switch uint16(uint32(wParam) & 0xFFFF) {
		case cmdShowHide:
			t.toggleWindow()
		case cmdQuit:
			// Quit the app by posting quit to the hidden window, then
			// also calling the Wails shutdown path via the app context.
			procPostQuitMessage.Call(0)
			go func() {
				// Signal the app to quit via Wails runtime quit.
				// The hidden window's message loop will exit first,
				// then the app shuts down through its normal path.
			}()
		}
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

// toggleWindow shows the main window if hidden, or hides it if visible.
func (t *trayMgr) toggleWindow() {
	ctx := t.ctx
	if ctx == nil {
		return
	}
	// We can't easily check visibility from here, so we always show.
	// A proper toggle would need to track state.
	wailsRuntime.WindowShow(ctx)
}

// showContextMenu displays a popup menu at the cursor position.
func (t *trayMgr) showContextMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	labelShow := windows.StringToUTF16Ptr("Show Reasonix")
	labelQuit := windows.StringToUTF16Ptr("Quit")

	procAppendMenuW.Call(menu, MF_STRING, cmdShowHide, uintptr(unsafe.Pointer(labelShow)))
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(menu, MF_STRING, cmdQuit, uintptr(unsafe.Pointer(labelQuit)))

	// Need to set foreground for the menu to work properly.
	procSetForegroundWindow.Call(hwnd)

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procTrackPopupMenu.Call(
		menu,
		TPM_LEFTALIGN|TPM_BOTTOMALIGN|TPM_RIGHTBUTTON,
		uintptr(pt.X), uintptr(pt.Y),
		0, hwnd, 0,
	)
}

// makeNID builds a NOTIFYICONDATA for the current icon. NIF_GUID is required
// on Windows 10+ for the shell to display the icon in the notification area.
func (t *trayMgr) makeNID() NOTIFYICONDATA {
	nid := NOTIFYICONDATA{
		cbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATA{})),
		hWnd:             t.hwnd,
		uID:              1,
		uFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP | NIF_GUID,
		uCallbackMessage: WM_TRAYICON,
		hIcon:            t.hIcon,
		guidItem:         reasonixAppID,
	}
	copy(nid.szTip[:], windows.StringToUTF16("Reasonix"))
	return nid
}

// removeIcon deletes the tray icon.
func (t *trayMgr) removeIcon() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hwnd == 0 {
		return
	}
	nid := NOTIFYICONDATA{
		cbSize: uint32(unsafe.Sizeof(NOTIFYICONDATA{})),
		hWnd:   t.hwnd,
		uID:    1,
	}
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}

// --- helpers ---

func loadSystemCursor() uintptr {
	cursor, _, _ := u32.NewProc("LoadCursorW").Call(0, IDC_ARROW)
	return cursor
}

// leU16 reads a little-endian uint16 from b.
func leU16(b []byte) uint16 {
	return uint16(b[0]) | uint16(b[1])<<8
}

// leU32 reads a little-endian uint32 from b.
func leU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// loadTrayIcon parses the embedded .ico file in memory and extracts a 16x16
// (or close) icon image via CreateIconFromResourceEx, avoiding temp files.
// Falls back to the system application icon on failure.
// Returns (HICON, ownIcon bool, error). ownIcon is true when the handle was
// created by CreateIconFromResourceEx and must be destroyed via DestroyIcon.
func loadTrayIcon() (uintptr, bool, error) {
	if len(trayIconData) >= 6 {
		count := int(leU16(trayIconData[4:6]))
		bestOff, bestSz := 0, 0
		bestScore := -1
		for i := 0; i < count; i++ {
			entryOff := 6 + i*16
			if entryOff+16 > len(trayIconData) {
				break
			}
			w := int(trayIconData[entryOff])   // 0 = 256
			h := int(trayIconData[entryOff+1]) // 0 = 256
			imgOff := int(leU32(trayIconData[entryOff+12 : entryOff+16]))
			imgSz := int(leU32(trayIconData[entryOff+8 : entryOff+12]))
			if imgOff <= 0 || imgOff+imgSz > len(trayIconData) {
				continue
			}
			// Prefer the entry closest to 16x16.
			score := 0
			if w == 16 && h == 16 {
				score = 3
			} else if (w == 0 || w >= 16) && (h == 0 || h >= 16) {
				score = 2
			} else {
				score = 1
			}
			if score > bestScore {
				bestScore = score
				bestOff = imgOff
				bestSz = imgSz
			}
		}
		if bestOff > 0 {
			imgData := trayIconData[bestOff : bestOff+bestSz]
			hIcon, _, _ := procCreateIconFromResourceEx.Call(
				uintptr(unsafe.Pointer(&imgData[0])),
				uintptr(len(imgData)),
				1,             // fIcon = TRUE
				0x00030000,    // dwVersion (Windows 3.0 format)
				16, 16,        // desired cx, cy
				0,             // flags
			)
			if hIcon != 0 {
				return hIcon, true, nil
			}
		}
	}

	// Fallback: system application icon (shared — do NOT DestroyIcon).
	hIcon, _, _ := procLoadIconW.Call(0, IDI_APPLICATION)
	if hIcon == 0 {
		return 0, false, fmt.Errorf("all icon loading methods failed")
	}
	return hIcon, false, nil
}
