//go:build windows

package main

import (
	"runtime"

	"fyne.io/systray"
)

func startDesktopTray(onReady, onExit func()) func() {
	go runDesktopTrayLoop(func() {
		systray.Run(onReady, onExit)
	})
	return systray.Quit
}

func runDesktopTrayLoop(run func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	run()
}
