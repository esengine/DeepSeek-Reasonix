//go:build windows

package main

import (
	"os"

	"reasonix/internal/appidentity"
	"reasonix/internal/installlayout"
)

func init() {
	if err := appidentity.ApplyToCurrentProcess(); err != nil {
		println("Warning: apply Windows app identity:", err.Error())
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	installRoot, err := installlayout.ResolveInstallRoot(executable)
	if err != nil {
		return
	}
	if err := appidentity.RepairOwnedShortcuts(installRoot); err != nil {
		println("Warning: repair Windows shortcut identity:", err.Error())
	}
}
