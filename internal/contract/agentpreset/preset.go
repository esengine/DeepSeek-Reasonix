// Package agentpreset defines the two Agent role settings (角色设定) that
// control verification breadth without changing tool schemas or security
// boundaries.
package agentpreset

import "strings"

// AgentPreset is a session-scoped role setting. It is independent of
// sub-agent ProfileDefinition values.
type AgentPreset string

const (
	// Balanced is 均衡 · 智能适配: complexity-adaptive planning and review.
	// It is the zero-configuration default for every new entry point.
	Balanced AgentPreset = "balanced"
	// Delivery is 交付 · 证据闭环: full acceptance evidence, plus the review
	// the turn's own mutation receipts turn out to owe.
	Delivery AgentPreset = "delivery"
)

// PolicyVersion is the host-visible policy schema version embedded in the
// transient execution-policy block. Bump when the block shape changes.
const PolicyVersion = 3

// Normalize maps free-form and legacy values onto a canonical AgentPreset.
// Empty, unknown, and the retired "light"/"economy" names answer Balanced:
// light's only enforced differences were two sub-agent switches, so it cost
// every user a choice without changing what its name promised.
func Normalize(raw string) AgentPreset {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(Delivery), "deliver", "quality", "performance":
		return Delivery
	default:
		return Balanced
	}
}

// IsValid reports whether raw is an exact canonical preset name.
func IsValid(raw string) bool {
	switch AgentPreset(strings.ToLower(strings.TrimSpace(raw))) {
	case Balanced, Delivery:
		return true
	default:
		return false
	}
}

// All returns the canonical presets in stable menu order.
func All() []AgentPreset {
	return []AgentPreset{Balanced, Delivery}
}

// LegacyTokenMode returns the one-version dual-write tokenMode value for
// older clients. Unknown/empty presets map to "full" (historical balanced).
func LegacyTokenMode(p AgentPreset) string {
	if Normalize(string(p)) == Delivery {
		return "delivery"
	}
	return "full"
}

// FromLegacyTokenMode maps a persisted or CLI tokenMode onto AgentPreset.
func FromLegacyTokenMode(mode string) AgentPreset {
	return Normalize(mode)
}

// String returns the canonical identifier.
func (p AgentPreset) String() string {
	return string(Normalize(string(p)))
}
