package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func TestChildJoinPreservesLateMutationOrderAndLowersCompletion(t *testing.T) {
	parent := evidence.NewLedger()
	m := jobs.NewManager(event.Discard)
	t.Cleanup(m.Close)
	ctx := jobs.WithExecutionOwner(evidence.WithLedger(t.Context(), parent), m)
	sub := New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{}, event.Discard)
	sub.task.ledger = evidence.NewLedger()
	sub.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./...", Success: true, Verification: evidence.VerificationPassed})
	args := json.RawMessage(`{"status":"complete","summary":"done","acceptance_criteria":[{"id":"tests","status":"satisfied","evidence":[{"kind":"verification","command":"go test ./..."}]}]}`)
	sub.task.ledger.Record(evidence.ReceiptFromToolCall("complete_subtask", args, true, evidence.ToolFacts{ReadOnly: true}))
	entered, release := make(chan struct{}), make(chan struct{})
	_, err := m.StartOwnedForSession(jobs.SessionFromContext(ctx), "task", "late writer", func(jobCtx context.Context, _ io.Writer) (string, error) {
		close(entered)
		<-release
		jobs.PublishEvidence(jobCtx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{ToolName: "write_file", Mutation: true, Write: true, Success: true, Paths: []string{"late.go"}}}})
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	closed := make(chan struct{})
	go func() { settleChildJobs(ctx, sub, m, &err); close(closed) }()
	close(release)
	<-closed
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Summary().Receipts) != 0 {
		t.Fatal("job collection bypassed actual agent evidence owner")
	}
	mergeChildEvidence(ctx, sub)
	mutation, ok := parent.LatestSuccessfulMutationIndex()
	if !ok || parent.HasSuccessfulVerificationCommandAfter(mutation) {
		t.Fatal("old check moved after the late mutation")
	}
	answer := composeSubagentAnswer(ctx, "done", sub, writeclaim.WritePathSet{}, "fixture")
	if !strings.Contains(answer, "status: partial") || !strings.Contains(answer, "late.go") {
		t.Fatalf("stale handoff: %s", answer)
	}
}
