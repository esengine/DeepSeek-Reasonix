//go:build windows

package main

import "fyne.io/systray"

func startDesktopTray(onReady, onExit func()) func() {
	go systray.Run(onReady, onExit)
	return systray.Quit
}
