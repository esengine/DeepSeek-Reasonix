package main

import "github.com/wailsapp/wails/v2/pkg/runtime"

// MinimiseMainWindow backs the Windows frameless titlebar controls.
func (a *App) MinimiseMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowMinimise(a.ctx)
}

// ToggleMaximiseMainWindow backs the Windows frameless titlebar controls.
func (a *App) ToggleMaximiseMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowToggleMaximise(a.ctx)
}

// IsMainWindowMaximised reports the native maximise state for the Windows
// frameless titlebar controls.
func (a *App) IsMainWindowMaximised() bool {
	if a.ctx == nil {
		return false
	}
	return runtime.WindowIsMaximised(a.ctx)
}

// CloseMainWindow preserves Reasonix's configured close behavior for the
// Windows frameless titlebar close button.
func (a *App) CloseMainWindow() {
	if a.ctx == nil {
		return
	}
	if a.beforeClose(a.ctx) {
		return
	}
	a.forceQuit.Store(true)
	runtime.Quit(a.ctx)
}
