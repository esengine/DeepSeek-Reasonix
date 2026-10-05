package control

import "reasonix/internal/contract/config"

// displayPrefs is what a session shows, as opposed to what it does. Grouping
// them costs one field where loose ones cost one each, and the grouping is not
// cosmetic: independent flags multiply into states no type records as legal,
// which is what the struct-state ceiling exists to stop.
type displayPrefs struct {
	// Preferences a frontend sets mid-session; empty means follow the language
	// policy of the current user turn.
	responseLanguage  string
	reasoningLanguage string
	// embeddedDiffDetection is resolved once at build from the config the
	// session was built with; a frontend rebuilding a transcript reads it
	// rather than reloading config. See Options.EmbeddedDiffDetection.
	embeddedDiffDetection bool
}

func displayPrefsFrom(opts Options) displayPrefs {
	return displayPrefs{
		responseLanguage:      config.NormalizeLanguage(opts.ResponseLanguage),
		reasoningLanguage:     config.NormalizeReasoningLanguage(opts.ReasoningLanguage),
		embeddedDiffDetection: opts.EmbeddedDiffDetection,
	}
}

// EmbeddedDiffDetectionEnabled reports whether this session marks a shell
// result whose whole output is a unified diff. It is the value the session was
// built with, so a /history rebuild renders what the live sink did without
// reloading (and possibly rewriting) config.
func (c *Controller) EmbeddedDiffDetectionEnabled() bool {
	return c.display.embeddedDiffDetection
}
