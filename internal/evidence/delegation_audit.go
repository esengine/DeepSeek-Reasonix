package evidence

// DelegationAudit is one completed sub-agent run, recorded as structured data
// so a benchmark can compare orchestration arms without scraping prose. It
// carries what the host observed, never what the child claimed unchecked.
type DelegationAudit struct {
	Depth int
	// ToolCalls and Mutations are host-recorded receipt counts for this child.
	ToolCalls int
	Mutations int
	// MutationPaths lets an aggregator detect two children touching one file.
	MutationPaths []string
	// ClaimViolations counts writes the host saw outside the declared claim.
	ClaimViolations int
	// HasReport is false when the child ended in prose instead of a typed claim.
	HasReport bool
	// AdjudicatedStatus is the status the host was willing to back, and
	// Downgrades counts the criterion claims it refused.
	AdjudicatedStatus string
	Downgrades        int
}

// FalseCompletion reports a child that claimed more than the host could back.
// It is the count of refused criteria, not a comparison against a claimed
// status the host never independently recorded.
func (a DelegationAudit) FalseCompletion() bool {
	return a.HasReport && a.Downgrades > 0
}
