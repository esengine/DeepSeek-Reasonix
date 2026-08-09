package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func reasoningOnlyStop(text string) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: text},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}},
		{Type: provider.ChunkDone},
	}
}

func TestRunScopedVisibleFinalRepairsReasoningOnlyStopPastStepLimit(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("The requested plan is complete."),
		{{Type: provider.ChunkText, Text: "1. Apply the safe fix."}, {Type: provider.ChunkDone}},
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{MaxSteps: 1}, event.Discard)

	if err := a.Run(WithRequireVisibleFinal(context.Background()), "write a plan"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want original plus one repair", prov.call)
	}
	if got := lastAssistantContent(a.session); got != "1. Apply the safe fix." {
		t.Fatalf("last assistant content = %q", got)
	}
	if got := lastUser(prov.requests[1]); !strings.Contains(got, "Do not call tools or repeat any work") {
		t.Fatalf("repair prompt = %q, want finalization-only contract", got)
	}
	if !IsSyntheticUserText(visibleFinalRepairMessage()) {
		t.Fatal("visible-final repair prompt must never count as a user-authored turn")
	}
}

func TestRunScopedVisibleFinalRepairExhaustsAfterTwoAdditionalSamples(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("finished once"),
		reasoningOnlyStop("finished twice"),
		reasoningOnlyStop("finished three times"),
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)

	err := a.Run(WithRequireVisibleFinal(context.Background()), "answer visibly")
	if err == nil || !strings.Contains(err.Error(), "visible final answer") {
		t.Fatalf("Run error = %v, want bounded visible-final failure", err)
	}
	if prov.call != 1+maxVisibleFinalRepairRounds {
		t.Fatalf("provider calls = %d, want original + %d repairs", prov.call, maxVisibleFinalRepairRounds)
	}
}

func TestRunScopedVisibleFinalLeavesOrdinaryReasoningOnlyStopUnchanged(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("The ordinary answer lives in reasoning."),
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)

	if err := a.Run(context.Background(), "ordinary chat"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want one ordinary reasoning-only stop", prov.call)
	}
	if sessionHasUserMessageContaining(a.session, "Do not call tools or repeat any work") {
		t.Fatal("ordinary chat inherited the run-scoped visible-final repair")
	}
}

func TestRunScopedVisibleFinalRequirementDoesNotLeakToNextRun(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("first run needs repair"),
		{{Type: provider.ChunkText, Text: "first visible answer"}, {Type: provider.ChunkDone}},
		reasoningOnlyStop("second run remains reasoning-only"),
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)

	if err := a.Run(WithRequireVisibleFinal(context.Background()), "first"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := a.Run(context.Background(), "second"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want two scoped calls plus one ordinary call", prov.call)
	}
	if got := lastAssistantContent(a.session); got != "" {
		t.Fatalf("second run assistant content = %q, want accepted reasoning-only stop", got)
	}
}

type visibleFinalSideEffectTool struct {
	executions int
}

func (*visibleFinalSideEffectTool) Name() string        { return "dangerous_test_tool" }
func (*visibleFinalSideEffectTool) Description() string { return "records whether it executed" }
func (*visibleFinalSideEffectTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (*visibleFinalSideEffectTool) ReadOnly() bool { return false }
func (t *visibleFinalSideEffectTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.executions++
	return "executed", nil
}

func TestVisibleFinalRepairRejectsCoStreamedTextAndPairsToolBeforeCleanAnswer(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("work is complete"),
		{
			{Type: provider.ChunkReasoning, Text: "I should call the tool again."},
			{Type: provider.ChunkText, Text: "This text is not a clean final because I also called a tool."},
			toolCallChunk("repair-tool-1", "dangerous_test_tool", `{}`),
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "tool_calls"}},
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "visible result"}, {Type: provider.ChunkDone}},
	}}
	called := &visibleFinalSideEffectTool{}
	reg := tool.NewRegistry()
	reg.Add(called)
	a := New(deepseekThinkingProvider{prov}, reg, NewSession("sys"), Options{}, event.Discard)

	if err := a.Run(WithRequireVisibleFinal(context.Background()), "perform work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called.executions != 0 {
		t.Fatalf("repair tool executions = %d, want zero", called.executions)
	}
	if got := toolResultByID(a.session, "repair-tool-1"); !strings.Contains(got, "finalization-only") {
		t.Fatalf("paired repair tool result = %q, want host block", got)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want original plus two repairs", prov.call)
	}
}

func TestVisibleFinalRepairToolCallsCannotResetRepairCap(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("work is complete"),
		{{Type: provider.ChunkReasoning, Text: "tool retry one"}, toolCallChunk("repair-tool-1", "dangerous_test_tool", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkReasoning, Text: "tool retry two"}, toolCallChunk("repair-tool-2", "dangerous_test_tool", `{}`), {Type: provider.ChunkDone}},
	}}
	called := &visibleFinalSideEffectTool{}
	reg := tool.NewRegistry()
	reg.Add(called)
	a := New(deepseekThinkingProvider{prov}, reg, NewSession("sys"), Options{}, event.Discard)

	err := a.Run(WithRequireVisibleFinal(context.Background()), "perform work")
	if err == nil || !strings.Contains(err.Error(), "visible final answer") {
		t.Fatalf("Run error = %v, want bounded visible-final failure", err)
	}
	if called.executions != 0 {
		t.Fatalf("repair tool executions = %d, want zero", called.executions)
	}
	for _, id := range []string{"repair-tool-1", "repair-tool-2"} {
		if got := toolResultByID(a.session, id); !strings.Contains(got, "finalization-only") {
			t.Fatalf("paired result %s = %q, want host block", id, got)
		}
	}
	if prov.call != 1+maxVisibleFinalRepairRounds {
		t.Fatalf("provider calls = %d, want cap at %d", prov.call, 1+maxVisibleFinalRepairRounds)
	}
}
