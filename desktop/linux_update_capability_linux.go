//go:build linux

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func linuxSelfUpdateAvailable() bool {
	executable := currentExecutablePath()
	if executable == "" || unix.Access(executable, unix.W_OK) != nil {
		return false
	}
	runtimeRoot := linuxRuntimePathForExecutable(executable)
	return writableExistingAncestor(filepath.Dir(runtimeRoot))
}

func writableExistingAncestor(name string) bool {
	for {
		if _, err := os.Stat(name); err == nil {
			return unix.Access(name, unix.W_OK|unix.X_OK) == nil
		}
		parent := filepath.Dir(name)
		if parent == name {
			return false
		}
		name = parent
	}
}

func linuxSelfUpdateManualReason() string {
	return "This Linux installation is package-managed or its bundled Chromium directory is not writable; install the new package manually"
}
