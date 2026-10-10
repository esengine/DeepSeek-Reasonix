package control

import (
	"log/slog"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/sessionstore"
)

// AutoArchiveSettings is the user's [auto_archive] section with its defaults
// resolved. Only the user file is read: a project file cannot set it.
type AutoArchiveSettings struct {
	Enabled     bool   `json:"enabled"`
	Days        int    `json:"days"`
	DefaultDays int    `json:"defaultDays"`
	Path        string `json:"path"`
}

func (c *Controller) AutoArchiveSettings() AutoArchiveSettings {
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	return AutoArchiveSettings{
		Enabled:     cfg.AutoArchive.Enabled,
		Days:        cfg.AutoArchiveDays(),
		DefaultDays: config.DefaultAutoArchiveDays,
		Path:        path,
	}
}

// SaveAutoArchiveSettings persists the section. The sweep reads it afresh on
// every pass, so no rebuild is needed.
func (c *Controller) SaveAutoArchiveSettings(in AutoArchiveSettings) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetAutoArchive(config.AutoArchiveConfig{Enabled: in.Enabled, Days: in.Days}); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

// ArchiveInactiveSessions archives this workspace's conversations idle past
// the user's setting and returns how many moved. It is a no-op while the
// setting is off. A conversation this controller holds is never touched, and
// the store skips anything pinned, leased by a live run, or mid-turn.
func (c *Controller) ArchiveInactiveSessions() int {
	dir := c.SessionDir()
	idle := config.LoadForEdit(config.UserConfigPath()).AutoArchiveAfter()
	if dir == "" || idle <= 0 {
		return 0
	}
	current := sessionstore.CanonicalSessionPath(c.SessionPath())
	n, err := sessionstore.ArchiveInactiveSessions(dir, time.Now(), idle, func(path string) bool {
		return sessionstore.CanonicalSessionPath(path) == current
	})
	if err != nil {
		slog.Warn("control: archive inactive sessions", "dir", dir, "err", err)
	}
	if n > 0 {
		slog.Info("control: archived inactive conversations", "count", n)
	}
	return n
}
