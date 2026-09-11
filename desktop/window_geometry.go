package main

import goruntime "runtime"

// restoreWindowGeometry applies the saved origin and maximise state to the
// still-hidden main window before domReady presents it.
func (a *App) restoreWindowGeometry() {
	host := a.nativeHost()
	state, ok := loadWindowState()
	if ok {
		// The host screen list carries sizes but no per-screen origin, so only a
		// basic sanity check is possible. Windows border insets (commonly x=-8,
		// y=-8) are legal; off-screen positions (unplugged display) re-center.
		maxW, maxH := 0, 0
		screens, err := host.Screens(a.ctx)
		if err == nil {
			for _, sc := range screens {
				if sc.Width > maxW {
					maxW = sc.Width
				}
				if sc.Height > maxH {
					maxH = sc.Height
				}
			}
		}
		if windowPositionRestorable(state, maxW, maxH) {
			host.SetWindowPosition(a.ctx, state.X, state.Y)
		} else {
			host.CenterWindow(a.ctx)
		}
	} else {
		host.CenterWindow(a.ctx)
	}

	if ok && state.Maximised {
		if goruntime.GOOS == "windows" {
			// Preserve the established Windows maximise -> show ordering through
			// the unified presentation plan without appending SW_RESTORE.
			a.backgroundMaximised.Store(true)
		} else {
			host.MaximiseWindow(a.ctx)
		}
	}
}
