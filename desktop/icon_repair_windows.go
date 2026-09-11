//go:build windows

package main

import (
	"os"

	"reasonix/internal/appidentity"
	"reasonix/internal/installlayout"
)

func repairDesktopIconIntegration() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	root, err := installlayout.ResolveInstallRoot(executable)
	if err != nil {
		return err
	}
	return appidentity.RepairOwnedShortcuts(root)
}
