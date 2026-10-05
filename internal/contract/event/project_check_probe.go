// project_check_probe.go — the two project-check derivations, compared.
package event

import "reasonix/internal/base/nilutil"

// Divergence classes. Each names the one explanation that accounts for a
// disagreement, in the order they are tried: an earlier class rules out the
// ones below it, so a diff never carries a weaker reason than the true one.
const (
	// ProjectCheckBaselinePreservation is a criterion the task began requiring
	// that the current declaration no longer names. The gate cannot see it at
	// all, so this class is the protection working, never a regression.
	ProjectCheckBaselinePreservation = "baseline_preservation"
	// ProjectCheckMutationIndex is a disagreement the two sides' baselines
	// explain: they measure "since the change" from different receipts.
	ProjectCheckMutationIndex = "mutation_index"
	// ProjectCheckIdentityNormalization is a disagreement canonicalisation
	// explains: the raw command and its identity match different receipts.
	ProjectCheckIdentityNormalization = "identity_normalization"
	// ProjectCheckCandidateOnly and ProjectCheckLegacyOnly are the unexplained
	// remainders — the only two classes that are candidate defects.
	ProjectCheckCandidateOnly = "candidate_only"
	ProjectCheckLegacyOnly    = "legacy_only"
)

// ProjectCheckDiff is one criterion the two derivations disagree about, and the
// reason they do. Identity is the project's own declared command, canonicalised
// — the same text the readiness reason already hands the model verbatim.
type ProjectCheckDiff struct {
	Identity string `json:"identity"`
	Class    string `json:"class"`
}

// ProjectCheckProbe is one comparison between the readiness gate's project-check
// derivation and the ledger's obligations. It observes and decides nothing, so a
// class here is a question to answer, never a block that happened. One probe per
// readiness evaluation, not per turn: a goal turn evaluates readiness more than
// once, and the denominator has to be what was actually compared.
type ProjectCheckProbe struct {
	// Declared and Baseline are how many criteria each declaration names, so a
	// run with nothing declared is not counted as agreement.
	Declared int
	Baseline int
	// LegacyBlocked and CandidateBlocked are the two stop decisions. Parity
	// between them is the headline; Diffs is why it was or was not reached.
	LegacyBlocked    bool
	CandidateBlocked bool
	// AgreedMissing counts criteria both derivations call unmet.
	AgreedMissing int
	Diffs         []ProjectCheckDiff
	// LegacyAfter and CandidateAfter are the receipt indices each side measured
	// after. They differ by construction, so a divergence they explain is not a
	// defect in either derivation.
	LegacyAfter    int
	CandidateAfter int
}

// ProjectCheckProbeSink is an optional sink capability for the shadow
// comparison; implementations must not let it reach a user-facing channel.
type ProjectCheckProbeSink interface {
	RecordProjectCheckProbe(ProjectCheckProbe)
}

// RecordProjectCheckProbe forwards one comparison to sinks that opt in.
func RecordProjectCheckProbe(s Sink, p ProjectCheckProbe) {
	if nilutil.IsNil(s) {
		return
	}
	if ps, ok := s.(ProjectCheckProbeSink); ok {
		ps.RecordProjectCheckProbe(p)
	}
}
