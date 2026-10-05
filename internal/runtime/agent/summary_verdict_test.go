package agent

import (
	"testing"

	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

func ledgerWith(receipts ...evidence.Receipt) *evidence.Ledger {
	ledger := evidence.NewLedger()
	for _, r := range receipts {
		ledger.Record(r)
	}
	return ledger
}

// The summary line and the completion receipt answer the same question, so a
// contract the ledger cannot fully vouch for must not be summarized as complete
// beside a receipt that lists what is still unproven.
func TestSummaryVerdictFollowsTheReceiptsGaps(t *testing.T) {
	ledger := ledgerWith(
		evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./...", OutputBytes: 64},
		evidence.Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"calc.py"}},
	)
	c := buildShadowContract("fix the add bug in calc.py", ledger.Receipts(), nil)
	rep := completion.Build(c, ledger, nil)

	if got := c.GoalVerdict(); got != taskcontract.VerdictComplete {
		t.Fatalf("contract verdict = %v, want the satisfied contract this case is about", got)
	}
	if len(rep.Gaps) == 0 {
		t.Fatal("expected the change made after the run to stay a gap")
	}
	if got := summaryVerdictOf(c, rep); got != taskcontract.VerdictPartial {
		t.Fatalf("summary verdict = %v, want partial beside a receipt listing gaps", got)
	}
}

// With nothing left unproven the summary keeps the contract's own answer.
func TestSummaryVerdictKeepsCompleteWhenNothingIsUnproven(t *testing.T) {
	ledger := ledgerWith(
		evidence.Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"calc.py"}},
		evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"calc.py"}, OutputBytes: 64},
		evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./...", OutputBytes: 64},
	)
	c := buildShadowContract("fix the add bug in calc.py", ledger.Receipts(), nil)
	rep := completion.Build(c, ledger, nil)

	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none once the change was read back and verified", rep.Gaps)
	}
	if got := summaryVerdictOf(c, rep); got != taskcontract.VerdictComplete {
		t.Fatalf("summary verdict = %v, want complete", got)
	}
}

// A blocked contract keeps its own verdict: gaps never soften a hard stop.
func TestSummaryVerdictKeepsBlocked(t *testing.T) {
	ledger := ledgerWith(
		evidence.Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"calc.py"}},
	)
	c := buildShadowContract("fix the add bug in calc.py", ledger.Receipts(), nil)
	rep := completion.Build(c, ledger, nil)
	if got := summaryVerdictOf(c, rep); got == taskcontract.VerdictComplete {
		t.Fatalf("summary verdict = %v, want the contract's own unfinished answer", got)
	}
}

// A fresh passing run carries the change it covers. Demanding a read-back on
// top of it would flag every correct turn, which is how a signal stops meaning
// anything.
func TestSummaryVerdictKeepsCompleteWhenAFreshRunProvedTheChange(t *testing.T) {
	ledger := ledgerWith(
		evidence.Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, Paths: []string{"calc.py"}},
		evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./...", OutputBytes: 64},
	)
	c := buildShadowContract("fix the add bug in calc.py", ledger.Receipts(), nil)
	rep := completion.Build(c, ledger, nil)

	if len(rep.Gaps) != 0 {
		t.Fatalf("gaps = %+v, want none: the run is the proof", rep.Gaps)
	}
	if got := summaryVerdictOf(c, rep); got != taskcontract.VerdictComplete {
		t.Fatalf("summary verdict = %v, want complete", got)
	}
}
