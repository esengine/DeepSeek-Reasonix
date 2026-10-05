package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/safety/evidence"
)

func submitCompleteSubtask(t *testing.T, led *evidence.Ledger, args string) string {
	t.Helper()
	ctx := evidence.WithLedger(context.Background(), led)
	out, err := NewCompleteSubtaskTool().Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("complete_subtask: %v", err)
	}
	return out
}

// The child may claim anything; the status the parent sees is the host's.
func TestCompleteSubtaskHostLowersUnbackedClaim(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./parser", Success: true, OutputBytes: 12})

	args := `{
		"status":"complete",
		"summary":"fixed the parser",
		"acceptance_criteria":[
			{"id":"AC1","status":"satisfied","evidence":[{"kind":"verification","summary":"unit tests","command":"go test ./parser"}]},
			{"id":"AC2","status":"satisfied","evidence":[{"kind":"verification","summary":"integration suite","command":"go test ./integration"}]}
		]}`
	if out := submitCompleteSubtask(t, led, args); !strings.Contains(out, "status=partial") {
		t.Fatalf("tool result = %q, want the host-lowered status", out)
	}

	// The agent host, not the tool, records the call; replay that receipt so the
	// ledger lookup the report renderer uses is covered too.
	led.Record(evidence.ReceiptFromToolCall("complete_subtask", json.RawMessage(args), true, evidence.ToolFacts{ReadOnly: true}))
	report, ok := led.LatestCompletionReport()
	if !ok {
		t.Fatal("a recorded complete_subtask call must be recoverable from the ledger")
	}
	adjudicated, reasons := led.AdjudicateCompletion(report)
	if adjudicated.Status != evidence.CompletionPartial {
		t.Fatalf("status = %q, want partial", adjudicated.Status)
	}
	if adjudicated.Criteria[0].Status != evidence.CriterionSatisfied {
		t.Fatal("AC1 was backed by a real command receipt and must survive")
	}
	if adjudicated.Criteria[1].Status != evidence.CriterionUnsatisfied {
		t.Fatal("AC2 cited a command that never ran and must be lowered")
	}
	if len(reasons) != 1 || !strings.HasPrefix(reasons[0], "AC2:") {
		t.Fatalf("reasons = %v, want one naming AC2", reasons)
	}
}

func TestCompleteSubtaskKeepsFullyBackedClaim(t *testing.T) {
	led := evidence.NewLedger()
	led.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./parser", Success: true, OutputBytes: 12})
	led.Record(evidence.Receipt{ToolName: "write_file", Success: true, Mutation: true, Write: true, Paths: []string{"parser.go"}})

	out := submitCompleteSubtask(t, led, `{
		"status":"complete",
		"summary":"fixed the parser",
		"acceptance_criteria":[
			{"id":"AC1","status":"satisfied","evidence":[{"kind":"verification","summary":"tests","command":"go test ./parser"}]},
			{"id":"AC2","status":"satisfied","evidence":[{"kind":"diff","summary":"the fix","paths":["parser.go"]}]}
		]}`)
	if !strings.Contains(out, "status=complete") || strings.Contains(out, "lowered") {
		t.Fatalf("tool result = %q, want an untouched complete status", out)
	}
}

// A satisfied criterion resting only on the model's word is not host-backed.
func TestCompleteSubtaskLowersManualOnlyAndEvidenceFreeClaims(t *testing.T) {
	led := evidence.NewLedger()
	report, err := evidence.ParseCompletionReport(json.RawMessage(`{
		"status":"complete","summary":"done",
		"acceptance_criteria":[
			{"id":"AC1","status":"satisfied","evidence":[{"kind":"manual","summary":"I checked it"}]},
			{"id":"AC2","status":"satisfied"}
		]}`))
	if err != nil {
		t.Fatal(err)
	}
	adjudicated, reasons := led.AdjudicateCompletion(report)
	if adjudicated.Status != evidence.CompletionPartial || len(reasons) != 2 {
		t.Fatalf("status = %q reasons = %v, want partial with both lowered", adjudicated.Status, reasons)
	}
}

