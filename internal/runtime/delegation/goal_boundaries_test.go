package delegation

import (
	"context"
	"testing"
)

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
