package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRunSubAgentRetriesReasoningOnlyStopForVisibleFinal(t *testing.T) {
	prov := &scriptedProvider{name: "sub", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "The analysis is complete."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "visible result"},
			{Type: provider.ChunkDone},
		},
	}}
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "previous task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "previous result"})

	answer, err := RunSubAgentWithSession(
		testTaskContext(), deepseekThinkingProvider{prov}, tool.NewRegistry(), sess,
		"analyze the code", Options{SubagentDepth: 1}, event.Discard,
	)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if answer != "visible result" {
		t.Fatalf("answer = %q, want visible result", answer)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2", prov.call)
	}
	if got := lastUser(prov.requests[1]); !strings.Contains(got, "visible answer") {
		t.Fatalf("retry prompt = %q, want visible-answer nudge", got)
	}
}

func TestRunSubAgentDoesNotReturnStalePreToolTextAfterReasoningOnlyStop(t *testing.T) {
	prov := &scriptedProvider{name: "sub", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I need to inspect the input."},
			{Type: provider.ChunkText, Text: "I'll inspect first."},
			toolCallChunk("call-1", "echo", `{"text":"input"}`),
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "tool_calls", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkReasoning, Text: "The inspection is complete."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "final findings"},
			{Type: provider.ChunkDone},
		},
	}}
	reg := tool.NewRegistry()
	reg.Add(echoTool{})

	answer, err := RunSubAgentWithSession(
		testTaskContext(), deepseekThinkingProvider{prov}, reg, NewSession("sys"),
		"inspect the input", Options{SubagentDepth: 1}, event.Discard,
	)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if answer != "final findings" {
		t.Fatalf("answer = %q, want final findings (not stale tool preamble)", answer)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3", prov.call)
	}
	if got := lastUser(prov.requests[2]); !strings.Contains(got, "visible answer") {
		t.Fatalf("retry prompt = %q, want visible-answer nudge", got)
	}
}

func TestRunSubAgentStopsAfterRepeatedReasoningOnlyStops(t *testing.T) {
	prov := &scriptedProvider{name: "sub", turns: [][]provider.Chunk{
		{{Type: provider.ChunkReasoning, Text: "thinking 1"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 2"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "thinking 3"}, {Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}}, {Type: provider.ChunkDone}},
	}}

	_, err := RunSubAgentWithSession(
		testTaskContext(), deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"),
		"analyze the code", Options{SubagentDepth: 1}, event.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "visible final answer") {
		t.Fatalf("error = %v, want bounded visible-final failure", err)
	}
	if prov.call != maxEmptyFinalBlocks {
		t.Fatalf("provider calls = %d, want %d", prov.call, maxEmptyFinalBlocks)
	}
}

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
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinator(
		deepseekThinkingProvider{plannerScript}, NewSession("planner-sys"), nil,
		plannerTools, Options{}, executor, 0, event.Discard, nil,
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
	if !strings.Contains(got, "1. apply the verified fix") || !strings.Contains(got, executorHandoffMarker) {
		t.Fatalf("executor input = %q, want visible plan and handoff marker", got)
	}
	if strings.Contains(got, "I'll inspect first.") {
		t.Fatalf("executor input contains stale planner preamble: %q", got)
	}
}

func TestCoordinatorRollbackAfterRewriteDropsReasoningOnlyRetryTail(t *testing.T) {
	plannerSess := NewSession("planner-sys")
	before := plannerSess.Snapshot()
	rewriteBefore := plannerSess.RewriteVersion()

	compactedWithEvidence := []provider.Message{
		{Role: provider.RoleSystem, Content: "planner-sys"},
		{Role: provider.RoleUser, Content: summaryTagOpen + "\ncompacted research\n" + summaryTagClose},
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
