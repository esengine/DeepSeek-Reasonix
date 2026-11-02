package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/safety/evidence"
)

func blockedCtx(ledger *evidence.Ledger) context.Context {
	return evidence.WithLedger(context.Background(), ledger)
}

func TestConcludeBlockedRequiresCheckableEvidence(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: false, Command: "go test ./..."})
	tool := NewConcludeBlockedTool()
	ctx := blockedCtx(ledger)

	// A failing run is the usual proof that a task cannot be done, so it counts.
	out, err := tool.Execute(ctx, json.RawMessage(`{
		"blocker":"two tests demand different values from one pure function",
		"evidence":[{"command":"go test ./..."}]
	}`))
	if err != nil {
		t.Fatalf("failed command should count as evidence: %v", err)
	}
	if !strings.Contains(out, "blocked") {
		t.Fatalf("output = %q", out)
	}

	if _, err := tool.Execute(ctx, json.RawMessage(`{
		"blocker":"cannot be done",
		"evidence":[{"command":"go test ./somewhere-else"}]
	}`)); err == nil {
		t.Fatal("a command with no receipt must not establish a blocker")
	}
	if _, err := tool.Execute(ctx, json.RawMessage(`{"blocker":"cannot be done","evidence":[]}`)); err == nil {
		t.Fatal("a claim with no evidence must be rejected")
	}
	if _, err := tool.Execute(ctx, json.RawMessage(`{"blocker":"  ","evidence":[{"command":"go test ./..."}]}`)); err == nil {
		t.Fatal("an empty blocker must be rejected")
	}
}

func TestConcludeBlockedAcceptsReadPathsAsEvidence(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"gateway_test.go"}})
	if _, err := NewConcludeBlockedTool().Execute(blockedCtx(ledger), json.RawMessage(`{
		"blocker":"the two suites disagree",
		"evidence":[{"paths":["gateway_test.go"]}]
	}`)); err != nil {
		t.Fatalf("read paths should establish a blocker: %v", err)
	}
}

// The declaration waives one thing: that the check passed. A turn that never
// ran the check has established nothing and stays unfinished.
func TestReadinessAcceptsBlockedOnlyWithAnActualCheck(t *testing.T) {
	newAgent := func(records ...evidence.Receipt) *Agent {
		ledger := evidence.NewLedger()
		for _, r := range records {
			ledger.Record(r)
		}
		a := &Agent{task: taskRuntime{ledger: ledger}}
		return a
	}
	write := evidence.Receipt{ToolName: "edit_file", Success: true, Write: true, Mutation: true, MutationEvidence: evidence.MutationProven, Paths: []string{"quota.go"}}
	failedCheck := evidence.Receipt{ToolName: "bash", Success: false, Command: "go test ./...", Verification: evidence.VerificationFailed}
	declaration := evidence.Receipt{ToolName: "conclude_blocked", Success: true, Args: json.RawMessage(`{"blocker":"x"}`)}

	a := newAgent(write, failedCheck, declaration)
	idx, _ := a.task.ledger.LatestProvenMutationIndex()
	if !a.task.ledger.HasBlockedConclusionAfter(idx) || !a.task.ledger.HasVerificationCommandAfter(idx) {
		t.Fatal("a declared blocker backed by a check that ran must be recognized")
	}

	b := newAgent(write, declaration)
	idxB, _ := b.task.ledger.LatestProvenMutationIndex()
	if b.task.ledger.HasVerificationCommandAfter(idxB) {
		t.Fatal("a turn that never ran a check must not read as having run one")
	}
}

// A rejected call proves nothing, so it must leave the contract alone. A call
// the host accepted is a fact about the task even when it names no single
// criterion — the turn is stuck either way.
