//go:build !windows && cgo

package main

import "fyne.io/systray"

func startDesktopTray(onReady, onExit func()) func() {
	start, end := systray.RunWithExternalLoop(onReady, onExit)
	start()
	return end
}
