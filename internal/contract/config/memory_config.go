package config

import "strings"

// MemoryConfig bounds what saved memory costs, and every axis ships off. The
// prefix index and pinned bodies are paid for once per session and automatic
// recall rides each turn's tail; how much of that is worth paying is the
// user's call, so unset means unbounded rather than a number picked here.
// `/memory` reports what the current store actually costs.
type MemoryConfig struct {
	// PinnedBudgetChars caps total pinned-body runes at write time. 0 = off.
	PinnedBudgetChars int `toml:"pinned_budget_chars"`
	// RecallLimit is how many facts automatic recall may inject per turn.
	// 0 selects the default (4).
	RecallLimit int `toml:"recall_limit"`
	// RecallMaxChars bounds that injection's total size. 0 selects the
	// default (2400). Each hit's snippet gets an equal share.
	RecallMaxChars int `toml:"recall_max_chars"`
	// Proactive says what the model may do on its own: "ask" holds what it
	// wants to keep for the user to accept, "auto" lets it write, "off" does
	// not offer it the remember tool. Empty is "ask".
	Proactive string `toml:"proactive"`
}

// MemoryProactive normalizes memory.proactive to ask, auto or off.
func (c *Config) MemoryProactive() string {
	switch strings.ToLower(strings.TrimSpace(c.Memory.Proactive)) {
	case "auto":
		return "auto"
	case "off":
		return "off"
	default:
		return "ask"
	}
}
