package config

import (
	"os"
	"path/filepath"
)

// LastKnownGoodConfigPath is the fixed recovery snapshot of the user config,
// maintained by the Guard repair pipeline. The updater's minimal network
// loader falls back to it when the live user config cannot be parsed, so a
// damaged config never blocks checking for or installing updates.
func LastKnownGoodConfigPath() string {
	if root := MemoryUserDir(); root != "" {
		return filepath.Join(root, "repair", "config.toml.last-known-good")
	}
	return ""
}

// LoadUpdateNetworkConfig builds the minimal configuration the updater needs:
// update channel, proxy, CA and network fields only. It never reads project
// reasonix.toml files, plugins, MCP or prompt configuration, so even a config
// damaged by a plugin path still allows checking for updates, downloading an
// installer, or entering the recovery UI.
//
// Resolution order:
//
//  1. the live user-global config;
//  2. the last-known-good snapshot when the live config cannot be parsed;
//  3. environment proxy settings with built-in network defaults.
func LoadUpdateNetworkConfig() (*Config, error) {
	if SafeModeRequested() {
		return loadSafeModeForRoot("."), nil
	}
	cfg, err := LoadUserConfigReadOnly()
	if err == nil {
		return cfg, nil
	}
	if lkg := LastKnownGoodConfigPath(); lkg != "" {
		if b, readErr := os.ReadFile(lkg); readErr == nil {
			candidate := Default()
			if _, decodeErr := decodeTOMLBytes(b, candidate); decodeErr == nil {
				return candidate, nil
			}
		}
	}
	// Environment proxy settings are already honored by the default network
	// configuration (NetworkProxyMode defaults to "env").
	return Default(), nil
}
