package agent

import (
	"context"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type childIsolationGoalRecorder struct {
	reports []tool.GoalReport
}

func (r *childIsolationGoalRecorder) RecordGoalReport(report tool.GoalReport) (string, error) {
	r.reports = append(r.reports, report)
	return "recorded " + report.Status, nil
}

func TestSubAgentDoesNotInheritParentGoalRecorder(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	prov := &scriptedProvider{name: "goal-child", turns: [][]provider.Chunk{
		{toolCallChunk("goal", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "Child result."}, {Type: provider.ChunkDone}},
	}}
	recorder := &childIsolationGoalRecorder{}
	ctx := tool.WithGoalTurnRecorder(context.Background(), recorder)
	sess := NewSession("child system")
	answer, err := RunSubAgentWithSession(ctx, prov, reg, sess, "inspect the task", Options{}, event.Discard)
	if err != nil {
		t.Fatalf("Goal child: %v", err)
	}
	if answer != "Child result." {
		t.Fatalf("Goal child answer = %q", answer)
	}
	for i, req := range prov.requests {
		if !slices.Contains(toolSchemaNames(req.Tools), "update_goal") {
			t.Fatalf("child request %d changed the static tool surface: %v", i+1, toolSchemaNames(req.Tools))
		}
	}
	if len(recorder.reports) != 0 {
		t.Fatalf("child wrote reports into parent Goal recorder: %+v", recorder.reports)
	}
	if got := lastToolResult(sess, "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("child update_goal result = %q", got)
	}
}

type coordinatorGoalRecorder struct {
	reports []tool.GoalReport
}

func (r *coordinatorGoalRecorder) RecordGoalReport(report tool.GoalReport) (string, error) {
	r.reports = append(r.reports, report)
	return "recorded " + report.Status, nil
}

func TestCoordinatorPlannerCannotReportExecutorGoalDisposition(t *testing.T) {
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(goalTool)
	planner := &mockProvider{name: "planner", streams: [][]provider.Chunk{
		{toolCallChunk("planner-goal", "update_goal", `{"status":"complete"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("plan-1", "submit_plan", `{"objective":"inspect the implementation","steps":[{"title":"apply and verify the fix"}]}`), {Type: provider.ChunkDone}},
	}}
	exec := &mockProvider{name: "executor", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "Implemented and verified."}, {Type: provider.ChunkDone}}}
	plannerSess := NewSession("planner-sys")
	executor := New(exec, reg, NewSession("exec-sys"), Options{}, event.Discard)
	customPlannerReg := tool.NewRegistry()
	customPlannerReg.Add(goalTool)
	coord := NewCoordinator(planner, plannerSess, nil, PlannerToolRegistry(customPlannerReg), Options{}, executor, 0, event.Discard, nil)
	recorder := &coordinatorGoalRecorder{}
	ctx := withNoClosedLoop(tool.WithGoalTurnRecorder(context.Background(), recorder))
	if err := coord.Run(ctx, "fix the goal bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i, req := range planner.requests {
		if !slices.Contains(toolSchemaNames(req.Tools), "update_goal") {
			t.Fatalf("planner request %d changed the static tool surface: %v", i+1, toolSchemaNames(req.Tools))
		}
	}
	if got := lastToolResult(plannerSess, "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("planner update_goal result = %q", got)
	}
	for i, req := range exec.requests {
		if !slices.Contains(toolSchemaNames(req.Tools), "update_goal") {
			t.Fatalf("executor request %d lost update_goal: %v", i+1, toolSchemaNames(req.Tools))
		}
	}
	if len(recorder.reports) != 0 {
		t.Fatalf("planner wrote reports into executor Goal recorder: %+v", recorder.reports)
	}
}
