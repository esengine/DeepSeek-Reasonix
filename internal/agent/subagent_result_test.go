package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestSubagentResultToolContractIsStableAndReadOnly(t *testing.T) {
	reader := NewSubagentResultTool(nil)
	if reader.Name() != "read_subagent_result" || !reader.ReadOnly() || !reader.PlanModeSafe() {
		t.Fatalf("reader contract = name %q readOnly=%v planSafe=%v", reader.Name(), reader.ReadOnly(), reader.PlanModeSafe())
	}
	if !json.Valid(reader.Schema()) {
		t.Fatalf("reader schema is invalid: %s", reader.Schema())
	}
	canonical := provider.CanonicalizeSchema(reader.Schema())
	if got := string(provider.CanonicalizeSchema(canonical)); got != string(canonical) {
		t.Fatalf("reader schema canonicalization is not stable:\n%s", canonical)
	}
}

func TestSplitSubagentRunResultDoesNotRewriteEphemeralAnswer(t *testing.T) {
	ephemeral := "Research notes\n\nFinal answer:\nkeep this heading and everything before it"
	if answer, ref := splitSubagentRunResult(ephemeral); answer != ephemeral || ref != "" {
		t.Fatalf("ephemeral answer was treated as a persisted envelope: answer=%q ref=%q", answer, ref)
	}
	persisted := "Subagent reference: sa_test\n\nFinal answer:\nanswer only"
	if answer, ref := splitSubagentRunResult(persisted); answer != "answer only" || ref != "sa_test" {
		t.Fatalf("persisted envelope was not split: answer=%q ref=%q", answer, ref)
	}
}

func TestSubagentResultToolPagesOnUTF8Boundaries(t *testing.T) {
	workspace := t.TempDir()
	store := NewSubagentStore(t.TempDir())
	run, err := store.PrepareFresh(SubagentSpec{
		Kind:          "task",
		Name:          "task",
		WorkspaceRoot: workspace,
		ParentSession: "parent-session",
		SystemPrompt:  "sys",
		Registry:      tool.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	answer := "BEGIN\n" + strings.Repeat("中", 10_000) + "\nEND"
	run.Session.Add(provider.Message{Role: provider.RoleUser, Content: "research"})
	run.Session.Add(provider.Message{Role: provider.RoleAssistant, Content: answer})
	if err := store.SaveCompleted(run); err != nil {
		run.Release()
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()

	reader := &SubagentResultTool{store: store, workspaceRoot: workspace}
	ctx := WithParentSession(context.Background(), "parent-session")
	first, err := reader.Execute(ctx, json.RawMessage(`{"ref":"`+run.Ref+`","limit_bytes":10001}`))
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if !utf8.ValidString(first) || !strings.Contains(first, "offset_bytes=9999") || !strings.Contains(first, "More remains") {
		t.Fatalf("first page did not end on the expected UTF-8 boundary:\n%s", first[max(0, len(first)-240):])
	}
	second, err := reader.Execute(ctx, json.RawMessage(`{"ref":"`+run.Ref+`","offset_bytes":9999,"limit_bytes":24576}`))
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if !utf8.ValidString(second) || !strings.Contains(second, "END") || !strings.Contains(second, "End of subagent result") {
		t.Fatalf("second page did not finish the answer: %s", second[max(0, len(second)-240):])
	}
	if _, err := reader.Execute(ctx, json.RawMessage(`{"ref":"`+run.Ref+`","offset_bytes":10000}`)); err == nil || !strings.Contains(err.Error(), "UTF-8 character boundary") {
		t.Fatalf("reader accepted a split UTF-8 offset: %v", err)
	}
}
