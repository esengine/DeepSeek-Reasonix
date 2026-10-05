package coordinator

import (
	"context"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"strings"
	"testing"
)

func TestCoordinatorToolPlannerRetriesReasoningOnlyStopForVisiblePlan(t *testing.T) {
	plannerScript := &scriptedProvider{name: "planner", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I need to inspect the input."},
			{Type: provider.ChunkText, Text: "I'll inspect first."},
			toolCallChunk("call-1", "echo", `{"text":"input"}`),
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "tool_calls", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkReasoning, Text: "The plan is ready."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "1. apply the verified fix"},
			{Type: provider.ChunkDone},
		},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "Done."},
		{Type: provider.ChunkDone},
	}}
	plannerTools := tool.NewRegistry()
	plannerTools.Add(echoTool{})
	executor := agent.New(exec, tool.NewRegistry(), sessionstore.NewSession("exec-sys"), agent.Options{}, event.Discard)
	coord := NewCoordinator(
		deepseekThinkingProvider{plannerScript}, sessionstore.NewSession("planner-sys"), nil,
		plannerTools, agent.Options{}, executor, 0, event.Discard, nil,
	)

	if err := coord.Run(context.Background(), "fix the bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plannerScript.call != 3 {
		t.Fatalf("planner calls = %d, want 3", plannerScript.call)
	}
	if len(exec.requests) == 0 {
		t.Fatal("executor received no handoff request")
	}
	got := lastUser(exec.requests[0])
	if !strings.Contains(got, "1. apply the verified fix") || !strings.Contains(got, sessionstore.ExecutorHandoffMarker) {
		t.Fatalf("executor input = %q, want visible plan and handoff marker", got)
	}
	if strings.Contains(got, "I'll inspect first.") {
		t.Fatalf("executor input contains stale planner preamble: %q", got)
	}
}

func TestCoordinatorRollbackAfterRewriteDropsReasoningOnlyRetryTail(t *testing.T) {
	plannerSess := sessionstore.NewSession("planner-sys")
	before := plannerSess.Snapshot()
	rewriteBefore := plannerSess.RewriteVersion()

	compactedWithEvidence := []provider.Message{
		{Role: provider.RoleSystem, Content: "planner-sys"},
		{Role: provider.RoleUser, Content: agent.SummaryTagOpen + "\ncompacted research\n" + agent.SummaryTagClose},
		{Role: provider.RoleAssistant, Content: "Visible evidence collected before the current tool round."},
		{Role: provider.RoleUser, Content: "Plan the current task."},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "read-1", Name: "read_file", Arguments: `{"path":"main.go"}`}}},
		{Role: provider.RoleTool, ToolCallID: "read-1", Name: "read_file", Content: "package main"},
	}
	plannerSess.Replace(compactedWithEvidence)
	plannerSess.IncrementRewrite()
	plannerSess.Add(provider.Message{Role: provider.RoleAssistant, ReasoningContent: "first hidden-only plan"})
	plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: "provide a visible plan"})
	plannerSess.Add(provider.Message{Role: provider.RoleAssistant, ReasoningContent: "second hidden-only plan"})
	plannerSess.Add(provider.Message{Role: provider.RoleUser, Content: "provide a visible plan"})
	plannerSess.Add(provider.Message{Role: provider.RoleAssistant, ReasoningContent: "third hidden-only plan"})

	coord := &Coordinator{plannerSess: plannerSess}
	coord.rollbackPlannerTurn(before, rewriteBefore)

	got := plannerSess.Snapshot()
	if !reflect.DeepEqual(got, compactedWithEvidence) {
		t.Fatalf("rewrite-aware rollback changed compacted or completed tool evidence:\n got=%+v\nwant=%+v", got, compactedWithEvidence)
	}
	if normalized := provider.NormalizeMessages(got); !reflect.DeepEqual(normalized, got) {
		t.Fatalf("preserved planner history is not provider-coherent:\n got=%+v\nnormalized=%+v", got, normalized)
	}
}
