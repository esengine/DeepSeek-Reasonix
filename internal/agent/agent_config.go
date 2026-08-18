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
	maxOutputTokens    int
	temperature        float64
	usageSource        string
	modelRef           string
	// workspaceID is a prompt-cache lineage component, so it must not move
	// while an agent lives — a change would silently rekey the cache.
	workspaceID string
	// classifierTaskText is the host-trusted task text for delivery intent
	// classification, set by sub-agent spawners whose Run input carries host
	// framing. Empty means classify the raw input verbatim.
	classifierTaskText string
	// writeWorkspaceRoot scopes write reservations when writeScheduler is set.
	writeWorkspaceRoot string
	// subagentDepth caps delegation; at maxSubagentDepth the recursive
	// agent/skill tools are excluded.
	subagentDepth    int
	maxSubagentDepth int
	// contextWindow and compactRatio decide when at most one provider-visible
	// checkpoint is installed; recentKeep and archiveDir shape what it keeps.
	contextWindow          int
	compactRatio           float64
	recentKeep             int
	archiveDir             string
	legacyAnchorSafetyGate bool
}
