//go:build !windows && !cgo

package main

import "sync"

// desktopTray remains part of App's shape in pure-Go builds, but native tray
// implementations require platform UI libraries. Desktop startup already
// treats a missing tray as an optional capability.
type desktopTray struct {
	ready     chan struct{}
	readyOnce sync.Once
}

func newDesktopTray() *desktopTray { return &desktopTray{ready: make(chan struct{})} }

func (t *desktopTray) markReady() { t.readyOnce.Do(func() { close(t.ready) }) }

func (a *App) startTray() bool {
	if a == nil || a.shuttingDown.Load() || a.forceQuit.Load() {
		return false
	}
	a.setTrayHealth(nil, "unavailable", "native_tray_unavailable")
	return false
}

func (a *App) stopTray() {}

func (a *App) updateTrayLocale(string) {}
