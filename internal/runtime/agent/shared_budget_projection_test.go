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

// Reported-budget tests isolate accounting from pre-send reservation. Combined
// admission is exercised by shared-budget tests and the real recovery effect.
func reportedBudgetContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, sharedBudgetKey{}, newSharedBudget(TaskBudget{}, nil))
}

func TestTaskBudgetProjectionPreservesBothOwnersAndJobCancellation(t *testing.T) {
	for _, withLedger := range []bool{false, true} {
		a := New(&spendingProvider{}, tool.NewRegistry(), sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 1000, Wall: time.Hour}}, event.Discard)
		parent, cancelParent := context.WithCancel(t.Context())
		parent = a.PrepareRootTaskBudget(parent)
		root := parent.Value(rootTokenBudgetKey{}).(*rootTokenBudget)
		var ledger *sharedBudget
		if withLedger {
			ledger = newSharedBudget(TaskBudget{Tokens: 1000}, nil)
			parent = context.WithValue(parent, sharedBudgetKey{}, ledger)
		}
		job, cancelJob := context.WithCancel(t.Context())
		projected := InheritTaskBudget(job, parent)
		cancelParent()
		if projected.Err() != nil || projected.Value(rootTokenBudgetKey{}) != root {
			t.Fatal("job lost the root token/wall owner or inherited parent cancellation")
		}
		if got, _ := projected.Value(sharedBudgetKey{}).(*sharedBudget); got != ledger {
			t.Fatal("job lost its reservation owner")
		}
		provider.ObserveAuxiliaryUsage(projected, &provider.Usage{PromptTokens: 100, CompletionTokens: 20})
		if root.used.Load() != 120 || root.wall.limit != time.Hour {
			t.Fatal("auxiliary usage or wall bound was lost")
		}
		cancelJob()
		if projected.Err() != context.Canceled {
			t.Fatal("job lost its own cancellation")
		}
	}
}
