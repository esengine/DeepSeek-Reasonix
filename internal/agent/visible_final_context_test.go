package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func reasoningOnlyStop(text string) []provider.Chunk {
	return reasoningOnlyStopWithTokens(text, 0, 0)
}

func reasoningOnlyStopWithTokens(text string, prompt, completion int) []provider.Chunk {
	return []provider.Chunk{
		{Type: provider.ChunkReasoning, Text: text},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			FinishReason: "stop", PromptTokens: prompt, CompletionTokens: completion,
		}},
		{Type: provider.ChunkDone},
	}
}

func TestRunScopedVisibleFinalDoesNotBypassExplicitMaxSteps(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStop("The requested plan is complete."),
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{MaxSteps: 1}, event.Discard)

	err := a.Run(WithRequireVisibleFinal(context.Background()), "write a plan")
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "max_steps" || info.HostOwned || info.Limit != 1 {
		t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want the explicit one-round limit", prov.call)
	}
	if sessionHasUserMessageContaining(a.session, "finalization-only repair") {
		t.Fatal("visible-final repair bypassed explicit max_steps")
	}
	if !IsSyntheticUserText(visibleFinalRepairMessage()) {
		t.Fatal("visible-final repair prompt must never count as a user-authored turn")
	}
}

func TestRunScopedVisibleFinalPreservesSpendPrecedenceWhenBothLimitsCross(t *testing.T) {
	prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
		reasoningOnlyStopWithTokens("The result stayed internal.", 6, 4),
		{{Type: provider.ChunkText, Text: "must not be sampled"}, {Type: provider.ChunkDone}},
	}}
	a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{MaxSteps: 1}, event.Discard)
	ctx := WithTaskBudget(WithRequireVisibleFinal(context.Background()), TaskBudget{Tokens: 10})

	err := a.Run(ctx, "finish within both limits")
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "task_budget" || info.Key != "token" {
		t.Fatalf("pause = %+v ok=%v err=%v, want task-spend precedence", info, ok, err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want no finalization sample beyond max_steps", prov.call)
	}
	if sessionHasUserMessageContaining(a.session, "reached its token budget") ||
		sessionHasUserMessageContaining(a.session, "finalization-only repair") {
		t.Fatal("simultaneous boundary appended a prompt with no permitted next sample")
	}
}

func TestRunScopedVisibleFinalSharesTaskBudgetFinalization(t *testing.T) {
	t.Run("budget crossing uses the repair as its one finalization round", func(t *testing.T) {
		prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
			reasoningOnlyStopWithTokens("The work is complete internally.", 6, 4),
			{{Type: provider.ChunkText, Text: "Visible budget summary."}, {Type: provider.ChunkDone}},
		}}
		a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
		ctx := WithTaskBudget(WithRequireVisibleFinal(context.Background()), TaskBudget{Tokens: 10})

		err := a.Run(ctx, "finish within budget")
		info, ok := InspectRunPause(err)
		if !ok || info.Kind != "task_budget" || info.Key != "token" {
			t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
		}
		if prov.call != 2 {
			t.Fatalf("provider calls = %d, want original plus one shared finalization/repair", prov.call)
		}
		if got := lastAssistantContent(a.session); got != "Visible budget summary." {
			t.Fatalf("last assistant content = %q", got)
		}
	})

	t.Run("repair that crosses budget cannot stack another allowance", func(t *testing.T) {
		prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
			reasoningOnlyStopWithTokens("first internal completion", 2, 2),
			reasoningOnlyStopWithTokens("repair stayed internal", 3, 3),
			{{Type: provider.ChunkText, Text: "must not be sampled"}, {Type: provider.ChunkDone}},
		}}
		a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{
			TaskBudget: TaskBudget{Tokens: 10},
		}, event.Discard)

		err := a.Run(WithRequireVisibleFinal(context.Background()), "finish within budget")
		info, ok := InspectRunPause(err)
		if !ok || info.Kind != "task_budget" || info.Key != "token" {
			t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
		}
		if prov.call != 2 {
			t.Fatalf("provider calls = %d, want the crossing repair to consume finalization", prov.call)
		}
		if !sessionHasUserMessageContaining(a.session, "finalization-only repair") {
			t.Fatal("first in-budget response did not request the scoped repair")
		}
		if sessionHasUserMessageContaining(a.session, "reached its token budget") {
			t.Fatal("crossing repair appended a second finalization prompt")
		}
	})
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

func TestRunScopedVisibleFinalPreservesHostFinalizationBoundaries(t *testing.T) {
	t.Run("explicit max_steps finalization pauses before a rendering repair", func(t *testing.T) {
		prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
			{{Type: provider.ChunkReasoning, Text: "performing the bounded action"}, toolCallChunk("work-1", "dangerous_test_tool", `{}`), {Type: provider.ChunkDone}},
			reasoningOnlyStop("The bounded work is summarized internally."),
		}}
		reg := tool.NewRegistry()
		reg.Add(&visibleFinalSideEffectTool{})
		a := New(deepseekThinkingProvider{prov}, reg, NewSession("sys"), Options{MaxSteps: 1}, event.Discard)

		err := a.Run(WithRequireVisibleFinal(context.Background()), "perform bounded work")
		info, ok := InspectRunPause(err)
		if !ok || info.Kind != "max_steps" || info.HostOwned || info.Limit != 1 {
			t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
		}
		if prov.call != 2 {
			t.Fatalf("provider calls = %d, want work plus the host-owned summary", prov.call)
		}
		if sessionHasUserMessageContaining(a.session, "finalization-only repair") {
			t.Fatal("visible-final repair overrode the host-owned run boundary")
		}
	})

	t.Run("armed recovery pause wins over a rendering repair", func(t *testing.T) {
		prov := &scriptedProvider{name: "deepseek", turns: [][]provider.Chunk{
			reasoningOnlyStop("The repeated host failure is summarized internally."),
		}}
		a := New(deepseekThinkingProvider{prov}, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
		a.forkRestore = func(state *runLoopState) { state.recoveryGraceRound = true }

		err := a.Run(WithRequireVisibleFinal(context.Background()), "perform goal work")
		var pause *RecoveryPauseError
		if !errors.As(err, &pause) {
			t.Fatalf("Run error = %v, want armed recovery pause", err)
		}
		if prov.call != 1 {
			t.Fatalf("provider calls = %d, want the already-armed recovery sample only", prov.call)
		}
		if sessionHasUserMessageContaining(a.session, "finalization-only repair") {
			t.Fatal("visible-final repair overrode the armed recovery boundary")
		}
	})
}

type visibleFinalSideEffectTool struct {
	executions int
	previews   int
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
func (t *visibleFinalSideEffectTool) Preview(context.Context, json.RawMessage) (diff.Change, error) {
	t.previews++
	return diff.Change{Diff: "@@\n+must-not-preview\n", Added: 1}, nil
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
	if called.previews != 0 {
		t.Fatalf("repair tool previews = %d, want zero registry/preview access", called.previews)
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
	if called.previews != 0 {
		t.Fatalf("repair tool previews = %d, want zero registry/preview access", called.previews)
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
