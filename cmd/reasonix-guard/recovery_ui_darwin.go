//go:build darwin

package main

import (
	"os/exec"
	"strings"
)

func nativeRecoveryChoice() recoveryChoice {
	script := `button returned of (display dialog "Reasonix failed to start repeatedly. You can repair damaged configuration or start with external integrations disabled." with title "Reasonix Recovery" buttons {"Quit", "Safe Mode", "Repair and Start"} default button "Repair and Start" with icon caution)`
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return recoverySafeMode
	}
	switch strings.TrimSpace(string(out)) {
	case "Repair and Start":
		return recoveryRepair
	case "Safe Mode":
		return recoverySafeMode
	default:
		return recoveryQuit
	}
}
