// Package agentpreset holds the deprecated execution-mode vocabulary kept for
// one compatibility version. Reasonix now runs a single adaptive standard
// host execution (see internal/runtimepolicy); these helpers exist only so old
// CLI flags, ACP options, persisted sessions, and Desktop tabs can be parsed,
// answered with a deprecation notice, and dual-written with safe values for
// older clients. Nothing here may influence runtime behavior.
package agentpreset

import "strings"

// AgentPreset is a deprecated session-scoped execution-mode label.
type AgentPreset string

const (
	// Light is the deprecated 轻量 mode label.
	Light AgentPreset = "light"
	// Balanced is the deprecated 均衡 mode label. It is also the fixed safe
	// value written into new persisted compat fields.
	Balanced AgentPreset = "balanced"
	// Delivery is the deprecated 交付 mode label.
	Delivery AgentPreset = "delivery"
)

// Normalize maps free-form and legacy values onto a canonical label. Empty and
// unknown values fall back to Balanced. Old token/work-mode names are accepted
// for one compatibility version.
func Normalize(raw string) AgentPreset {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(Light), "economy", "eco", "save", "saving", "low", "lite", "minimal":
		return Light
	case string(Delivery), "deliver", "quality", "performance":
		return Delivery
	case string(Balanced), "full", "":
		return Balanced
	default:
		return Balanced
	}
}

// IsValid reports whether raw is an exact canonical preset label.
func IsValid(raw string) bool {
	switch AgentPreset(strings.ToLower(strings.TrimSpace(raw))) {
	case Light, Balanced, Delivery:
		return true
	default:
		return false
	}
}

// LegacyTokenMode returns the deprecated dual-write tokenMode value older
// clients expect next to a persisted preset. It is a wire-compat mapping only.
func LegacyTokenMode(p AgentPreset) string {
	switch Normalize(string(p)) {
	case Light:
		return "economy"
	case Delivery:
		return "delivery"
	default:
		return "full"
	}
}

// FromLegacyTokenMode maps a persisted or CLI tokenMode onto a preset label.
func FromLegacyTokenMode(mode string) AgentPreset {
	return Normalize(mode)
}

// DeprecatedNotice is the one-time-per-process notice old entry points print
// after accepting a mode argument.
const DeprecatedNotice = "Reasonix now uses one adaptive standard execution: planning, verification, and review strength follow task risk. Execution modes are no longer switchable; this option is accepted for compatibility and ignored."

// String returns the canonical identifier.
func (p AgentPreset) String() string {
	return string(Normalize(string(p)))
}
