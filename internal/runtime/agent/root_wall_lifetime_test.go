package agent

import (
	"context"
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

func TestRootWallBudgetProjectionKeepsJobDeadline(t *testing.T) {
	root := &rootTokenBudget{wall: rootWallBudget{started: time.Now().Add(-time.Hour), limit: time.Minute}}
	parent, cancelParent := context.WithTimeout(context.WithValue(t.Context(), rootTokenBudgetKey{}, root), 2*time.Hour)
	defer cancelParent()
	job, cancelJob := context.WithTimeout(t.Context(), time.Hour)
	defer cancelJob()
	projected := InheritRootTokenBudget(job, parent)
	want, _ := job.Deadline()
	got, ok := projected.Deadline()
	if !ok || !got.Equal(want) || projected.Value(rootTokenBudgetKey{}) != root {
		t.Fatal("budget projection changed job deadline or lost the immutable root scope")
	}
	cancelParent()
	if projected.Err() != nil {
		t.Fatal("parent cancellation canceled the Manager-owned job")
	}
	if axis, _ := root.wall.exceeded(); axis != "time" {
		t.Fatalf("axis = %q, want inherited elapsed limit without context cancellation", axis)
	}
	cancelJob()
	if projected.Err() != context.Canceled {
		t.Fatal("job cancellation no longer controls its context")
	}
	for _, wall := range []rootWallBudget{{limit: time.Second}, {started: time.Now().Add(-time.Hour)}} {
		if axis, _ := wall.exceeded(); axis != "" {
			t.Fatalf("axis = %q, an unknown start or disabled limit cannot certify a crossing", axis)
		}
	}
}
