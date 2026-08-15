package main

const (
	defaultDesktopWindowWidth  = 1240
	defaultDesktopWindowHeight = 720
)

// desktopWindowFrameless keeps the Windows main window's custom chrome while
// remote web children retain the native frame. Remote pages have no Wails
// bindings, drag regions, or window controls.
func desktopWindowFrameless(goos string, remoteWindow bool) bool {
	return goos == "windows" && !remoteWindow
}

// initialDesktopWindowSize restores saved geometry only for the main window.
// Remote children use defaults because a maximised main window's saved outer
// size can exceed the display and hide the centred remote window's titlebar.
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
