package hostaudit

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
	// ParentScopeHints counts directories the delegation narrowed the search to.
	ParentScopeHints int
	// ParentNamedFiles counts specific files the delegation text handed over.
	ParentNamedFiles int
	// EvidencePaths is every distinct path the child produced a receipt for.
	EvidencePaths int
	// DiscoveredPaths is the subset of those no named file already covered.
	DiscoveredPaths int
}
