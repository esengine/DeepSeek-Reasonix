package main

import (
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func singleInstanceLock(app *App) *options.SingleInstanceLock {
	// Allow contributors to run a dev build alongside the installed app.
	// Set REASONIX_DEV=1 to skip the single-instance lock.
	if os.Getenv("REASONIX_DEV") != "" {
		return nil
	}

	// Derive a unique lock ID from the binary's own executable path, so each
	// copy of the .app (different branch builds, timestamped copies, etc.)
	// gets its own instance lock — they can all run side by side.
	// The same binary (same path) still prevents double-launching.
	id := singleInstanceID
	if exe, err := os.Executable(); err == nil {
		h := sha256.Sum256([]byte(exe))
		id = fmt.Sprintf("%s.%x", singleInstanceID, h[:8])
	}

	return &options.SingleInstanceLock{
		UniqueId: id,
		OnSecondInstanceLaunch: func(options.SecondInstanceData) {
			app.secondInstanceLaunch()
		},
	}
}
