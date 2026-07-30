//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// telemetryInstallProfile returns only a bounded distribution label. Executable
// paths are never included in telemetry or reports.
func telemetryInstallProfile() string {
	executable, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	normalized := strings.ToLower(filepath.Clean(executable))
	if strings.Contains(normalized, `\scoop\apps\reasonix-desktop\`) ||
		strings.Contains(normalized, `\scoop\apps\reasonix\`) {
		return "scoop"
	}
	// The NSIS package always places its per-user uninstaller beside the
	// executable, including when the user selected a custom install directory.
	if info, statErr := os.Stat(filepath.Join(filepath.Dir(executable), "uninstall.exe")); statErr == nil && !info.IsDir() {
		return "installer"
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		installed := strings.ToLower(filepath.Join(localAppData, "Programs", "Reasonix"))
		if normalized == installed || strings.HasPrefix(normalized, installed+string(os.PathSeparator)) {
			return "installer"
		}
	}
	return "portable"
}
