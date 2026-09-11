package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/provider"
)

func TestTodoInventoryListsTurnTodos(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "todo_write",
		Success:  true,
		Todos:    []evidence.TodoItem{{Content: "Phase 5：脚本编辑与执行代码"}, {Content: "Review notes"}},
	})
	got := todoInventory(ledger)
	if !strings.Contains(got, `1) "Phase 5：脚本编辑与执行代码"`) || !strings.Contains(got, `2) "Review notes"`) {
		t.Fatalf("inventory should list both todos, got %s", got)
	}
	if got := todoInventory(evidence.NewLedger()); !strings.Contains(got, "no todos") {
		t.Fatalf("empty ledger inventory = %s", got)
	}
}

func TestCompleteStepRejectsMissingEvidence(t *testing.T) {
	_, err := completeStep{}.Execute(evidence.WithClosedLoopExecution(context.Background()),
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
	ctx = evidence.WithClosedLoopExecution(ctx)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "failed verification command",
			body: `{"step":"x","result":"y","evidence":[{"kind":"verification","summary":"claimed tests","command":"go test ./..."}]}`,
			want: "exited non-zero",
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

func TestCompleteStepExplainsRenewalAgainstCompletedTodoList(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("todo_write", json.RawMessage(`{"todos":[{"content":"Implement","status":"completed"},{"content":"Final review","status":"completed"}]}`), true, true))
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Review and verify again",
		"result":"ready",
		"evidence":[{"kind":"manual","summary":"reviewed"}]
	}`))
	if err == nil {
		t.Fatal("invented renewal step should not bypass the canonical todo list")
	}
	for _, want := range []string{"renewal sign-off", "step_index 2", "Final review", "do not invent a new step"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("renewal error %q missing %q", err, want)
		}
	}
}

// A command the host does not recognize as a verifier is reported, not
// rejected: most projects verify through a Makefile, a wrapper, or a private
// script, and rejecting those only made the model rewrite the same command.
// The delivery gate still refuses to sign the work off without a recognized
// verification — the report is what reaches the user.
func TestCompleteStepReportsOpaqueEvalVerificationWithoutRejecting(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"app.js"}`), true, false))
	ledger.Record(evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"node -e 'console.log(1)'"}`), true, false))
	ctx := evidence.WithClosedLoopExecution(evidence.WithLedger(context.Background(), ledger))

	out, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"Check JavaScript",
		"result":"syntax valid",
		"evidence":[{"kind":"verification","summary":"syntax valid","command":"node -e 'console.log(1)'"}]
	}`))
	if err != nil {
		t.Fatalf("a successful but unclassified command must not be rejected: %v", err)
	}
	if !strings.Contains(out, "not a recognized verifier") {
		t.Fatalf("ack should report the unclassified check exactly once, got %q", out)
	}
}

