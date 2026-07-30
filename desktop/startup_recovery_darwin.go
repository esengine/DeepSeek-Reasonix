//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"reasonix/internal/repair"
)

func preparePackagedStartupRecovery(tracker *repair.StartupTracker, recommended, explicitSafeMode bool) (bool, bool) {
	return runDesktopStartupRecovery(recommended, explicitSafeMode, desktopStartupRecoveryDeps{
		recoverFailedInstall:  repair.RecoverFailedInstall,
		rollbackPendingUpdate: repair.RollbackPendingUpdate,
		repairGlobalConfig: func() error {
			_, err := repair.InspectAndRepairConfig(repair.ConfigOptions{Apply: true, OnlyScope: "global"})
			return err
		},
		choose:    nativeDesktopRecoveryChoice,
		markClean: tracker.MarkClean,
		relaunch: func(appBundle string) error {
			if strings.TrimSpace(appBundle) == "" {
				return fmt.Errorf("restored application bundle path is empty")
			}
			return exec.Command("/usr/bin/open", "-na", appBundle).Run()
		},
		report: func(message string) {
			fmt.Fprintln(os.Stderr, "Reasonix recovery:", message)
			showDesktopRecoveryError(message)
		},
	})
}

func nativeDesktopRecoveryChoice() desktopRecoveryChoice {
	script := `button returned of (display dialog "Reasonix failed to start repeatedly. You can repair damaged global configuration or start with external integrations disabled." with title "Reasonix Recovery" buttons {"Quit", "Safe Mode", "Repair and Start"} default button "Repair and Start" with icon caution)`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return desktopRecoverySafeMode
	}
	switch strings.TrimSpace(string(out)) {
	case "Repair and Start":
		return desktopRecoveryRepair
	case "Safe Mode":
		return desktopRecoverySafeMode
	default:
		return desktopRecoveryQuit
	}
}

func showDesktopRecoveryError(message string) {
	// Pass the message as argv rather than interpolating AppleScript so file
	// paths and command errors containing quotes cannot alter the script.
	script := `on run argv
display dialog (item 1 of argv) with title "Reasonix Recovery" buttons {"OK"} default button "OK" with icon stop
end run`
	_ = exec.Command("osascript", "-e", script, message).Run()
}
