package main

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

// editUserConfig runs mutate against the user-global config under the edit lock
// and saves it there. Remote hosts are user-global (pinned in LoadForRoot).
func editUserConfig(mutate func(*config.Config) error) error {
	return editUserConfigIfChanged(func(cfg *config.Config) (bool, error) {
		if err := mutate(cfg); err != nil {
			return false, err
		}
		return true, nil
	})
}

// editUserConfigIfChanged keeps both the read and the no-op decision inside
// the edit lock. Callers can avoid rewriting an unchanged file without a
// stale read racing another process-local config mutation.
func editUserConfigIfChanged(mutate func(*config.Config) (bool, error)) error {
	path := config.UserConfigPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("cannot resolve user config path")
	}
	return editUserConfigIfChangedAtPath(path, config.LockConfigFileEdits, mutate)
}

// editUserConfigIfChangedAtPath is the strict read-modify-write transaction.
// The error-returning lock API is required even for a no-op mutation: unlike
// Config.SaveTo, that path has no later write at which to surface a failed
// cross-process lock acquisition.
func editUserConfigIfChangedAtPath(
	path string,
	lock func(string) (func(), error),
	mutate func(*config.Config) (bool, error),
) error {
	unlock, err := lock(path)
	if err != nil {
		return fmt.Errorf("lock user config edits: %w", err)
	}
	defer unlock()
	cfg := config.LoadForEdit(path)
	if cfg == nil {
		cfg = config.Default()
	}
	changed, err := mutate(cfg)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return cfg.SaveTo(path)
}
