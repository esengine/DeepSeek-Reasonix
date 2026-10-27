package agent

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func TestPreparedRootBudgetLifetime(t *testing.T) {
	for _, mode := range []string{"continuation", "new_scope", "new_task", "reset", "new_session"} {
		t.Run(mode, func(t *testing.T) {
			a := New(&spendingProvider{}, tool.NewRegistry(), sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 5000, Wall: time.Hour}}, event.Discard)
			ctx := WithDeliveryExecutionScope(t.Context(), DeliveryExecutionScope{ID: "first"})
			first := a.PrepareRootTaskBudget(ctx)
			provider.ObserveAuxiliaryUsage(first, &provider.Usage{PromptTokens: 1000, CompletionTokens: 100})
			old := a.task.budget.rootTokens
			projected := InheritRootTokenBudget(context.Background(), first)
			if err := a.Run(first, "read-only answer"); err != nil {
				t.Fatal(err)
			}
			if a.task.budget.rootTokens != old || old.used.Load() != 2200 {
				t.Fatalf("planning scope lost at Run boundary: %d", old.used.Load())
			}
			switch mode {
			case "new_scope":
				ctx = WithDeliveryExecutionScope(t.Context(), DeliveryExecutionScope{ID: "second"})
			case "new_task":
				ctx = t.Context()
			case "reset":
				a.ResetTaskBudget()
			case "new_session":
				a.SetSession(sessionstore.NewSession("replacement"))
			}
			next := a.PrepareRootTaskBudget(WithTaskBudget(ctx, TaskBudget{Tokens: 7000, Wall: 2 * time.Hour}))
			current := a.task.budget.rootTokens
			if current.limit != 7000 || current.wall.limit != 2*time.Hour || old.limit != 5000 || old.wall.limit != time.Hour {
				t.Fatal("turn override mutated an old scope")
			}
			if mode == "continuation" {
				if current.used != old.used || !current.wall.started.Equal(old.wall.started) {
					t.Fatal("continuation lost counters or clock")
				}
			} else if current.used == old.used || current.used.Load() != 0 {
				t.Fatal("replacement inherited old spend")
			}
			provider.ObserveAuxiliaryUsage(projected, &provider.Usage{PromptTokens: 500})
			want := uint64(0)
			if mode == "continuation" {
				want = 2700
			}
			if current.used.Load() != want {
				t.Fatalf("late old scope charged wrong generation: %d want%d", current.used.Load(), want)
			}
			if inherited := a.PrepareRootTaskBudget(next); inherited != next {
				t.Fatal("inherited context was rebound")
			}
		})
	}
}

func TestPreparedRootBudgetProjectionAndPreservedTurn(t *testing.T) {
	a := New(&spendingProvider{}, tool.NewRegistry(), sessionstore.NewSession("root"), Options{}, event.Discard)
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	bound := a.PrepareRootTaskBudget(WithTaskBudget(parent, TaskBudget{Tokens: 5000}))
	provider.ObserveAuxiliaryUsage(bound, &provider.Usage{PromptTokens: 1000})
	old := a.task.budget.rootTokens
	a.pending.preserveEvidence = true
	continued := a.PrepareRootTaskBudget(WithTaskBudget(parent, TaskBudget{Tokens: 6000}))
	if a.task.budget.rootTokens.used != old.used {
		t.Fatal("host-preserved continuation reset spend")
	}
	job, stop := context.WithCancel(t.Context())
	defer stop()
	projected := InheritRootTokenBudget(job, continued)
	cancel()
	if projected.Err() != nil {
		t.Fatal("budget projection copied parent cancellation")
	}
	provider.ObserveAuxiliaryUsage(projected, &provider.Usage{CompletionTokens: 100})
	if old.used.Load() != 1100 {
		t.Fatal("projected job lost root scope")
	}
	stop()
	if projected.Err() != context.Canceled {
		t.Fatal("projection lost job cancellation")
	}
}
