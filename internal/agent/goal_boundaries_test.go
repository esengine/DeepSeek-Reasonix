package agent

import (
	"context"
	"fmt"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestGoalRunHasNoDefaultModelRoundCeiling(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	turns := make([]testutil.Turn, 0, 102)
	for i := range 101 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: fmt.Sprintf("r%d", i), Name: "read_file", Arguments: fmt.Sprintf(`{"path":"file-%d"}`, i),
		}}})
	}
	turns = append(turns, testutil.Turn{Text: "Done."})
	prov := testutil.NewMock("m", turns...)
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1", TaskText: "work"})
	if err := a.Run(ctx, "work"); err != nil {
		t.Fatalf("Goal run stopped at a default round boundary: %v", err)
	}
	if prov.CallCount() != 102 {
		t.Fatalf("provider calls = %d, want 101 tool rounds plus final", prov.CallCount())
	}
}

func TestExplicitMaxStepsStillAppliesToGoal(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"a"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r2", Name: "read_file", Arguments: `{"path":"b"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r3", Name: "read_file", Arguments: `{"path":"c"}`}}},
		testutil.Turn{Text: "Done."},
	)
	a := New(prov, reg, NewSession(""), Options{MaxSteps: 3}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1", TaskText: "work"})
	err := a.Run(ctx, "work")
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "max_steps" || info.HostOwned || info.Limit != 3 {
		t.Fatalf("explicit MaxSteps pause = %+v ok=%v err=%v", info, ok, err)
	}
	if prov.CallCount() != 4 {
		t.Fatalf("provider calls = %d, want explicit three rounds plus summary", prov.CallCount())
	}
}

func TestGoalSameFailureRedirectsWithoutPausing(t *testing.T) {
	turns := []testutil.Turn{
		{ToolCalls: []provider.ToolCall{{ID: "x1", Name: "missing_tool", Arguments: `{}`}}},
		{ToolCalls: []provider.ToolCall{{ID: "x2", Name: "missing_tool", Arguments: `{}`}}},
		{ToolCalls: []provider.ToolCall{{ID: "x3", Name: "missing_tool", Arguments: `{}`}}},
		{Text: "The same host failure repeated; a different approach is required."},
	}
	prov := testutil.NewMock("m", turns...)
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1", TaskText: "finish"})
	err := a.Run(ctx, "work")
	if err != nil {
		t.Fatalf("Goal structural guard paused the run: %v", err)
	}
	if prov.CallCount() != stormBreakThreshold+1 {
		t.Fatalf("provider calls = %d, want threshold plus one summary", prov.CallCount())
	}

	ordinary := testutil.NewMock("m", turns...)
	plain := New(ordinary, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	if err := plain.Run(context.Background(), "work"); err != nil {
		t.Fatalf("ordinary mode keeps the structural guard advisory: %v", err)
	}
}

func TestGoalZeroEvidenceRedirectsAndContinues(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	turns := make([]testutil.Turn, 0, progressStopStreak+2)
	for i := range progressStopStreak + 1 {
		turns = append(turns, testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: "same-" + string(rune('a'+i)), Name: "read_file", Arguments: `{"path":"same"}`,
		}}})
	}
	turns = append(turns, testutil.Turn{Text: "The repeated read produced no new evidence."})
	prov := testutil.NewMock("m", turns...)
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal-1", TaskText: "research"})
	err := a.Run(ctx, "work")
	if err != nil {
		t.Fatalf("Goal progress guard paused the run: %v", err)
	}
}

func TestUnboundedGoalParentLeavesChildUnbounded(t *testing.T) {
	task := &TaskTool{}
	if got := task.childMaxStepsForContext(context.Background(), 0); got != 0 {
		t.Fatalf("child steps = %d, want unlimited", got)
	}
	if got := task.childMaxStepsForContext(context.Background(), 3); got != 3 {
		t.Fatalf("explicit child steps = %d, want 3", got)
	}
	task.maxSteps = 16
	if got := task.childMaxStepsForContext(context.Background(), 0); got != 8 {
		t.Fatalf("explicit parent child steps = %d, want 8", got)
	}
}
