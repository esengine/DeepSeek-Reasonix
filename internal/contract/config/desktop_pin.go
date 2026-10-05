package config

import "strings"

// A rollback pins the desktop to the release the user chose. The pin lives in
// config rather than in updater state so it is visible, editable, and survives
// a reinstall — the point of leaving a bad build is not being put back on it.

// DesktopPinnedVersion returns the pinned release, or "" to follow the channel.
func (c *Config) DesktopPinnedVersion() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Desktop.PinnedVersion)
}

// SetDesktopPinnedVersion pins the desktop to a release, or releases the pin
// when version is empty. Rolling back sets it; only the user clears it.
func (c *Config) SetDesktopPinnedVersion(version string) error {
	c.Desktop.PinnedVersion = strings.TrimSpace(version)
	return nil
}
