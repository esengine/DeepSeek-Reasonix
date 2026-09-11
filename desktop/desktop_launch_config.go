package main

import (
	goruntime "runtime"

	"reasonix/desktop/internal/hostrpc"
)

const (
	defaultDesktopWindowWidth  = 1240
	defaultDesktopWindowHeight = 720
)

// desktopWindowFrameless reports whether the main window is created without a
// native OS frame.
//
// The main window is frameless on Windows and draws its own chrome: the
// frontend supplies the drag rail (the shell rewrites the drag-region marker
// to -webkit-app-region) and the minimise/maximise/close buttons via the App
// bindings. Remote Serve windows are shell BrowserWindows with the native
// frame and never consult this function.
func desktopWindowFrameless(goos string) bool {
	return goos == "windows"
}

// initialDesktopWindowGeometry reads one saved rectangle. The shell fits it to
// the current displays before creation, regardless of the saved maximise flag.
func initialDesktopWindowGeometry() *hostrpc.WindowGeometry {
	geometry := &hostrpc.WindowGeometry{
		Width: defaultDesktopWindowWidth, Height: defaultDesktopWindowHeight,
		MinWidth: desktopWindowMinWidth, MinHeight: desktopWindowMinHeight,
		Frameless: desktopWindowFrameless(goruntime.GOOS), ZoomFactor: initialDesktopZoomFactor(),
	}
	if saved, ok := loadWindowState(); ok {
		geometry.Width, geometry.Height = saved.Width, saved.Height
		geometry.Position = &hostrpc.WindowPosition{X: saved.X, Y: saved.Y}
	}
	return geometry
}
