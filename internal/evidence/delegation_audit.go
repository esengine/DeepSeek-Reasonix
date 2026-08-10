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
	// ParentScopeHints counts directories the delegation narrowed the search to.
	ParentScopeHints int
	// ParentNamedFiles counts specific files the delegation text handed over.
	ParentNamedFiles int
	// EvidencePaths is every distinct path the child produced a receipt for.
	EvidencePaths int
	// DiscoveredPaths is the subset of those no named file already covered.
	DiscoveredPaths int
}

// ClassifyEvidenceOrigin scores one run against the text its parent wrote.
// Discovery is judged against named files only: a child sent to a directory
// still had to work out which file in it mattered. Counts only — a rate is a
// ratio of sums across runs, and averaging per-run rates would weight a child
// that read two files like one that read forty.
func (a *DelegationAudit) ClassifyEvidenceOrigin(delegationText string, evidencePaths []string) {
	scope, files := SplitNamedPaths(NamedPaths(delegationText))
	a.ParentScopeHints = len(scope)
	a.ParentNamedFiles = len(files)
	a.EvidencePaths = len(evidencePaths)
	a.DiscoveredPaths = 0
	for _, p := range evidencePaths {
		if !UnderNamedPath(files, p) {
			a.DiscoveredPaths++
		}
	}
}

// FalseCompletion reports a child that claimed more than the host could back.
// It is the count of refused criteria, not a comparison against a claimed
// status the host never independently recorded.
func (a DelegationAudit) FalseCompletion() bool {
	return a.HasReport && a.Downgrades > 0
}
