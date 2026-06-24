package main

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func singleInstanceLock(app *App) *options.SingleInstanceLock {
	// Allow contributors to run a dev build alongside the installed app.
	// Set QIAOTONGAGENT_DEV=1 to skip the single-instance lock.
	if os.Getenv("QIAOTONGAGENT_DEV") != "" {
		return nil
	}
	return &options.SingleInstanceLock{
		UniqueId: singleInstanceID,
		OnSecondInstanceLaunch: func(options.SecondInstanceData) {
			app.secondInstanceLaunch()
		},
	}
}
