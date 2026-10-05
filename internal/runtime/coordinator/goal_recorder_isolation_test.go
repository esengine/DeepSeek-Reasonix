package coordinator

import (
	"context"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"
)

func TestCoordinatorPlannerCannotReportExecutorGoalDisposition(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{toolCallChunk("planner-goal", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "1. inspect the implementation\n2. apply and verify the fix"}, {Type: provider.ChunkDone}},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Implemented and verified."}, {Type: provider.ChunkDone}}}
	plannerSess := sessionstore.NewSession("planner-sys")
	executor := agent.New(exec, reg, sessionstore.NewSession("exec-sys"), agent.Options{}, event.Discard)
	customPlannerReg := tool.NewRegistry()
	customPlannerReg.Add(goalTool)
	coord := NewCoordinator(planner, plannerSess, nil, customPlannerReg, agent.Options{}, executor, 0, event.Discard, nil)
	recorder := &coordinatorGoalRecorder{}
	ctx := tool.WithGoalTurnRecorder(context.Background(), recorder)
	if err := coord.Run(ctx, "fix the goal bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, req := range planner.requests {
		if slices.Contains(toolSchemaNames(req.Tools), "update_goal") {
			t.Fatalf("planner request %d offers update_goal while the executor owns the disposition: %v", i+1, toolSchemaNames(req.Tools))
		}
	}
	if got := lastToolResult(plannerSess, "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("planner update_goal result = %q", got)
	}
	for i, req := range exec.requests {
		if !slices.Contains(toolSchemaNames(req.Tools), "update_goal") {
			t.Fatalf("executor request %d lost update_goal while holding the recorder: %v", i+1, toolSchemaNames(req.Tools))
		}
	}
	if len(recorder.reports) != 0 {
		t.Fatalf("planner wrote reports into executor Goal recorder: %+v", recorder.reports)
	}
}
