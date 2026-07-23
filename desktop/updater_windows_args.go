package main

import (
	"fmt"
	"strconv"
)

func installerCommandLine(installer, dir string) string {
	// Keep this in sync with cmd/update-helper/args.go. The desktop uses it for
	// its Windows command invariant tests; the copied helper uses the same mode
	// after the desktop exits.
	line := fmt.Sprintf(`"%s" /REASONIXUPDATE=1`, installer)
	if dir != "" {
		line += " /D=" + dir
	}
	return line
}

func windowsUpdateHandoffArgs(parentPID int, installer, installDir, relaunch, toVersion string) []string {
	args := []string{
		"--parent-pid", strconv.Itoa(parentPID),
		"--installer", installer,
		"--to-version", toVersion,
	}
	if installDir != "" {
		args = append(args, "--install-dir", installDir)
	}
	if relaunch != "" {
		args = append(args, "--relaunch", relaunch)
	}
	return args
}
