package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type tokenBudgetSiblings struct {
	readProbe
	children []*Agent
	errors   []error
}

func (t *tokenBudgetSiblings) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	var group sync.WaitGroup
	t.errors = make([]error, len(t.children))
	for i, child := range t.children {
		group.Go(func() { t.errors[i] = child.Run(ctx, "read a few files") })
	}
	group.Wait()
	return "children ended", nil
}

func TestRootTokenBudgetContinuationAndReset(t *testing.T) {
	for _, mode := range []string{"continuation", "new_task", "reset"} {
		t.Run(mode, func(t *testing.T) {
			child := New(&spendingProvider{}, tool.NewRegistry(), sessionstore.NewSession("child"), Options{}, event.Discard)
			tools := tool.NewRegistry()
			tools.Add(&tokenBudgetChildTool{child: child})
			prov := &spendingProvider{max: 1}
			root := New(prov, tools, sessionstore.NewSession("root"), Options{TaskBudget: TaskBudget{Tokens: 4000}}, event.Discard)
			if err := root.Run(t.Context(), "first work"); err != nil {
				t.Fatal(err)
			}
			old := root.task.budget.rootTokens
			if old.used.Load() != 3300 {
				t.Fatalf("root tokens = %d, want parent2200+child1100", old.used.Load())
			}
			if mode != "new_task" {
				root.pending.preserveEvidence = true
			}
			if mode == "reset" {
				root.ResetTaskBudget()
			}
			prov.rounds.Store(0)
			err := root.Run(t.Context(), "second work")
			if mode == "continuation" {
				pause, ok := InspectRunPause(err)
				if !ok || pause.Kind != "task_budget" || pause.Key != "token" {
					t.Fatalf("continuation = %v (%+v), want root token pause", err, pause)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				old.observe(&provider.Usage{PromptTokens: 5000})
				if root.task.budget.rootTokens.used.Load() != 3300 {
					t.Fatal("an old task context charged the replacement task")
				}
			}
		})
	}
}

func TestRootTokenScopeAtomicAccountingAndJobCancellation(t *testing.T) {
	root := &rootTokenBudget{used: new(atomic.Uint64), limit: 500}
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() { root.observe(&provider.Usage{PromptTokens: 7, CompletionTokens: 3}) })
	}
	group.Wait()
	if root.used.Load() != 1000 {
		t.Fatal("concurrent usage was lost or counted twice")
	}
	root.observe(nil)
	root.observe(&provider.Usage{PromptTokens: -3, CompletionTokens: -4})
	if root.used.Load() != 1000 {
		t.Fatal("missing or negative usage credited the root")
	}
	root.used.Store(^uint64(0) - 1)
	root.observe(&provider.Usage{PromptTokens: 10})
	if root.used.Load() != ^uint64(0) {
		t.Fatal("usage overflow reopened the budget")
	}
	parent, cancelParent := context.WithCancel(context.WithValue(t.Context(), rootTokenBudgetKey{}, root))
	job, cancelJob := context.WithCancel(t.Context())
	defer cancelJob()
	projected := InheritRootTokenBudget(job, parent)
	cancelParent()
	if projected.Err() != nil || projected.Value(rootTokenBudgetKey{}) != root {
		t.Fatal("job inherited parent cancellation or lost the budget")
	}
	cancelJob()
	if projected.Err() != context.Canceled {
		t.Fatal("budget projection lost job cancellation")
	}
}
