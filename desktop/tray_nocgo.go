//go:build !windows && !cgo

package main

// desktopTray remains part of App's shape in pure-Go builds, but native tray
// implementations require platform UI libraries. Desktop startup already
// treats a missing tray as an optional capability.
type desktopTray struct {
	ready chan struct{}
}

func (a *App) startTray() bool {
	return false
}

func (a *App) stopTray() {}

func (a *App) updateTrayLocale(string) {}
