package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
)

func TestCompleteStepRejectsMissingEvidence(t *testing.T) {
	_, err := completeStep{}.Execute(context.Background(),
		json.RawMessage(`{"step":"Add the parser","result":"parser added","evidence":[]}`))
	if err == nil {
		t.Fatal("completion with empty evidence should be rejected")
	}
	if !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("error should mention evidence, got %v", err)
	}
}

func TestCompleteStepRequiresStepAndResult(t *testing.T) {
	cases := []string{
		`{"step":"","result":"x","evidence":[{"kind":"manual","summary":"checked"}]}`,
		`{"step":"x","result":"","evidence":[{"kind":"manual","summary":"checked"}]}`,
	}
	for _, c := range cases {
		if _, err := (completeStep{}).Execute(context.Background(), json.RawMessage(c)); err == nil {
			t.Fatalf("expected rejection for %s", c)
		}
	}
}

func TestCompleteStepRejectsBadEvidenceKind(t *testing.T) {
	_, err := completeStep{}.Execute(context.Background(),
		json.RawMessage(`{"step":"x","result":"y","evidence":[{"kind":"vibes","summary":"trust me"}]}`))
	if err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("bad evidence kind should be rejected, got %v", err)
	}
}

func TestCompleteStepRejectsEmptyEvidenceSummary(t *testing.T) {
	_, err := completeStep{}.Execute(context.Background(),
		json.RawMessage(`{"step":"x","result":"y","evidence":[{"kind":"verification","summary":""}]}`))
	if err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatalf("empty evidence summary should be rejected, got %v", err)
	}
}

func TestCompleteStepAccepts(t *testing.T) {
	out, err := completeStep{}.Execute(context.Background(), json.RawMessage(`{
		"step":"Add the parser",
		"result":"parser added and wired into the loop",
		"evidence":[
			{"kind":"verification","summary":"all tests pass","command":"go test ./..."},
			{"kind":"diff","summary":"new parser.go + call site","paths":["parser.go","loop.go"]}
		]}`))
	if err != nil {
		t.Fatalf("valid completion rejected: %v", err)
	}
	for _, want := range []string{"Add the parser", "2 evidence", "verification", "diff"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ack %q missing %q", out, want)
		}
	}
}

func TestCompleteStepVerifiesHostReceipts(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		Command:  "go test ./internal/...",
	})
	ledger.Record(evidence.Receipt{
		ToolName: "write_file",
		Success:  true,
		Paths:    []string{"internal/evidence/evidence.go"},
		Write:    true,
	})
	ledger.Record(evidence.Receipt{
		ToolName: "read_file",
		Success:  true,
		Paths:    []string{"internal/tool/builtin/completestep.go"},
		Read:     true,
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Verify receipts",
		"result":"complete_step checks host receipts",
		"evidence":[
			{"kind":"verification","summary":"tests passed","command":"go test ./internal/..."},
			{"kind":"diff","summary":"ledger package added","paths":["internal/evidence/evidence.go"]},
			{"kind":"files","summary":"complete_step implementation inspected","paths":["internal/tool/builtin/completestep.go"]}
		]}`))
	if err != nil {
		t.Fatalf("host-verified evidence rejected: %v", err)
	}
	if !strings.Contains(out, "host-verified 3") {
		t.Fatalf("ack should report host verification, got %q", out)
	}
}

func TestCompleteStepRejectsUnverifiedHostEvidence(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: false, Command: "go test ./..."})
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := evidence.WithLedger(context.Background(), ledger)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "failed verification command",
			body: `{"step":"x","result":"y","evidence":[{"kind":"verification","summary":"claimed tests","command":"go test ./..."}]}`,
			want: "successful bash receipt",
		},
		{
			name: "missing diff writer",
			body: `{"step":"x","result":"y","evidence":[{"kind":"diff","summary":"claimed diff","paths":["other.go"]}]}`,
			want: "successful writer receipt",
		},
		{
			name: "missing file receipt",
			body: `{"step":"x","result":"y","evidence":[{"kind":"files","summary":"claimed file","paths":["other.go"]}]}`,
			want: "successful read/write receipt",
		},
		{
			name: "diff without path",
			body: `{"step":"x","result":"y","evidence":[{"kind":"diff","summary":"claimed diff"}]}`,
			want: "paths",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := completeStep{}.Execute(ctx, json.RawMessage(tc.body))
			if err == nil {
				t.Fatal("unverified host evidence should be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing %q", err, tc.want)
			}
		})
	}
}

func TestCompleteStepAllowsManualAsUnverified(t *testing.T) {
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Manual check",
		"result":"operator confirmed behavior",
		"evidence":[{"kind":"manual","summary":"checked the visible output"}]}`))
	if err != nil {
		t.Fatalf("manual evidence should remain allowed: %v", err)
	}
	if !strings.Contains(out, "manual/unverified 1") {
		t.Fatalf("manual evidence should be marked unverified, got %q", out)
	}
}

func TestCompleteStepReadOnly(t *testing.T) {
	if !(completeStep{}).ReadOnly() {
		t.Fatal("complete_step must be ReadOnly so it stays available and needs no approval")
	}
}
