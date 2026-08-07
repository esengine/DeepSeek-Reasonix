package main

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func singleInstanceLock(app *App) *options.SingleInstanceLock {
	// Allow contributors to run a dev build alongside the installed app.
	// Set REASONIX_DEV=1 to skip the single-instance lock.
	if os.Getenv("REASONIX_DEV") != "" {
		return nil
	}
	return &options.SingleInstanceLock{
		UniqueId: singleInstanceID(),
		OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
			app.secondInstanceLaunch()
			// Windows delivers a second deep-link click as a second-instance
			// launch with the URL in Args (the scheme's "%1" argument). Forward
			// it to the already-running app the same way a first-launch argv
			// deep link is handled.
			for _, arg := range data.Args {
				if strings.HasPrefix(strings.ToLower(arg), "reasonix://") {
					app.queueDeepLink(arg)
					return
				}
			}
		},
	}
}
