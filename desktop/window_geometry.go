package main

import goruntime "runtime"

// restoreWindowGeometry applies the saved maximise state before presentation.
// The shell already restored and fitted the full rectangle during creation;
// never overwrite its display-aware position with a second Go-side restore.
func (a *App) restoreWindowGeometry() {
	host := a.nativeHost()
	state, ok := loadWindowState()

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
