package main

import (
	"log/slog"
	"os"

	"reasonix/internal/installlayout"
)

// supersededDesktopNeedsRelaunch reports whether this executable is a retained
// versions/<old>/ desktop after current.json already points at a newer version.
func supersededDesktopNeedsRelaunch(exe string) bool {
	if os.Getenv("REASONIX_DEV") != "" {
		return false
	}
	active, err := installlayout.IsActiveDesktopExecutable(exe)
	return err == nil && !active
}

func maybeRelaunchPrimaryIfSuperseded(launch desktopLaunchOptions) bool {
	if launch.RemoteWindowTicket != "" {
		return false
	}
	return maybeRelaunchIfSuperseded()
}

func maybeRelaunchIfSuperseded() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if !supersededDesktopNeedsRelaunch(exe) {
		return false
	}
	if err := relaunchThroughLauncher(); err != nil {
		slog.Error("desktop: relaunch superseded version", "err", err)
		return false
	}
	return true
}
