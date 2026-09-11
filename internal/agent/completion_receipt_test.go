package agent

import (
	"testing"

	"reasonix/internal/completion"
	"reasonix/internal/evidence"
)

func TestReceiptCarriesWhatProseDoesNot(t *testing.T) {
	ledger := evidence.NewLedger()
	for _, r := range []evidence.Receipt{
		{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"calc.py"}},
		{ToolName: "bash", Success: true, Command: "go test ./...", OutputBytes: 64},
	} {
		ledger.Record(r)
	}
	c := buildShadowContract("fix the add bug in calc.py", ledger.Receipts(), nil)
	got := completionReceipt(completion.Build(c, ledger))
	if got == nil {
		t.Fatal("a turn that changed a file must produce a receipt")
	}
	if got.Verdict != "partial" {
		t.Fatalf("verdict = %q, want partial", got.Verdict)
	}
	if len(got.Changes) != 1 || got.Changes[0].Path != "calc.py" || got.Changes[0].Reviewed {
		t.Fatalf("changes = %+v, want calc.py recorded as unreviewed", got.Changes)
	}
	if len(got.Verifications) != 1 || !got.Verifications[0].Passed {
		t.Fatalf("verifications = %+v, want the passing command named", got.Verifications)
	}
	if len(got.Gaps) != 1 || got.Gaps[0].Kind != "unreviewed_change" || got.Gaps[0].Detail != "calc.py" {
		t.Fatalf("gaps = %+v, want the unreviewed path named", got.Gaps)
	}
}

// A receipt that says nothing is noise, and noise is what makes people stop
// reading receipts.
func TestNoReceiptForATurnWithNothingToJudge(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"calc.py"}, OutputBytes: 64})
	c := buildShadowContract("what does this function do?", ledger.Receipts(), nil)
	if got := completionReceipt(completion.Build(c, ledger)); got != nil {
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
