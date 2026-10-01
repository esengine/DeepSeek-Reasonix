package config

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// AutoArchiveConfig archives conversations nobody has touched for a while. It
// is user-global and off by default: a repository's reasonix.toml cannot hide
// the user's conversations, so load restores the user's value over a project's.
type AutoArchiveConfig struct {
	Enabled bool `toml:"enabled"`
	Days    int  `toml:"days"` // idle days before archiving; zero is the default
}

const (
	DefaultAutoArchiveDays = 30
	maxAutoArchiveDays     = 3650
)

// ErrAutoArchiveOutOfRange marks an idle period the setting does not accept.
var ErrAutoArchiveOutOfRange = errors.New("auto archive setting out of range")

// AutoArchiveDays is the effective idle period in days.
func (c *Config) AutoArchiveDays() int {
	if c == nil || c.AutoArchive.Days <= 0 {
		return DefaultAutoArchiveDays
	}
	return c.AutoArchive.Days
}

// AutoArchiveAfter is the idle period after which a conversation is archived,
// or zero while the setting is off.
func (c *Config) AutoArchiveAfter() time.Duration {
	if c == nil || !c.AutoArchive.Enabled {
		return 0
	}
	return time.Duration(c.AutoArchiveDays()) * 24 * time.Hour
}

// SetAutoArchive validates and stores the setting. Zero days keeps the
// default, so a blank field cannot archive everything.
func (c *Config) SetAutoArchive(a AutoArchiveConfig) error {
	if a.Days < 0 || a.Days > maxAutoArchiveDays {
		return fmt.Errorf("%w: days must be between 1 and %d", ErrAutoArchiveOutOfRange, maxAutoArchiveDays)
	}
	c.AutoArchive = a
	return nil
}

func renderAutoArchiveSection(b *strings.Builder, c *Config) {
	if c.AutoArchive == (AutoArchiveConfig{}) {
		return
	}
	b.WriteString("[auto_archive]\n")
	fmt.Fprintf(b, "enabled = %v   # archive conversations idle for `days`; reversible, default off\n", c.AutoArchive.Enabled)
	fmt.Fprintf(b, "days = %d   # idle days before a conversation is archived\n", c.AutoArchiveDays())
	b.WriteString("\n")
}
