package config

import (
	"errors"
	"strings"
)

// The update modes, from least to most automatic. Manual is the default and
// what any other value reads as: a misspelt or hand-edited mode must never turn
// background work on.
const (
	UpdateModeManual = "manual"
	UpdateModeNotify = "notify"
	// UpdateModeDownload also fetches and verifies a newer release in the
	// background and leaves it waiting. It never installs or restarts.
	UpdateModeDownload = "download"
)

// ErrUpdateModeInvalid is a mode this build does not offer.
var ErrUpdateModeInvalid = errors.New("config: unknown update mode")

// DesktopUpdateMode is how the desktop behaves about new releases: manual does
// nothing on its own, notify (the default) reads the version catalog in the
// background and says so when something newer is published, download also
// leaves that release downloaded and verified. None of them installs or restarts.
func (c *Config) DesktopUpdateMode() string {
	if m := c.chosenUpdateMode(); m != "" {
		return m
	}
	return UpdateModeNotify
}

// chosenUpdateMode is the mode the user picked, or "" while none is on file, so
// that a choice of manual outlives a default that is not manual.
func (c *Config) chosenUpdateMode() string {
	if c == nil {
		return ""
	}
	switch m := strings.TrimSpace(c.Desktop.UpdateMode); m {
	case UpdateModeManual, UpdateModeNotify, UpdateModeDownload:
		return m
	}
	return ""
}

// SetDesktopUpdateMode records the user's choice. An unknown mode is refused
// rather than stored, so the file never holds a value that reads as something
// the user did not pick.
func (c *Config) SetDesktopUpdateMode(mode string) error {
	switch m := strings.TrimSpace(mode); m {
	case UpdateModeManual, UpdateModeNotify, UpdateModeDownload:
		c.Desktop.UpdateMode = m
		return nil
	}
	return ErrUpdateModeInvalid
}
