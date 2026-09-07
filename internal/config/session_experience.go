package config

import (
	"fmt"
	"strings"
)

// SessionExperience is the single user-facing desktop presentation preference.
// It intentionally combines the old transcript density, reasoning display, and
// process-fold controls into two complete reading strategies.
type SessionExperience string

const (
	SessionExperienceStandard SessionExperience = "standard"
	SessionExperienceDeep     SessionExperience = "deep"
)

// DesktopSessionExperience returns the canonical desktop preference. Missing
// and legacy-only configurations deliberately migrate to standard: the new
// setting must not inherit a surprising combination of old independent flags.
func (c *Config) DesktopSessionExperience() string {
	if c == nil {
		return string(SessionExperienceStandard)
	}
	switch strings.ToLower(strings.TrimSpace(c.Desktop.SessionExperience)) {
	case string(SessionExperienceDeep):
		return string(SessionExperienceDeep)
	case string(SessionExperienceStandard):
		return string(SessionExperienceStandard)
	default:
		return string(SessionExperienceStandard)
	}
}

// DesktopDisplayMode keeps the old density snapshot coherent for one release.
func (c *Config) DesktopDisplayMode() string {
	if c != nil && strings.TrimSpace(c.Desktop.SessionExperience) != "" {
		return "standard"
	}
	switch strings.ToLower(strings.TrimSpace(c.Desktop.DisplayMode)) {
	case "compact", "minimal":
		return "compact"
	default:
		return "standard"
	}
}

// SetDesktopSessionExperience persists the canonical desktop presentation
// preference and keeps the deprecated fields coherent for older clients.
func (c *Config) SetDesktopSessionExperience(mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(SessionExperienceStandard):
		c.Desktop.SessionExperience = string(SessionExperienceStandard)
		c.Desktop.DisplayMode = string(SessionExperienceStandard)
		c.Desktop.ReasoningDisplayMode = "auto"
		c.Desktop.ExpandThinking = true
		return nil
	case string(SessionExperienceDeep):
		c.Desktop.SessionExperience = string(SessionExperienceDeep)
		c.Desktop.DisplayMode = string(SessionExperienceStandard)
		c.Desktop.ReasoningDisplayMode = "expanded"
		c.Desktop.ExpandThinking = true
		return nil
	default:
		return fmt.Errorf("session experience %q: must be standard|deep", mode)
	}
}