func TestParseCompletionReportRejectsMalformedClaims(t *testing.T) {
	for name, args := range map[string]string{
		"bad status":           `{"status":"done","summary":"x"}`,
		"missing summary":      `{"status":"complete"}`,
		"verification no cmd":  `{"status":"complete","summary":"x","acceptance_criteria":[{"id":"AC1","status":"satisfied","evidence":[{"kind":"verification","summary":"tests"}]}]}`,
		"diff without paths":   `{"status":"complete","summary":"x","acceptance_criteria":[{"id":"AC1","status":"satisfied","evidence":[{"kind":"diff","summary":"change"}]}]}`,
		"unknown evidence":     `{"status":"complete","summary":"x","acceptance_criteria":[{"id":"AC1","status":"satisfied","evidence":[{"kind":"vibes","summary":"trust me"}]}]}`,
		"criterion without id": `{"status":"complete","summary":"x","acceptance_criteria":[{"status":"satisfied"}]}`,
	} {
		if _, err := evidence.ParseCompletionReport(json.RawMessage(args)); err == nil {
			t.Errorf("%s: accepted a malformed report", name)
		}
	}
}

// A verdict is not an error. A malformed submission fails the call; a claim the
// host cannot back is a well-formed call the host answers with needs_work, and
// keeping those apart is what lets "submit again" and "you are done" be
// different answers instead of two readings of the word accepted.
func TestCompleteSubtaskVerdictSeparatesClosureFromMoreWork(t *testing.T) {
	const criterion = `{"id":"AC1","status":"satisfied","evidence":[{"kind":"verification","summary":"tests","command":"go test ./parser"}]}`

	backed := evidence.NewLedger()
	backed.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./parser", Success: true, OutputBytes: 12})
	out := submitCompleteSubtask(t, backed, `{"status":"complete","summary":"fixed it","acceptance_criteria":[`+criterion+`]}`)
	if !strings.Contains(out, "complete_subtask closed:") {
		t.Fatalf("backed report = %q, want a closed verdict", out)
	}
	if v := backed.ClosureVerdicts(); v.Closed != 1 || v.NeedsWork != 0 {
		t.Fatalf("verdicts = %+v, want one closure", v)
	}

	// The same claim with nothing behind it: the call still succeeds.
	unbacked := evidence.NewLedger()
	out = submitCompleteSubtask(t, unbacked, `{"status":"complete","summary":"fixed it","acceptance_criteria":[`+criterion+`]}`)
	if !strings.Contains(out, "complete_subtask needs_work:") {
		t.Fatalf("unbacked report = %q, want a needs_work verdict", out)
	}
	if v := unbacked.ClosureVerdicts(); v.NeedsWork != 1 || v.Closed != 0 {
		t.Fatalf("verdicts = %+v, want one needs_work", v)
	}

	// A malformed submission is the other layer and still fails the call.
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	if _, err := NewCompleteSubtaskTool().Execute(ctx, json.RawMessage(`{"status":"nonsense"}`)); err == nil {
		t.Fatal("a malformed report must fail the call, not return a verdict")
	}
}

// A rejection the sub-agent cannot act on costs a whole round: it resubmits
// with a different evidence shape and hopes. The host knows which criterion it
// could not back and what it looked for, so the verdict says both.
func TestCompleteSubtaskRejectionNamesWhatTheHostLookedFor(t *testing.T) {
	led := evidence.NewLedger()
	ctx := evidence.WithLedger(context.Background(), led)

	args := []byte(`{"status":"complete","summary":"done",
	  "acceptance_criteria":[
	    {"id":"c1","status":"satisfied","evidence":[
	      {"kind":"files","summary":"read the files","paths":["internal/runtime/agent/agent.go","internal/runtime/agent/run_loop.go"]}]},
	    {"id":"c2","status":"satisfied","evidence":[
	      {"kind":"verification","summary":"ran the tests","command":"go test ./internal/runtime/agent/"}]},
	    {"id":"c3","status":"satisfied","evidence":[
	      {"kind":"review","summary":"first reviewer"},
	      {"kind":"review","summary":"second reviewer"}]}]}`)

	out, err := (&CompleteSubtaskTool{}).Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "needs_work") {
		t.Fatalf("verdict = %q, want needs_work for claims with no receipts", out)
	}
	if n := strings.Count(out, "no completed review"); n > 1 {
		t.Errorf("one missing receipt is reported %d times, which reads as %d problems:\n%s", n, n, out)
	}
	for _, want := range []string{
		"c1:", "no read or write recorded for", "internal/runtime/agent/agent.go",
		"c2:", "no successful run of", "go test ./internal/runtime/agent/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("verdict does not say %q; the sub-agent cannot tell what to fix:\n%s", want, out)
		}
	}
}
