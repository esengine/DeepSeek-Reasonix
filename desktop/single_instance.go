package main

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func singleInstanceLock(app *App) *options.SingleInstanceLock {
	// Allow contributors to run multiple dev instances from the same binary.
	if os.Getenv("REASONIX_DEV") != "" {
		return nil
	}
	return &options.SingleInstanceLock{
		UniqueId: singleInstanceID(),
		OnSecondInstanceLaunch: func(options.SecondInstanceData) {
			app.secondInstanceLaunch()
		},
	}
}
