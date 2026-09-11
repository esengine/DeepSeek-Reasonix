//go:build darwin || (!windows && !cgo)

package main

func (a *App) startTrayHealthMonitor(*desktopTray) {}
func (a *App) trayConfigured(*desktopTray)         {}
