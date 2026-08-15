package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestCoordinatorPlannerDepthDoesNotCapResearchRounds(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"a"}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-2", Name: "read_file", Arguments: `{"path":"b"}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-3", Name: "read_file", Arguments: `{"path":"c"}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "plan-1", Name: "submit_plan", Arguments: `{"objective":"apply the narrow change","steps":[{"title":"run the focused test","verified_files":["a","b","c"]}]}`}}, {Type: provider.ChunkDone}},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Done."}, {Type: provider.ChunkDone}}}
	reg := tool.NewRegistry()
	reg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "ok"})
	policy := func(context.Context, string) PlannerDecision {
		return PlannerDecision{Route: PlannerRoutePlanAndExecute, Depth: PlannerDepthLight, Reason: "adaptive_work"}
	}
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(planner, NewSession("planner-sys"), nil,
		PlannerToolRegistry(reg), Options{}, executor, 0, event.Discard, policy)

	if err := coord.Run(withNoClosedLoop(context.Background()), "make the adaptive change"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(planner.requests); got != 4 {
		t.Fatalf("planner requests = %d, want three evidence rounds plus submit_plan", got)
	}
	for i, req := range planner.requests {
		if got := lastUser(req); strings.Contains(got, "research rounds") || strings.Contains(got, "safety boundary") {
			t.Fatalf("planner request %d received a fixed-round nudge: %q", i, got)
		}
	}
	if len(exec.requests) == 0 || !strings.Contains(lastUser(exec.requests[0]), executorHandoffMarker) {
		t.Fatal("executor did not receive the submitted plan")
	}
}

func TestCoordinatorEmergencyBoundedPlannerCanSubmitPlanInFinalizationRound(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"a"}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-2", Name: "read_file", Arguments: `{"path":"b"}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "plan-1", Name: "submit_plan", Arguments: `{"objective":"fix the bounded planner","steps":[{"title":"apply the owner-level fix","verified_files":["a","b"]}]}`}}, {Type: provider.ChunkDone}},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Done."}, {Type: provider.ChunkDone}}}
	reg := tool.NewRegistry()
	reg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: strings.Repeat("research evidence\n", 10_000)})
	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	})
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	plannerSess := NewSession("planner-sys")
	coord := NewCoordinatorWithPlannerPolicy(planner, plannerSess, nil, PlannerToolRegistry(reg),
		Options{MaxSteps: 2, MaxStepsKey: "planner emergency rounds"}, executor, 0, sink,
		func(context.Context, string) PlannerDecision {
			return PlannerDecision{Route: PlannerRoutePlanAndExecute, Depth: PlannerDepthLight, Reason: "emergency_bounded_work"}
		})

	if err := coord.Run(withNoClosedLoop(context.Background()), "fix the planner bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(planner.requests); got != 3 {
		t.Fatalf("planner requests = %d, want two research rounds plus terminal submission", got)
	}
	if got := lastUser(planner.requests[2]); !strings.Contains(got, "call submit_plan now") || strings.Contains(got, "Do not call any more tools") {
		t.Fatalf("planner finalization nudge conflicts with submit_plan: %q", got)
	}
	sawTruncation := false
	for _, notice := range notices {
		sawTruncation = sawTruncation || strings.HasPrefix(notice.Text, "tool output truncated:")
		if notice.Text == plannerSafetyFallbackNotice {
			t.Fatalf("valid terminal plan incorrectly fell back: %+v", notice)
		}
	}
	if !sawTruncation {
		t.Fatal("test setup did not reproduce truncated planner research")
	}
	if len(exec.requests) == 0 || !strings.Contains(lastUser(exec.requests[0]), "**Objective** — fix the bounded planner") {
		t.Fatal("executor did not receive the structured bounded plan")
	}
	messages := plannerSess.Snapshot()
	if last := messages[len(messages)-1]; last.Role != provider.RoleAssistant || last.Content != plannerPlanSubmittedClosure {
		t.Fatalf("planner transcript did not close deterministically: %+v", last)
	}
}

func TestCoordinatorTaskBudgetLetsPlannerSubmitTerminalPlan(t *testing.T) {
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"main.go"}`}},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, TotalTokens: 10}},
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "plan-1", Name: "submit_plan", Arguments: `{"objective":"finish within the safety envelope","steps":[{"title":"apply the verified change","verified_files":["main.go"]}]}`}}, {Type: provider.ChunkDone}},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Done."}, {Type: provider.ChunkDone}}}
	reg := tool.NewRegistry()
	reg.Add(coordinatorTestTool{name: "read_file", readOnly: true, output: "package main"})
	var notices []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	})
	executor := New(exec, tool.NewRegistry(), NewSession("exec-sys"), Options{}, event.Discard)
	coord := NewCoordinatorWithPlannerPolicy(planner, NewSession("planner-sys"), nil, PlannerToolRegistry(reg),
		Options{}, executor, 0, sink, func(context.Context, string) PlannerDecision {
			return PlannerDecision{Route: PlannerRoutePlanAndExecute, Depth: PlannerDepthFull, Reason: "budgeted_work"}
		})

	ctx := withNoClosedLoop(WithTaskBudget(context.Background(), TaskBudget{Tokens: 1}))
	if err := coord.Run(ctx, "plan the budgeted change"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(planner.requests); got != 2 {
		t.Fatalf("planner requests = %d, want research plus terminal submit_plan", got)
	}
	if got := lastUser(planner.requests[1]); !strings.Contains(got, "reached its token budget") || !strings.Contains(got, "call submit_plan now") {
		t.Fatalf("task-budget finalization did not request terminal plan: %q", got)
	}
	for _, notice := range notices {
		if notice.Text == plannerSafetyFallbackNotice {
			t.Fatalf("successful terminal plan incorrectly triggered fallback: %+v", notice)
		}
	}
	if len(exec.requests) == 0 || !strings.Contains(lastUser(exec.requests[0]), "finish within the safety envelope") {
		t.Fatal("executor did not receive the task-budget terminal plan")
	}
}
