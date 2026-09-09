//go:build windows

package main

import (
	"log/slog"
	"os"
	"runtime"

	"fyne.io/systray"
	"reasonix/desktop/internal/instanceidentity"
)

var verifyTraySignatureFn = verifyTraySignature

func signedTrayIdentity(dev, exe, id string, verify func(string) error) string {
	if dev != "" || exe == "" || verify(exe) != nil {
		return ""
	}
	return instanceidentity.TrayGUID(id)
}

func startDesktopTray(onReady, onExit func()) func() {
	go runDesktopTrayLoop(func() {
		exe, _ := os.Executable()
		if id := signedTrayIdentity(os.Getenv("REASONIX_DEV"), exe, singleInstanceID(), verifyTraySignatureFn); id != "" {
			if err := systray.SetIconID(id); err != nil {
				slog.Warn("desktop: configure tray identity", "err", err)
			}
		}
		systray.Run(onReady, onExit)
	})
	return systray.Quit
}

func runDesktopTrayLoop(run func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	run()
}
