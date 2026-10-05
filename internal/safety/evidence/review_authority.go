package evidence

import (
	"slices"
	"strings"
)

// ReviewAuthority is what the host allowed one execution to prove. It is
// granted before that execution starts and stamped onto the receipts it
// produces, so what a report may close is decided by the host and never by a
// label the worker chose for its own payload.
type ReviewAuthority struct {
	// Satisfies are the review obligations a report from this execution may
	// close. Empty means the execution may report and prove nothing.
	Satisfies []ReviewKind `json:"satisfies,omitempty"`
}

// GrantReviewAuthority builds a grant over the given obligations.
func GrantReviewAuthority(kinds ...ReviewKind) ReviewAuthority {
	return ReviewAuthority{Satisfies: slices.Clone(kinds)}
}

// Proves reports whether this grant covers the obligation.
func (a ReviewAuthority) Proves(kind ReviewKind) bool {
	return slices.Contains(a.Satisfies, kind)
}

// ReceiptProves reports whether the host granted the execution behind this
// receipt the authority to close this obligation. A missing grant and a missing
// execution both mean nothing is proved: ReportKind cannot stand in for the
// first, since a worker contracted for one kind was once able to submit the
// other, and evidence nothing can be joined back to is not the second.
func ReceiptProves(r Receipt, kind ReviewKind) bool {
	return r.Success && r.ToolName == reviewReportTool &&
		strings.TrimSpace(r.SourceExecutionID) != "" &&
		r.ReviewAuthority != nil && r.ReviewAuthority.Proves(kind)
}

// ReviewProofGap says why a report of some kind on this ledger does not close
// its obligation. The host knows which of these happened; the model can only
// see that its report did not count, and would invent a rule for it.
type ReviewProofGap int

const (
	// ReviewProofNone: no report of that kind was submitted at all.
	ReviewProofNone ReviewProofGap = iota
	// ReviewProofUnattributed: submitted by a run the host could not name.
	ReviewProofUnattributed
	// ReviewProofUngranted: submitted by a run granted no such authority.
	ReviewProofUngranted
)

// ReviewProof is why the most recent report of a kind does not close its
// obligation, and which run submitted it. The id is what makes the gap
// actionable — "some report did not count" and "the report execution-3 filed
// carries no grant for this" are different things to be told.
type ReviewProof struct {
	Kind              ReviewKind
	Gap               ReviewProofGap
	SourceExecutionID string
}

// ReviewProofGapFor classifies the most recent report of this kind that failed
// to prove. A report that did prove leaves no gap.
func (l *Ledger) ReviewProofGapFor(kind ReviewKind) ReviewProofGap {
	return l.ReviewProofFor(kind).Gap
}

// ReviewProofFor is the same classification with the offending run named. One
// walk, one rule: the gap and the id must describe the same receipt, and two
// passes over the ledger could disagree about which one that was.
func (l *Ledger) ReviewProofFor(kind ReviewKind) ReviewProof {
	out := ReviewProof{Kind: kind}
	if l == nil {
		return out
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if !r.Success || r.ToolName != reviewReportTool || r.ReportKind != kind {
			continue
		}
		switch {
		case ReceiptProves(r, kind):
			out.Gap, out.SourceExecutionID = ReviewProofNone, ""
		case strings.TrimSpace(r.SourceExecutionID) == "":
			out.Gap, out.SourceExecutionID = ReviewProofUnattributed, ""
		default:
			out.Gap, out.SourceExecutionID = ReviewProofUngranted, strings.TrimSpace(r.SourceExecutionID)
		}
	}
	return out
}
