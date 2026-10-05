package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
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
	sess := sessionstore.NewSession("child system")
	answer, err := RunSubAgentWithSession(ctx, prov, reg, sess, "inspect the task", Options{}, event.Discard)
	if err != nil {
		t.Fatalf("Goal child: %v", err)
	}
	if answer != "Child result." {
		t.Fatalf("Goal child answer = %q", answer)
	}
	for i, req := range prov.requests {
		if slices.Contains(toolSchemaNames(req.Tools), "update_goal") {
			t.Fatalf("child request %d offers update_goal: the parent's recorder does not reach the child, so its schema must not carry the tool: %v", i+1, toolSchemaNames(req.Tools))
		}
	}
	if len(recorder.reports) != 0 {
		t.Fatalf("child wrote reports into parent Goal recorder: %+v", recorder.reports)
	}
	if got := lastToolResult(sess, "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("child update_goal result = %q", got)
	}
}
