package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
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
	led.Record(evidence.ReceiptFromToolCall("complete_subtask", json.RawMessage(args), true, true))
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

// End to end: the parent's view leads with the adjudicated status, not prose.
func TestSubAgentAnswerLeadsWithAdjudicatedStatus(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeWriteFileTool{})
	AttachCompleteSubtaskTool(reg)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "write_file", `{"path":"parser.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("2", "complete_subtask", `{"status":"complete","summary":"fixed the parser","acceptance_criteria":[{"id":"AC1","status":"satisfied","evidence":[{"kind":"diff","summary":"the fix","paths":["parser.go"]}]},{"id":"AC2","status":"satisfied","evidence":[{"kind":"verification","summary":"suite","command":"go test ./..."}]}],"unresolved":["integration suite not executed"]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "all good"}, {Type: provider.ChunkDone}},
	}}

	answer, err := RunSubAgentWithSession(withNoClosedLoop(context.Background()), prov, reg, NewSession("sys"),
		"fix the parser", Options{}, event.Discard)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if !strings.HasPrefix(answer, "status: partial") {
		t.Fatalf("answer must lead with the host-adjudicated status:\n%s", answer)
	}
	for _, want := range []string{
		"AC1 satisfied",
		"AC2 unsatisfied",
		"host lowered AC2",
		"unresolved: integration suite not executed",
		hostReceiptsHeader,
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("answer missing %q:\n%s", want, answer)
		}
	}
}