func TestCompleteStepDeliveryAcceptsNodeSyntaxCheck(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"app.js"}`), true, false))
	ledger.Record(evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"node --check app.js"}`), true, false))
	ctx := evidence.WithClosedLoopExecution(evidence.WithLedger(context.Background(), ledger))

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Check JavaScript",
		"result":"syntax valid",
		"evidence":[{"kind":"verification","summary":"syntax valid","command":"node --check app.js"}]
	}`)); err != nil {
		t.Fatalf("delivery complete_step rejected node --check: %v", err)
	}
}

func TestCompleteStepDeliveryKeepsReadOnlyEvidenceCompatibility(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"grep -n TODO app.js"}`), true, false))
	ctx := evidence.WithClosedLoopExecution(evidence.WithLedger(context.Background(), ledger))

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Inspect JavaScript",
		"result":"TODOs inspected",
		"evidence":[{"kind":"verification","summary":"inspection completed","command":"grep -n TODO app.js"}]
	}`)); err != nil {
		t.Fatalf("read-only delivery evidence regressed: %v", err)
	}
}

func TestCompleteStepRejectsMissingProjectCheckAfterWrite(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := instruction.WithChecks(evidence.WithLedger(context.Background(), ledger), []instruction.VerifyCheck{
		{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3},
	})
	ctx = evidence.WithClosedLoopExecution(ctx)

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
	ctx = evidence.WithClosedLoopExecution(ctx)

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

func TestCompleteStepReadOnlyForPermissionLayer(t *testing.T) {
	if !(completeStep{}).ReadOnly() {
		t.Fatal("complete_step stays ReadOnly so permission policy need not prompt; plan mode blocks it as an execution-only workflow")
	}
}

// Replays of real complete_step rejections captured from local sessions (2026-06-02) and issue #2917.
func TestCompleteStepMatchesParaphrasedCommands(t *testing.T) {
	cases := []struct {
		name  string
		ran   string
		cited string
	}{
		{
			name:  "cd prefix dropped",
			ran:   "cd /repo && git merge upstream/main-v2 --ff-only",
			cited: "git merge upstream/main-v2 --ff-only",
		},
		{
			name:  "flag drift inside compound",
			ran:   "rm -v scripts/test_lines.txt && ls -la scripts/test_lines.txt 2>&1 || true",
			cited: "rm -v scripts/test_lines.txt && ls scripts/test_lines.txt 2>&1",
		},
		{
			name:  "quote style drift",
			ran:   `test -f test-tools.md && echo "still exists" || echo "deleted"`,
			cited: `test -f test-tools.md && echo 'still exists' || echo 'deleted'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := evidence.NewLedger()
			ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: tc.ran})
			ctx := evidence.WithLedger(context.Background(), ledger)

			body, _ := json.Marshal(map[string]any{
				"step": "x", "result": "y",
				"evidence": []map[string]any{{"kind": "verification", "summary": "verified", "command": tc.cited}},
			})
			if _, err := (completeStep{}).Execute(ctx, body); err != nil {
				t.Fatalf("paraphrased citation of a ran command rejected: %v", err)
			}
		})
	}
}

func TestCompleteStepAcceptsSuccessfulReviewEvidence(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("review", json.RawMessage(`{"task":"review changes"}`), true, true))
	ctx := evidence.WithLedger(context.Background(), ledger)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Review code","result":"review completed",
		"evidence":[{"kind":"review","summary":"the built-in review completed"}]}`)); err != nil {
		t.Fatalf("successful review evidence rejected: %v", err)
	}
}

func TestCompleteStepRejectsFailedReviewEvidence(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("review", json.RawMessage(`{"task":"review changes"}`), false, true))
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Review code","result":"review completed",
		"evidence":[{"kind":"review","summary":"the built-in review completed"}]}`)); err == nil {
		t.Fatal("failed review task must not satisfy review evidence")
	}
}

func TestCompleteStepRejectsReviewEvidenceBeforeLatestMutation(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("review", json.RawMessage(`{"task":"review changes"}`), true, true))
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"changed.go"}`), true, false))
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Review code","result":"review completed",
		"evidence":[{"kind":"review","summary":"the built-in review completed"}]}`))
	if err == nil || !strings.Contains(err.Error(), "review must be newer and cover the changed result") {
		t.Fatalf("stale review evidence should be rejected with recovery guidance, got %v", err)
	}
}

func TestCompleteStepAcceptsStructuredReviewWithBlockingFindings(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"changed.go"}`), true, false))
	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"block",
		"reviewed_paths":["changed.go"],
		"findings":[{"severity":"critical","summary":"must fix","path":"changed.go"}]
	}`)})
	ctx := evidence.WithLedger(context.Background(), ledger)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"Review code","result":"review found blocking follow-up",
		"evidence":[{"kind":"review","summary":"structured review completed with a blocking finding"}]}`)); err != nil {
		t.Fatalf("a blocking verdict should complete the review activity while remaining enforceable by delivery gates: %v", err)
	}
}

func TestCompleteStepExplainsFailedCommandReceipt(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: false, Command: "ls scripts/test_lines.txt 2>&1"})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"verification","summary":"ls confirms the file is gone","command":"ls scripts/test_lines.txt 2>&1"}]}`))
	if err == nil {
		t.Fatal("failed command citation should be rejected")
	}
	for _, want := range []string{"exited non-zero", "|| true"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should carry the recovery hint %q, got %v", want, err)
		}
	}
}

