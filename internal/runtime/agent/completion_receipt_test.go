package agent

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/safety/evidence"
)

func TestReceiptCarriesWhatProseDoesNot(t *testing.T) {
	ledger := evidence.NewLedger()
	// The run came before the edit, so it proves nothing about what is there
	// now — which is exactly the kind of thing a transcript reads past.
	for _, r := range []evidence.Receipt{
		{ToolName: "bash", Success: true, Command: "go test ./...", OutputBytes: 64},
		{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"calc.py"}},
	} {
		ledger.Record(r)
	}
	c := buildShadowContract("fix the add bug in calc.py", ledger.Receipts(), nil)
	got := completionReceipt(completion.Build(c, ledger, nil))
	if got == nil {
		t.Fatal("a turn that changed a file must produce a receipt")
	}
	if got.Verdict != "partial" {
		t.Fatalf("verdict = %q, want partial", got.Verdict)
	}
	if len(got.Changes) != 1 || got.Changes[0].Path != "calc.py" || got.Changes[0].Reviewed {
		t.Fatalf("changes = %+v, want calc.py recorded as unreviewed", got.Changes)
	}
	if len(got.Verifications) != 1 || !got.Verifications[0].Passed || !got.Verifications[0].Stale {
		t.Fatalf("verifications = %+v, want the passing but superseded command named", got.Verifications)
	}
	if !receiptHasGap(got, "unreviewed_change", "calc.py") {
		t.Fatalf("gaps = %+v, want the unreviewed path named", got.Gaps)
	}
	if !receiptHasGap(got, "stale_verification", "go test ./...") {
		t.Fatalf("gaps = %+v, want the superseded run named", got.Gaps)
	}
}

// A receipt that says nothing is noise, and noise is what makes people stop
// reading receipts.
func TestNoReceiptForATurnWithNothingToJudge(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"calc.py"}, OutputBytes: 64})
	c := buildShadowContract("what does this function do?", ledger.Receipts(), nil)
	if got := completionReceipt(completion.Build(c, ledger, nil)); got != nil {
		t.Fatalf("read-only answer produced a receipt: %+v", got)
	}
}

func TestAgentHoldsTheLastTurnsReceipt(t *testing.T) {
	var a *Agent
	if a.CompletionReceipt() != nil {
		t.Fatal("a nil agent must not produce a receipt")
	}
	a = &Agent{}
	if a.CompletionReceipt() != nil {
		t.Fatal("an agent that has not finished a turn has no receipt")
	}
}

func receiptHasGap(r *event.CompletionReceipt, kind, detail string) bool {
	for _, gap := range r.Gaps {
		if gap.Kind == kind && gap.Detail == detail {
			return true
		}
	}
	return false
}
