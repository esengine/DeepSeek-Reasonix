package main

import (
	"fmt"
	"strconv"
)

func installerCommandLine(installer, dir string) string {
	// Keep this in sync with cmd/update-helper/args.go. The desktop uses it for
	// its Windows command invariant tests; the copied helper extracts to a
	// transaction-owned staging directory after the desktop exits.
	line := fmt.Sprintf(`"%s" /REASONIXUPDATE=1 /REASONIXSTAGE=1`, installer)
	if dir != "" {
		line += " /D=" + dir
	}
	return line
}

func windowsUpdateHandoffArgs(parentPID int, installer, installerSHA256, installDir, relaunch, toVersion, createdAt, transactionID string) []string {
	args := []string{
		"--parent-pid", strconv.Itoa(parentPID),
		"--installer", installer,
		"--installer-sha256", installerSHA256,
		"--to-version", toVersion,
		"--created-at", createdAt,
		"--transaction-id", transactionID,
	}
	if installDir != "" {
		args = append(args, "--install-dir", installDir)
	}
	if relaunch != "" {
		args = append(args, "--relaunch", relaunch)
	}
	return args
}

func windowsVersionedUpdateHandoffArgs(parentPID int, installer, installerSHA256, installDir, relaunch, toVersion string) []string {
	args := []string{
		"--parent-pid", strconv.Itoa(parentPID),
		"--installer", installer,
		"--installer-sha256", installerSHA256,
		"--to-version", toVersion,
		"--install-layout", "versioned-v1",
	}
	if installDir != "" {
		args = append(args, "--install-dir", installDir)
	}
	if relaunch != "" {
		args = append(args, "--relaunch", relaunch)
	}
	return args
}
