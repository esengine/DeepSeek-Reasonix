package agent

import (
	"context"
	"reflect"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestDefaultRunStepLimitYieldsAfterSummary(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"a"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r2", Name: "read_file", Arguments: `{"path":"b"}`}}},
		testutil.Turn{Text: "Saved progress; more work remains."},
	)
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	err := a.Run(WithDefaultRunStepLimit(context.Background(), 2, "goal model rounds"), "work")
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "max_steps" || !info.HostOwned || info.Limit != 2 || info.Key != "goal model rounds" {
		t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
	}
	if prov.CallCount() != 3 {
		t.Fatalf("provider calls = %d, want two work rounds plus one summary", prov.CallCount())
	}
}

func TestExplicitMaxStepsOverridesGoalDefault(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"a"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r2", Name: "read_file", Arguments: `{"path":"b"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r3", Name: "read_file", Arguments: `{"path":"c"}`}}},
		testutil.Turn{Text: "Done."},
	)
	a := New(prov, reg, NewSession(""), Options{MaxSteps: 3}, event.Discard)
	if err := a.Run(WithDefaultRunStepLimit(context.Background(), 2, "goal model rounds"), "work"); err != nil {
		t.Fatalf("explicit MaxSteps should own the run: %v", err)
	}
	if prov.CallCount() != 4 {
		t.Fatalf("provider calls = %d, want explicit three rounds plus summary", prov.CallCount())
	}
}

func TestGoalSameFailurePausesAfterStructuralThreshold(t *testing.T) {
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
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "goal_stuck" || info.Limit != stormBreakThreshold || info.Key != "goal repeated host outcome" {
		t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
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

func TestGoalZeroEvidencePausesAfterSixRepeatedSuccesses(t *testing.T) {
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
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != "goal_stuck" || info.Limit != progressStopStreak || info.Key != "goal zero-evidence rounds" {
		t.Fatalf("pause = %+v ok=%v err=%v", info, ok, err)
	}
}

func TestGoalDefaultBudgetFlowsToChild(t *testing.T) {
	task := &TaskTool{}
	ctx := WithDefaultRunStepLimit(context.Background(), 16, "goal model rounds")
	if got := task.childMaxStepsForContext(ctx, 0); got != 8 {
		t.Fatalf("child steps = %d, want 8", got)
	}
	if got := task.childMaxStepsForContext(ctx, 3); got != 3 {
		t.Fatalf("explicit child steps = %d, want 3", got)
	}
}

func TestGoalDefaultBudgetDoesNotChangeNormalRequestPrefix(t *testing.T) {
	plainProvider := testutil.NewMock("m", testutil.Turn{Text: "done"})
	boundedProvider := testutil.NewMock("m", testutil.Turn{Text: "done"})
	plain := New(plainProvider, tool.NewRegistry(), NewSession("stable system"), Options{}, event.Discard)
	bounded := New(boundedProvider, tool.NewRegistry(), NewSession("stable system"), Options{}, event.Discard)
	if err := plain.Run(context.Background(), "same input"); err != nil {
		t.Fatal(err)
	}
	if err := bounded.Run(WithDefaultRunStepLimit(context.Background(), 16, "goal model rounds"), "same input"); err != nil {
		t.Fatal(err)
	}
	plainReqs, boundedReqs := plainProvider.Requests(), boundedProvider.Requests()
	if len(plainReqs) != 1 || len(boundedReqs) != 1 {
		t.Fatalf("request counts = %d/%d", len(plainReqs), len(boundedReqs))
	}
	if !reflect.DeepEqual(plainReqs[0], boundedReqs[0]) {
		t.Fatalf("host-only default budget changed the provider request\nplain=%+v\nbounded=%+v", plainReqs[0], boundedReqs[0])
	}
}
