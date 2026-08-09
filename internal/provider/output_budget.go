package provider

// OutputBudgetProvider reports the output budget used when a request leaves it unset.
type OutputBudgetProvider interface {
	OutputBudget() int
}

// SharedWindowOutputProvider reports whether input and output share one window.
type SharedWindowOutputProvider interface {
	SharesContextWindow() bool
}

// SharedWindowInputPolicy describes adapter-specific text replayed into a
// shared context window in addition to the fields common to all transports.
type SharedWindowInputPolicy struct {
	ReplaysOrdinaryReasoning bool
	ReplaysResponsesItems    bool
}

// SharedWindowInputPolicyProvider reports adapter-specific replay behavior so
// admission estimates can follow the actual wire without changing its bytes.
type SharedWindowInputPolicyProvider interface {
	SharedWindowInputPolicy() SharedWindowInputPolicy
}
