package main

import (
	"sync"

	"fyne.io/systray"
)

type desktopTray struct {
	end       func()
	openItem  *systray.MenuItem
	quitItem  *systray.MenuItem
	once      sync.Once
	ready     chan struct{}
	readyOnce sync.Once
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
	if !traySupported() {
		return false
	}
	a.mu.Lock()
	if a.tray != nil {
		a.mu.Unlock()
		return true
	}
	t := newDesktopTray()
	a.tray = t
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
		a.trayReady = true
		a.mu.Unlock()
		t.markReady()

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
		a.mu.Lock()
		if a.tray == t {
			a.trayReady = false
			a.tray = nil
		}
		a.mu.Unlock()
	})
	a.mu.Lock()
	t.end = end
	a.mu.Unlock()
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

func (a *App) trayLocale() string {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return ""
	}
	return cfg.DesktopLanguage()
}

func (a *App) showFromTray() {
	a.showMainWindow()
}

func (a *App) quitFromTray() {
	a.quitApp()
}

type trayLabels struct {
	openTitle   string
	openTooltip string
	quitTitle   string
	quitTooltip string
}

func trayMenuLabels(locale string) trayLabels {
	if locale == "zh" {
		return trayLabels{
			openTitle:   "打开",
			openTooltip: "打开 Reasonix 窗口",
			quitTitle:   "退出",
			quitTooltip: "退出 Reasonix",
		}
	}
	return trayLabels{
		openTitle:   "Open",
		openTooltip: "Open the Reasonix window",
		quitTitle:   "Quit",
		quitTooltip: "Quit Reasonix",
	}
}
