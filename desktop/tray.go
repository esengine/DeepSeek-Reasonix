//go:build windows || cgo

package main

import (
	"context"
	goruntime "runtime"
	"sync"

	"fyne.io/systray"
)

type desktopTray struct {
	end           func()
	openItem      *systray.MenuItem
	quitItem      *systray.MenuItem
	once          sync.Once
	ready         chan struct{}
	readyOnce     sync.Once
	healthMu      sync.Mutex
	cancel        context.CancelFunc
	healthStopped bool
}

func newDesktopTray() *desktopTray {
	return &desktopTray{ready: make(chan struct{})}
}

func (t *desktopTray) markReady() {
	t.readyOnce.Do(func() {
		close(t.ready)
	})
}

func (a *App) startTray() bool {
	if a == nil || a.shuttingDown.Load() || a.forceQuit.Load() {
		return false
	}
	if !traySupported() {
		reason := "no_session_bus"
		if goruntime.GOOS == "darwin" {
			reason = "platform_no_tray"
		}
		a.setTrayHealth(nil, "unavailable", reason)
		return false
	}
	a.mu.Lock()
	if a.tray != nil {
		a.mu.Unlock()
		return true
	}
	t := newDesktopTray()
	a.tray = t
	a.desktopShell.trayState = "probing"
	a.desktopShell.trayReason = ""
	a.trayReady = false
	a.mu.Unlock()

	end := startDesktopTray(func() {
		systray.SetIcon(trayIconBytes)
		systray.SetTitle("Reasonix")
		systray.SetTooltip("Reasonix")
		// Run off the systray Win32 message loop: SetOnTapped fires inside wndProc,
		// so a blocking showFromTray (a wedged webview after sleep freezes
		// runtime.WindowShow) would stall the whole tray's message pump (#3834). The
		// menu items below are already decoupled via goroutines for the same reason.
		systray.SetOnTapped(func() { a.goSafe("showFromTray", a.showFromTray) })
		// Keep secondary/right-click on systray's native menu path.
		systray.SetOnSecondaryTapped(nil)

		labels := trayMenuLabels(a.trayLocale())
		openItem := systray.AddMenuItem(labels.openTitle, labels.openTooltip)
		quitItem := systray.AddMenuItem(labels.quitTitle, labels.quitTooltip)

		// Publish the menu items under a.mu: this callback runs on the systray
		// goroutine while bound settings calls (updateTrayLocale) read them.
		a.mu.Lock()
		t.openItem = openItem
		t.quitItem = quitItem
		a.mu.Unlock()
		a.trayConfigured(t)

		a.goSafe("trayOpenLoop", func() {
			for range openItem.ClickedCh {
				a.showFromTray()
			}
		})
		a.goSafe("trayQuitLoop", func() {
			for range quitItem.ClickedCh {
				a.quitFromTray()
			}
		})
	}, func() {
		t.stopHealthMonitor()
		a.setTrayHealth(t, "unavailable", "tray_exited")
		a.mu.Lock()
		if a.tray == t {
			a.tray = nil
		}
		a.mu.Unlock()
	})
	a.mu.Lock()
	t.end = end
	a.mu.Unlock()
	if a.shuttingDown.Load() || a.forceQuit.Load() {
		t.once.Do(end)
		return false
	}
	a.startTrayHealthMonitor(t)
	return true
}

func (a *App) stopTray() {
	a.mu.RLock()
	t := a.tray
	var end func()
	if t != nil {
		end = t.end
	}
	a.mu.RUnlock()
	if t == nil || end == nil {
		return
	}
	t.once.Do(end)
}

func (t *desktopTray) stopHealthMonitor() {
	if t == nil {
		return
	}
	t.healthMu.Lock()
	t.healthStopped = true
	cancel := t.cancel
	t.cancel = nil
	t.healthMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) updateTrayLocale(locale string) {
	a.mu.RLock()
	t := a.tray
	var openItem, quitItem *systray.MenuItem
	if t != nil {
		openItem = t.openItem
		quitItem = t.quitItem
	}
	a.mu.RUnlock()
	if openItem == nil || quitItem == nil {
		return
	}
	labels := trayMenuLabels(locale)
	openItem.SetTitle(labels.openTitle)
	openItem.SetTooltip(labels.openTooltip)
	quitItem.SetTitle(labels.quitTitle)
	quitItem.SetTooltip(labels.quitTooltip)
}
