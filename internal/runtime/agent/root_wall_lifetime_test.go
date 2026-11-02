package agent

import (
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func TestRootWallBudgetContinuationAndReset(t *testing.T) {
	for _, mode := range []string{"continuation", "new_task", "reset", "override"} {
		t.Run(mode, func(t *testing.T) {
			root := New(&spendingProvider{}, tool.NewRegistry(), sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Wall: time.Hour}}, event.Discard)
			if err := root.Run(t.Context(), "first work"); err != nil {
				t.Fatal(err)
			}
			old := root.task.budget.rootTokens
			started := old.wall.started
			if started.IsZero() || old.wall.limit != time.Hour {
				t.Fatalf("root wall = %+v, want a real task start and explicit limit", old.wall)
			}
			time.Sleep(time.Millisecond)
			if mode != "new_task" {
				root.pending.preserveEvidence = true
			}
			if mode == "reset" {
				root.ResetTaskBudget()
			}
			ctx := t.Context()
			if mode == "override" {
				ctx = WithTaskBudget(ctx, TaskBudget{Wall: 2 * time.Hour})
			}
			if err := root.Run(ctx, "second work"); err != nil {
				t.Fatal(err)
			}
			same := root.task.budget.rootTokens.wall.started.Equal(started)
			if same != (mode == "continuation" || mode == "override") {
				t.Fatalf("same root start = %v for %s", same, mode)
			}
			if mode == "override" && root.task.budget.rootTokens.wall.limit != 2*time.Hour {
				t.Fatal("new root turn did not snapshot its explicit override")
			}
			if !old.wall.started.Equal(started) || old.wall.limit != time.Hour {
				t.Fatal("a new run mutated the wall budget already handed to an old job")
			}
		})
	}
}
