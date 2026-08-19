// Package agentpreset holds the session role vocabulary. The role is a
// session quality floor (standard|delivery): it records intent and writes
// through the controller's floor setting; enforcement lives in the fact
// contract. Light and its historical aliases fold to standard silently.
package agentpreset

import (
	"fmt"
	"strings"
)

// AgentPreset is the session role label.
type AgentPreset string

const (
	// Standard is the 默认 (standard) floor: the adaptive policy unchanged.
	Standard AgentPreset = "standard"
	// Delivery is the 交付 floor: session-sticky delivery completion gates.
	Delivery AgentPreset = "delivery"
)

// Normalize maps free-form and legacy values onto the canonical label. Light
// and its aliases fold to Standard; unknown values are an error so no new
// vocabulary can appear.
func Normalize(raw string) (AgentPreset, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(Standard), "balanced", "full", "normal",
		"light", "economy", "eco", "save", "saving", "low", "lite", "minimal":
		return Standard, nil
	case string(Delivery), "deliver", "quality":
		return Delivery, nil
	default:
		return "", fmt.Errorf("unknown role setting %q (accepted: standard, delivery; legacy light folds to standard)", raw)
	}
}

// IsValid reports whether raw is an exact canonical preset label.
func IsValid(raw string) bool {
	p, err := Normalize(raw)
	return err == nil && p != ""
}

// LegacyTokenMode returns the deprecated dual-write tokenMode value older
// clients expect next to a persisted preset. It is a wire-compat mapping only.
func LegacyTokenMode(p AgentPreset) string {
	if p == Delivery {
		return "delivery"
	}
	return "full"
}

// FromLegacyTokenMode maps a persisted or CLI tokenMode onto a preset label.
func FromLegacyTokenMode(mode string) AgentPreset {
	p, err := Normalize(mode)
	if err != nil {
		return Standard
	}
	return p
}

// FloorNotice is printed once when a legacy mode value is folded.
const FloorNotice = "The execution-mode setting is now a session quality floor: standard (default) or delivery. Light has been folded into standard; the adaptive policy already runs light work lightly."

// String returns the canonical identifier.
func (p AgentPreset) String() string {
	return string(p)
}
