package builtin

import (
	"context"
	"testing"

	"reasonix/internal/jobs"
	"reasonix/internal/planmode"
	"reasonix/internal/tool"
)

type visibilityRecorder struct{}

func (visibilityRecorder) RecordGoalReport(tool.GoalReport) (string, error) { return "", nil }

func TestContextualBuiltinVisibilityFollowsOwningContext(t *testing.T) {
	goal, _ := tool.LookupBuiltin("update_goal")
	step, _ := tool.LookupBuiltin("complete_step")
	jobNames := []string{"bash_output", "wait", "kill_shell"}

	if goal.(tool.ContextualTool).ProviderVisible(context.Background()) {
		t.Fatal("update_goal visible without a Goal recorder")
	}
	if !goal.(tool.ContextualTool).ProviderVisible(tool.WithGoalTurnRecorder(context.Background(), visibilityRecorder{})) {
		t.Fatal("update_goal hidden during an active Goal turn")
	}
	if !step.(tool.ContextualTool).ProviderVisible(context.Background()) {
		t.Fatal("complete_step hidden outside Plan mode")
	}
	if step.(tool.ContextualTool).ProviderVisible(planmode.WithActive(context.Background(), true)) {
		t.Fatal("complete_step visible during Plan mode")
	}
	for _, name := range jobNames {
		t.Run(name, func(t *testing.T) {
			candidate, ok := tool.LookupBuiltin(name)
			if !ok {
				t.Fatal("missing builtin")
			}
			contextual := candidate.(tool.ContextualTool)
			if contextual.ProviderVisible(context.Background()) {
				t.Fatal("job tool visible without a manager")
			}
			if !contextual.ProviderVisible(jobs.WithManager(context.Background(), &jobs.Manager{})) {
				t.Fatal("job tool hidden with an owned manager")
			}
		})
	}
}
