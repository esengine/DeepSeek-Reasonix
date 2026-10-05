package eventwire

import "reasonix/internal/contract/event"

// CompletionReceipt is the JSON form of event.CompletionReceipt: what the host
// verified about a turn's work, and what it could not.
type CompletionReceipt struct {
	Verdict       string                `json:"verdict"`
	Changes       []ReceiptChange       `json:"changes,omitempty"`
	Verifications []ReceiptVerification `json:"verifications,omitempty"`
	Gaps          []ReceiptGap          `json:"gaps,omitempty"`
	// Risks and Unverified are the turn's declarations, apart from the gaps the
	// host found: a reader that folds them together reads a volunteered caveat
	// as a failure.
	Risks      []string `json:"risks,omitempty"`
	Unverified []string `json:"unverified,omitempty"`
	// SaysSomething is the kernel's answer to "is this worth showing at all",
	// carried rather than recomputed so no frontend drifts from the others.
	SaysSomething bool `json:"saysSomething"`
}

type ReceiptChange struct {
	Path     string `json:"path"`
	Reviewed bool   `json:"reviewed"`
}

type ReceiptVerification struct {
	Command string `json:"command"`
	Passed  bool   `json:"passed"`
	Stale   bool   `json:"stale,omitempty"`
	// Inconclusive: the shell reported a later stage's status, so this run
	// proves nothing either way. Dropping it renders a hidden verdict as a pass.
	Inconclusive bool `json:"inconclusive,omitempty"`
}

type ReceiptGap struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// completionReceiptWire converts the receipt, nil in and nil out so the call
// site needs no branch.
func completionReceiptWire(r *event.CompletionReceipt) *CompletionReceipt {
	if r == nil {
		return nil
	}
	out := &CompletionReceipt{
		Verdict:       r.Verdict,
		Risks:         append([]string(nil), r.Risks...),
		Unverified:    append([]string(nil), r.Unverified...),
		SaysSomething: r.SaysSomething(),
	}
	for _, c := range r.Changes {
		out.Changes = append(out.Changes, ReceiptChange{Path: c.Path, Reviewed: c.Reviewed})
	}
	for _, v := range r.Verifications {
		out.Verifications = append(out.Verifications, ReceiptVerification{
			Command: v.Command, Passed: v.Passed, Stale: v.Stale, Inconclusive: v.Inconclusive,
		})
	}
	for _, g := range r.Gaps {
		out.Gaps = append(out.Gaps, ReceiptGap{Kind: g.Kind, Detail: g.Detail})
	}
	return out
}