func TestCompleteStepRejectionListsRanCommands(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "bash", Success: true, Command: "wc -l scripts/test_lines.txt"})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"verification","summary":"claimed","command":"go test ./internal/nilutil/... ./internal/fileutil/..."}]}`))
	if err == nil || !strings.Contains(err.Error(), "wc -l scripts/test_lines.txt") {
		t.Fatalf("rejection should list the commands that actually ran, got %v", err)
	}
}

func TestCompleteStepRejectionListsTouchedPaths(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{ToolName: "write_file", Success: true, Paths: []string{"changed.go"}, Write: true})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	_, err := completeStep{}.Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"diff","summary":"claimed","paths":["other.go"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "changed.go") {
		t.Fatalf("rejection should list the files actually written, got %v", err)
	}
}

func TestCompleteStepSessionFallbackUsesNormalizedMatching(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "bash",
			Arguments: `{"command":"go test ./internal/tool/... -count=1 -timeout 60s 2>&1 | tail -10"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "ok\nPASS"},
	}
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	ctx = evidence.WithSessionMessages(ctx, func() []provider.Message { return msgs })

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"verification","summary":"tool tests pass","command":"go test ./internal/tool/... -count=1 -timeout 60s"}]}`)); err != nil {
		t.Fatalf("cross-turn citation of a ran command rejected: %v", err)
	}
}

func TestCompleteStepSessionFallbackSkipsFailedCalls(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "c1", Name: "bash", Arguments: `{"command":"go test ./broken/..."}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "error: command exited: exit status 1\nFAIL"},
	}
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	ctx = evidence.WithSessionMessages(ctx, func() []provider.Message { return msgs })
	ctx = evidence.WithClosedLoopExecution(ctx)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./broken/..."}]}`)); err == nil {
		t.Fatal("a call whose recorded result is an error must not count as verification")
	}
}

// Replay from the 2026-06-11 e2e run: a file created via bash redirection has
// no reader/writer receipt, but the command text names the path.
func TestCompleteStepFilesEvidenceAcceptsBashCreatedFile(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		Command:  `mkdir -p scripts && seq -w 1 20 | while read i; do echo "line $i"; done > scripts/test_lines.txt && cat scripts/test_lines.txt`,
	})
	ctx := evidence.WithLedger(context.Background(), ledger)
	ctx = evidence.WithClosedLoopExecution(ctx)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"files","paths":["scripts/test_lines.txt"],"summary":"file created with 20 lines"}]}`)); err != nil {
		t.Fatalf("bash-created file should count as a files receipt: %v", err)
	}

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"files","paths":["scripts/never_touched.txt"],"summary":"claimed"}]}`)); err == nil {
		t.Fatal("a path no command mentions must still be rejected")
	}
}

func TestCompleteStepSessionFallbackResolvesDiffPaths(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "w1", Name: "write_file", Arguments: `{"path":"internal/foo/bar.go"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "w1", Name: "write_file", Content: "wrote 10 lines"},
	}
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	ctx = evidence.WithSessionMessages(ctx, func() []provider.Message { return msgs })

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"diff","summary":"added bar","paths":["internal/foo/bar.go"]}]}`)); err != nil {
		t.Fatalf("cross-turn diff citation of a written file rejected: %v", err)
	}
}

func TestCompleteStepSessionFallbackSkipsFailedWrite(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "w1", Name: "write_file", Arguments: `{"path":"internal/foo/bar.go"}`,
		}}},
		{Role: provider.RoleTool, ToolCallID: "w1", Name: "write_file", Content: "error: permission denied"},
	}
	ctx := evidence.WithLedger(context.Background(), evidence.NewLedger())
	ctx = evidence.WithSessionMessages(ctx, func() []provider.Message { return msgs })
	ctx = evidence.WithClosedLoopExecution(ctx)

	if _, err := (completeStep{}).Execute(ctx, json.RawMessage(`{
		"step":"x","result":"y",
		"evidence":[{"kind":"diff","summary":"added bar","paths":["internal/foo/bar.go"]}]}`)); err == nil {
		t.Fatal("a failed write must not satisfy cross-turn diff evidence")
	}
}
