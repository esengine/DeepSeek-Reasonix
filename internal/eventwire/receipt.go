package eventwire

import (
	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
)

// CompletionReceipt is the JSON form of event.CompletionReceipt: what the host
// verified about a turn's work, and what it could not.
type CompletionReceipt struct {
	Diff          *checkpoint.TurnChanges `json:"diff,omitempty"`
	Interrupted   bool                    `json:"interrupted,omitempty"`
	Verdict       string                  `json:"verdict"`
	Changes       []ReceiptChange         `json:"changes,omitempty"`
	Verifications []ReceiptVerification   `json:"verifications,omitempty"`
	Gaps          []ReceiptGap            `json:"gaps,omitempty"`
	Risks         []string                `json:"risks,omitempty"`
}

type ReceiptChange struct {
	Path     string `json:"path"`
	Reviewed bool   `json:"reviewed"`
}

type ReceiptVerification struct {
	Command      string `json:"command"`
	Passed       bool   `json:"passed"`
	Stale        bool   `json:"stale,omitempty"`
	ToolCallID   string `json:"toolCallId,omitempty"`
	ToolResultID string `json:"toolResultId,omitempty"`
	Interrupted  bool   `json:"interrupted,omitempty"`
	ExitCode     *int   `json:"exitCode,omitempty"`
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
	out := &CompletionReceipt{Verdict: r.Verdict, Risks: append([]string(nil), r.Risks...), Diff: r.Diff, Interrupted: r.Interrupted}
	for _, c := range r.Changes {
		out.Changes = append(out.Changes, ReceiptChange{Path: c.Path, Reviewed: c.Reviewed})
	}
	for _, v := range r.Verifications {
		out.Verifications = append(out.Verifications, ReceiptVerification{Command: v.Command, Passed: v.Passed, Stale: v.Stale, ToolCallID: v.ToolCallID, ToolResultID: v.ToolResultID, ExitCode: v.ExitCode, Interrupted: v.Interrupted})
	}
	for _, g := range r.Gaps {
		out.Gaps = append(out.Gaps, ReceiptGap{Kind: g.Kind, Detail: g.Detail})
	}
	return out
}

// CompletionReceiptEvent restores the host record when replaying durable events.
func CompletionReceiptEvent(r *CompletionReceipt) *event.CompletionReceipt {
	if r == nil {
		return nil
	}
	out := &event.CompletionReceipt{Verdict: r.Verdict, Diff: r.Diff, Interrupted: r.Interrupted, Risks: append([]string(nil), r.Risks...)}
	for _, c := range r.Changes {
		out.Changes = append(out.Changes, event.ReceiptChange{Path: c.Path, Reviewed: c.Reviewed})
	}
	for _, v := range r.Verifications {
		out.Verifications = append(out.Verifications, event.ReceiptVerification{Command: v.Command, Passed: v.Passed, Stale: v.Stale, ToolCallID: v.ToolCallID, ToolResultID: v.ToolResultID, ExitCode: v.ExitCode, Interrupted: v.Interrupted})
	}
	for _, g := range r.Gaps {
		out.Gaps = append(out.Gaps, event.ReceiptGap{Kind: g.Kind, Detail: g.Detail})
	}
	return out
}
