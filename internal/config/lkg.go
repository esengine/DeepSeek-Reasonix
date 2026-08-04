package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// LastKnownGoodConfigPath is the fixed path of the most recent verified user
// config snapshot. Written by repair.RecordHealthyConfig after a successful
// desktop boot; used only as an in-memory recovery source when the live file
// cannot be parsed. The original user config is never overwritten by a load.
func LastKnownGoodConfigPath() string {
	root := MemoryUserDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "repair", "config.toml.last-known-good")
}

// loadLastKnownGoodUserConfig merges a validated LKG snapshot into cfg.
// Returns an error when no usable snapshot exists.
func loadLastKnownGoodUserConfig(cfg *Config) error {
	path := LastKnownGoodConfigPath()
	if path == "" {
		return fmt.Errorf("last-known-good path unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := ValidateBytes(data); err != nil {
		return err
	}
	if _, err := decodeTOMLBytes(data, cfg); err != nil {
		return fmt.Errorf("last-known-good: %w", err)
	}
	return nil
}
