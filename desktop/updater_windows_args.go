package main

import (
	"fmt"
	"strconv"
)

func installerCommandLine(installer, dir string) string {
	line := fmt.Sprintf(`"%s" /S`, installer)
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
