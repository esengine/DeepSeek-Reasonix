//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	autostartRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartAppName     = "ReasonixDesktop"
	autostartTaskName    = "ReasonixDesktopAutostart"
)

// autostartSupported reports whether autostart is supported on this platform.
func autostartSupported() bool { return true }

// isAutostartEnabled checks if autostart is enabled (registry OR task scheduler).
func isAutostartEnabled() bool {
	return isAutostartRegistryEnabled() || isAutostartTaskEnabled()
}

// setAutostart enables or disables autostart using both registry and task scheduler.
func setAutostart(enabled bool) error {
	// Try registry first (requires user-level permissions)
	if err := setAutostartRegistry(enabled); err != nil {
		// If registry fails, fall back to task scheduler
		if taskErr := setAutostartTask(enabled); taskErr != nil {
			return fmt.Errorf("registry: %w; task scheduler: %w", err, taskErr)
		}
		return nil
	}

	// Registry succeeded, also update task scheduler for redundancy
	_ = setAutostartTask(enabled)
	return nil
}

// isAutostartRegistryEnabled checks the Windows registry for autostart entry.
func isAutostartRegistryEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRegistryKey, registry.READ)
	if err != nil {
		return false
	}
	defer key.Close()

	val, _, err := key.GetStringValue(autostartAppName)
	return err == nil && strings.TrimSpace(val) != ""
}

// setAutostartRegistry adds or removes the autostart entry in Windows registry.
func setAutostartRegistry(enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autostartRegistryKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open registry key: %w", err)
	}
	defer key.Close()

	if enabled {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		// Use forward slashes for consistency
		exePath = strings.ReplaceAll(exePath, "\\", "/")
		return key.SetStringValue(autostartAppName, fmt.Sprintf("%q --minimized", exePath))
	}

	// Remove the entry
	return key.DeleteValue(autostartAppName)
}

// isAutostartTaskEnabled checks if the task scheduler task exists and is enabled.
func isAutostartTaskEnabled() bool {
	out, err := exec.Command("schtasks", "/query", "/tn", autostartTaskName, "/fo", "CSV", "/nh").Output()
	if err != nil {
		return false
	}
	// If the task exists and contains "Ready" or "Running", it's enabled
	output := strings.ToLower(string(out))
	return strings.Contains(output, "ready") || strings.Contains(output, "running")
}

// setAutostartTask creates or deletes a task scheduler task for autostart.
func setAutostartTask(enabled bool) error {
	if !enabled {
		// Delete the task if it exists
		cmd := exec.Command("schtasks", "/delete", "/tn", autostartTaskName, "/f")
		if err := cmd.Run(); err != nil {
			// Task might not exist, that's okay
			return nil
		}
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Create a task that runs at logon
	cmd := exec.Command(
		"schtasks", "/create",
		"/tn", autostartTaskName,
		"/tr", fmt.Sprintf("%q --minimized", exePath),
		"/sc", "onlogon",
		"/rl", "limited",
		"/f", // Force overwrite if exists
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create task: %w\n%s", err, output)
	}

	return nil
}

// getAutostartExePath returns the path to the executable for autostart.
func getAutostartExePath() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exePath)
}
