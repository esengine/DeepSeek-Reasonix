// Package agentpreset defines the three Agent role settings (角色设定) that
// control planning depth, verification breadth, and independent review
// frequency without changing tool schemas or security boundaries.
package agentpreset

import "strings"

// AgentPreset is a session-scoped role setting. It is independent of
// sub-agent ProfileDefinition values.
type AgentPreset string

const (
	// Light is 轻量 · 快速可靠: direct when simple, targeted verification,
	// independent review only for high risk.
	Light AgentPreset = "light"
	// Balanced is 均衡 · 智能适配: complexity-adaptive planning and review.
	// It is the zero-configuration default for every new entry point.
	Balanced AgentPreset = "balanced"
	// Delivery is 交付 · 证据闭环: full acceptance evidence and forced
	// independent review on medium/high-risk mutations.
	Delivery AgentPreset = "delivery"
)

// PolicyVersion is the host-visible policy schema version embedded in the
// transient execution-policy block. Bump when the block shape changes.
const PolicyVersion = 1

// Normalize maps free-form and legacy values onto a canonical AgentPreset.
// Empty and unknown values fall back to Balanced. One compatibility version
// of old token/work-mode names is accepted.
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

// IsValid reports whether raw is an exact canonical preset name.
func IsValid(raw string) bool {
	switch AgentPreset(strings.ToLower(strings.TrimSpace(raw))) {
	case Light, Balanced, Delivery:
		return true
	default:
		return false
	}
}

// All returns the three canonical presets in stable menu order.
func All() []AgentPreset {
	return []AgentPreset{Light, Balanced, Delivery}
}

// LegacyTokenMode returns the one-version dual-write tokenMode value for
// older clients. Unknown/empty presets map to "full" (historical balanced).
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

// FromLegacyTokenMode maps a persisted or CLI tokenMode onto AgentPreset.
func FromLegacyTokenMode(mode string) AgentPreset {
	return Normalize(mode)
}

// String returns the canonical identifier.
func (p AgentPreset) String() string {
	return string(Normalize(string(p)))
}
