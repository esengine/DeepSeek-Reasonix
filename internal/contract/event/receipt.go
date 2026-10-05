package event

// CompletionReceipt is the user-facing completion record on TurnDone: what the
// host could verify about the turn's work, and — the part no prose reliably
// carries — what it could not. Unlike the shadow audit this holds content,
// because a receipt naming no file and no command tells the reader nothing.
type CompletionReceipt struct {
	Verdict       string
	Changes       []ReceiptChange
	Verifications []ReceiptVerification
	Gaps          []ReceiptGap
	// Risks and Unverified are the turn's own declarations, kept apart from
	// Gaps: a gap is something the host found missing, a declaration is
	// something the turn volunteered, and neither reads as the other.
	Risks      []string
	Unverified []string
}

// ReceiptChange is one mutated path and whether anything looked at it again.
type ReceiptChange struct {
	Path     string
	Reviewed bool
}

// ReceiptVerification is a verification command's last outcome. Stale means it
// ran before the newest change, so it proves nothing about the current tree;
// Inconclusive means the shell reported a later stage's status instead of this
// check's, so it proves nothing either.
type ReceiptVerification struct {
	Command      string
	Passed       bool
	Stale        bool
	Inconclusive bool
}

// ReceiptGap is one thing the receipt refuses to present as verified.
type ReceiptGap struct {
	Kind   string
	Detail string
}

// SaysSomething reports whether this receipt has anything a person needs. It
// lives here for the reason NeedsAttention does: two frontends deciding it
// separately is two answers to one question. A receipt with no gaps, no
// declarations and no clean verdict says only that the host could not judge,
// which a transcript that already showed every step does not need repeated.
func (r *CompletionReceipt) SaysSomething() bool {
	if r == nil {
		return false
	}
	return len(r.Gaps) > 0 || len(r.Risks) > 0 || len(r.Unverified) > 0 || r.Verdict == "done"
}
