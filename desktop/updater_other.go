//go:build !windows

package main

import (
	"os/exec"

	"reasonix/internal/repair"
)

// installerCommand exists only so updater.go compiles off Windows; applyWindows is
// never dispatched there (see updater_app.go).
func installerCommand(name, _ string) *exec.Cmd {
	return exec.Command(name)
}

func startWindowsUpdateHandoff(name, _, dir, _ string, _ *repair.UpdateTransaction) error {
	return installerCommand(name, dir).Start()
}
