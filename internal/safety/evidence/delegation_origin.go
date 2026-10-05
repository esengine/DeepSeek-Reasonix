package evidence

import "reasonix/internal/contract/hostaudit"

// ClassifyEvidenceOrigin scores one run against the text its parent wrote.
// Discovery is judged against named files only: a child sent to a directory
// still had to work out which file in it mattered. Counts only — a rate is a
// ratio of sums across runs, and averaging per-run rates would weight a child
// that read two files like one that read forty.
func ClassifyEvidenceOrigin(a *hostaudit.DelegationAudit, delegationText string, evidencePaths []string) {
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
