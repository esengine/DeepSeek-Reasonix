package main

// MinimiseMainWindow backs the Windows frameless titlebar controls.
func (a *App) MinimiseMainWindow() {
	if a.ctx == nil {
		return
	}
	a.nativeHost().MinimiseWindow(a.ctx)
}

// ToggleMaximiseMainWindow backs the Windows frameless titlebar controls.
func (a *App) ToggleMaximiseMainWindow() {
	if a.ctx == nil {
		return
	}
	a.nativeHost().ToggleMaximiseWindow(a.ctx)
}

// IsMainWindowMaximised reports the native maximise state for the Windows
// frameless titlebar controls.
func (a *App) IsMainWindowMaximised() bool {
	if a.ctx == nil {
		return false
	}
	return a.nativeHost().WindowIsMaximised(a.ctx)
}

// CloseMainWindow is a compatibility entry point for older renderers. The shell
// owns the close decision and shutdown sequence for every window-close source.
func (a *App) CloseMainWindow() {
	if a.ctx == nil {
		return
	}
	a.nativeHost().CloseWindow(a.ctx)
}
