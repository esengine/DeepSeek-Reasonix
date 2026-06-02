package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"reasonix/internal/config"
)

// desktopSettings holds desktop-only preferences (auto-start, silent-start)
// that live outside the project-scoped reasonix.toml.
type desktopSettings struct {
	AutoStart   bool `json:"autoStart"`
	SilentStart bool `json:"silentStart"`
}

var (
	ds     desktopSettings
	dsMu   sync.RWMutex
	dsPath string // resolved once by loadDesktopSettings
)

// desktopSettingsPath returns the persistent settings file path.
func desktopSettingsPath() string {
	dir := config.MemoryUserDir() // ~/.config/reasonix (or %APPDATA%/reasonix)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "desktop-settings.json")
}

// loadDesktopSettings reads the persisted settings from disk. Thread-safe.
func loadDesktopSettings() {
	dsMu.Lock()
	defer dsMu.Unlock()

	dsPath = desktopSettingsPath()
	if dsPath == "" {
		return
	}
	b, err := os.ReadFile(dsPath)
	if err != nil {
		return // file doesn't exist yet — use defaults (both false)
	}
	_ = json.Unmarshal(b, &ds)
}

// saveDesktopSettings writes the current settings to disk. Thread-safe.
func saveDesktopSettings() error {
	dsMu.RLock()
	defer dsMu.RUnlock()

	if dsPath == "" {
		dsPath = desktopSettingsPath()
	}
	if dsPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dsPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(&ds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dsPath, b, 0o644)
}

// getDesktopSettings returns a copy of the current settings.
func getDesktopSettings() desktopSettings {
	dsMu.RLock()
	defer dsMu.RUnlock()
	return ds
}

// setDesktopSettings replaces the persisted settings atomically.
func setDesktopSettings(s desktopSettings) error {
	if s.AutoStart != ds.AutoStart {
		// Apply the OS-level change before saving.
		if err := SetAutoStart(s.AutoStart); err != nil {
			return err
		}
	}
	dsMu.Lock()
	ds = s
	dsMu.Unlock()
	return saveDesktopSettings()
}
