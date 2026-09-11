package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
)

// Ordinary work is settled by the tool results the host already recorded, so a
// sign-off is a note. It never rejects, because a rejection here is what sent
// the model back to re-cite work it had already done.

func TestCompleteStepOrdinaryAcceptsWithoutEvidence(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Add the parser","result":"parser added","notes":"desktop check deferred"}`))
	if err != nil {
		t.Fatalf("an ordinary sign-off with no evidence must be accepted: %v", err)
	}
	if !strings.Contains(out, "signed off") {
		t.Fatalf("ack = %q, want the step recorded", out)
	}
}

func TestCompleteStepOrdinaryRecordsUnverifiedInsteadOfRejecting(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := evidence.WithLedger(context.Background(), ledger)

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"verification","summary":"claimed","command":"go test ./never-ran/..."}]}`))
	if err != nil {
		t.Fatalf("an unconfirmable citation must not block ordinary work: %v", err)
	}
	if !strings.Contains(out, "Recorded as unverified") {
		t.Fatalf("ack should state the gap once, got %q", out)
	}
}

func TestCompleteStepOrdinaryDoesNotBlockOnTodoMismatch(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write", Success: true,
		Todos: []evidence.TodoItem{{Content: "Add parser", Status: "in_progress"}},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Ship parser","result":"shipped",
		"evidence":[{"kind":"manual","summary":"checked"}]}`))
	if err != nil {
		t.Fatalf("a task-list mismatch must not block an ordinary sign-off: %v", err)
	}
	if !strings.Contains(out, "Recorded as unverified") {
		t.Fatalf("ack should state the mismatch once, got %q", out)
	}
}

func TestCompleteStepOrdinarySkipsProjectCheckBlocking(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := instruction.WithChecks(evidence.WithLedger(context.Background(), ledger), []instruction.VerifyCheck{
		{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3},
	})

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Edit code","result":"code changed",
		"evidence":[{"kind":"diff","summary":"changed code","paths":["changed.go"]}]}`))
	if err != nil {
		t.Fatalf("a missing project check must be reported, not enforced, outside the closed loop: %v", err)
	}
	if !strings.Contains(out, "Recorded as unverified") || !strings.Contains(out, "go test ./...") {
		t.Fatalf("ack should name the unrun project check, got %q", out)
	}
}

func TestCompleteStepClosedLoopStillRequiresEvidence(t *testing.T) {
	ctx := evidence.WithClosedLoopExecution(evidence.WithLedger(context.Background(), evidence.NewLedger()))
	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Ship the release","result":"released"}`)); err == nil {
		t.Fatal("the closed loop must still refuse an unevidenced sign-off")
	}
}

func TestCompleteStepArgumentShapeStaysValidated(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	cases := []string{
		`{"step":"x","result":"y","evidence":[{"kind":"vibes","summary":"trust me"}]}`,
		`{"step":"x","result":"y","evidence":[{"kind":"manual","summary":""}]}`,
		`{"step":"","result":"y"}`,
		`{"step":"x","result":""}`,
	}
	for _, body := range cases {
		if _, err := (completeStep{}).Execute(ctx, json.RawMessage(body)); err == nil {
			t.Fatalf("a malformed call is an argument error, not a gap: %s", body)
		}
	}
}
