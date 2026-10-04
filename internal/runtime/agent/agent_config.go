package agent

// agentConfig is everything New fixes for an Agent's lifetime; nothing writes
// it afterwards, which agent_config_test.go enforces. Embedded rather than
// nested so access stays flat, as perTurnState already does. Separating it lets
// the struct-state ratchet count reachable states instead of size: an immutable
// field combines with nothing. Anything that changes after New goes on Agent.
type agentConfig struct {
	maxSteps           int
	maxStepsKey        string
	reasoningByteLimit int
	// perseverationGuard enables the loop detector (on by default);
	// perseverationMaxRetries is the cut-and-retry budget (0 = report only,
	// >0 = cut and nudge).
	perseverationGuard      bool
	perseverationMaxRetries int
	maxOutputTokens         int
	temperature             float64
	usageSource             string
	modelRef                string
	// workspaceID is a prompt-cache lineage component, so it must not move
	// while an agent lives — a change would silently rekey the cache.
	workspaceID string
	// classifierTaskText is the host-trusted task text for delivery intent
	// classification, set by sub-agent spawners whose Run input carries host
	// framing. Empty means classify the raw input verbatim.
	classifierTaskText string
	// writeWorkspaceRoot scopes write reservations when writeScheduler is set.
	writeWorkspaceRoot string
	// observeRoot is the workspace an unclassified call's effect is observed in.
	observeRoot string
	// workspaceScanLimit bounds the observation walk; zero means workspaceScanLimit.
	workspaceScanLimit int
	// renderRoot opens written pages for a look; empty owes no look.
	renderRoot string
	// workspaceVCS rides the turn block; in the prefix it would diverge it.
	workspaceVCS string
	// subagentDepth caps delegation; at maxSubagentDepth the recursive
	// agent/skill tools are excluded.
	subagentDepth    int
	maxSubagentDepth int
	// contextWindow and compactRatio decide when at most one provider-visible
	// checkpoint is installed; recentKeep and archiveDir shape what it keeps.
	contextWindow int
	compactRatio  float64
	recentKeep    int
	archiveDir    string
	budgets       CompactionBudgets
}

// CompactionBudgets are the fold's user-owned bounds: what it may hold
// verbatim, and how small its result must be. A zero field selects the
// built-in default, so an unconfigured agent behaves exactly as before.
type CompactionBudgets struct {
	UserTurnKeepTokens     int
	FirstTurnPinTokens     int
	CheckpointCeilingRatio float64
	// ContextSoftLimitTokens is the economic maintenance boundary: the visible
	// input size at which replaying the prompt each round stops being worth it,
	// whatever window the provider declares. Negative disables that boundary.
	ContextSoftLimitTokens int
}
