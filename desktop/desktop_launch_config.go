package main

const (
	defaultDesktopWindowWidth  = 1240
	defaultDesktopWindowHeight = 720
)

// desktopWindowFrameless reports whether the Wails window is created without a
// native OS frame.
//
// The main window is frameless on Windows and draws its own chrome: the
// frontend supplies the drag rail via --wails-draggable and the
// minimise/maximise/close buttons via the App bindings.
//
// The remote web child window cannot do either. It is launched with no Wails
// bindings (see main) and then navigates to the host's Serve page over loopback
// HTTP, so neither window.go.main.App nor the Wails runtime exists in that
// document; internal/serve's UI has no drag regions or window controls at all.
// A frameless remote window therefore has no titlebar, cannot be moved, and
// cannot be minimised or restored — the only ways out are the keyboard system
// menu or killing the process. Remote windows keep the native frame.
func desktopWindowFrameless(goos string, remoteWindow bool) bool {
	return goos == "windows" && !remoteWindow
}

// initialDesktopWindowSize returns the startup size for the Wails window.
//
// The main window restores the saved geometry. The remote web child window must
// not: desktop-window.json stores the main window's *outer* size, which for a
// maximised window is the screen plus the invisible resize borders (e.g.
// 2574x1454 on a 2560x1440 display). The remote window is created without
// WS_MAXIMIZE and then centred by domReadyRemoteWindow, so inheriting that size
// centres a window larger than the screen and pushes its top-left corner —
// including the titlebar — off-display. Remote windows use the default size.
func initialDesktopWindowSize(remoteWindow bool) (int, int) {
	width, height := defaultDesktopWindowWidth, defaultDesktopWindowHeight
	if remoteWindow {
		return width, height
	}
	if saved, ok := loadWindowState(); ok {
		if saved.Width > 0 {
			width = saved.Width
		}
		if saved.Height > 0 {
			height = saved.Height
		}
	}
	return width, height
}
