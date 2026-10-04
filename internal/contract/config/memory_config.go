package config

// MemoryConfig bounds what saved memory costs, and every axis ships off. Unset
// means unbounded rather than a number picked here; `/memory` reports what the
// current store actually costs.
type MemoryConfig struct {
	// PinnedBudgetChars caps total pinned-body runes at write time. 0 = off.
	PinnedBudgetChars int `toml:"pinned_budget_chars"`
	// RecallLimit is how many facts automatic recall may inject per turn.
	// 0 selects the default (4).
	RecallLimit int `toml:"recall_limit"`
	// RecallMaxChars bounds that injection's total size. 0 selects the
	// default (2400). Each hit's snippet gets an equal share.
	RecallMaxChars int `toml:"recall_max_chars"`
	// AutoConfirmProjectRemember skips the confirmation dialog for remember
	// writes that land in this project's memory. Off by default, like every
	// other axis here: the dialog is what ships.
	AutoConfirmProjectRemember bool `toml:"auto_confirm_project_remember"`
}
