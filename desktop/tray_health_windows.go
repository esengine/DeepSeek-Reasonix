//go:build windows

package main

func (a *App) startTrayHealthMonitor(*desktopTray) {}

func (a *App) trayConfigured(t *desktopTray) {
	a.setTrayHealth(t, "ready", "")
}
