package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
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

func TestCompleteStepRejectsMissingProjectCheckAfterWrite(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := instruction.WithChecks(evidence.WithLedger(context.Background(), ledger), []instruction.VerifyCheck{
		{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3},
	})

	_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Edit code",
		"result":"code changed",
		"evidence":[{"kind":"diff","summary":"changed code","paths":["changed.go"]}]
	}`))
	if err == nil {
		t.Fatal("write-backed completion should require project verify checks")
	}
	for _, want := range []string{"project check", "go test ./...", "AGENTS.md:3"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestCompleteStepRejectsProjectCheckBeforeWrite(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."})
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := instruction.WithChecks(evidence.WithLedger(context.Background(), ledger), []instruction.VerifyCheck{
		{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3},
	})

	_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Edit code",
		"result":"code changed",
		"evidence":[{"kind":"diff","summary":"changed code","paths":["changed.go"]}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "after the latest matching write") {
		t.Fatalf("check before write should be rejected, got %v", err)
	}
}

func TestCompleteStepAcceptsProjectChecksAfterWrite(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "go test ./..."})
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "git diff --check"})
	ctx := instruction.WithChecks(evidence.WithLedger(context.Background(), ledger), []instruction.VerifyCheck{
		{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3},
		{Command: "git diff --check", SourcePath: "AGENTS.md", Line: 4},
	})

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Edit code",
		"result":"code changed",
		"evidence":[{"kind":"diff","summary":"changed code","paths":["changed.go"]}]
	}`))
	if err != nil {
		t.Fatalf("project checks after write should pass: %v", err)
	}
	if !strings.Contains(out, "project checks 2") {
		t.Fatalf("ack should mention project checks, got %q", out)
	}
}

func TestCompleteStepProjectChecksOnlyGateWriteBackedCompletions(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Paths: []string{"notes.md"}, Read: true})
	ctx := instruction.WithChecks(evidence.WithLedger(context.Background(), ledger), []instruction.VerifyCheck{
		{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3},
	})

	cases := []string{
		`{"step":"Manual","result":"checked","evidence":[{"kind":"manual","summary":"operator checked"}]}`,
		`{"step":"Inspect","result":"read file","evidence":[{"kind":"files","summary":"inspected file","paths":["notes.md"]}]}`,
	}
	for _, body := range cases {
		if _, err := (completeStep{}).Execute(ctx, json.RawMessage(body)); err != nil {
			t.Fatalf("non-write-backed completion should not require project checks: %v", err)
		}
	}
}

func TestCompleteStepMatchesTodoReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Add parser", Status: "in_progress", ActiveForm: "Adding parser"},
			{Content: "Wire parser", Status: "completed"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	for _, step := range []string{"Add parser", "Adding parser", "2"} {
		t.Run(step, func(t *testing.T) {
			out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
				"step":"`+step+`",
				"result":"step is complete",
				"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
			if err != nil {
				t.Fatalf("todo-backed step rejected: %v", err)
			}
			if !strings.Contains(out, "todo-matched") {
				t.Fatalf("ack should mention todo match, got %q", out)
			}
		})
	}
}

func TestCompleteStepRejectsTodoMismatchAndPending(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos: []evidence.TodoItem{
			{Content: "Add parser", Status: "in_progress"},
			{Content: "Document parser", Status: "pending"},
		},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	cases := []struct {
		name string
		step string
		want string
	}{
		{name: "missing", step: "Ship parser", want: "matching todo_write item"},
		{name: "pending", step: "Document parser", want: "pending"},
		{name: "pending number", step: "2", want: "pending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
				"step":"`+tc.step+`",
				"result":"step is complete",
				"evidence":[{"kind":"manual","summary":"checked manually"}]}`))
			if err == nil {
				t.Fatal("todo-backed mismatch should be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q missing %q", err, tc.want)
			}
		})
	}
}

func TestCompleteStepIgnoresFailedTodoReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  false,
		Todos:    []evidence.TodoItem{{Content: "Add parser", Status: "in_progress"}},
	})
	ctx := evidence.WithLedger(context.Background(), ledger)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Anything",
		"result":"step is complete",
		"evidence":[{"kind":"manual","summary":"checked manually"}]}`)); err != nil {
		t.Fatalf("failed todo_write receipt should not constrain step: %v", err)
	}
}

func TestCompleteStepReadOnly(t *testing.T) {
	if !(completeStep{}).ReadOnly() {
		t.Fatal("complete_step must be ReadOnly so it stays available and needs no approval")
	}
}
