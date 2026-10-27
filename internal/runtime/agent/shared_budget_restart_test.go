package agent

import (
	"context"
	"path/filepath"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
	"testing"
)

func TestBudgetRestartKeepsGoalSpendAndExplicitRenewalOpensNewSlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	b := newSharedBudget(TaskBudget{Tokens: 1000}, nil)
	b.path = budgetLedgerPath(path, "goal")
	b.scope = "goal"
	id, _, err := b.reserve(context.Background(), 500, 500, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.settle(context.Background(), id, &provider.Usage{PromptTokens: 500, CompletionTokens: 500}, nil); err != nil {
		t.Fatal(err)
	}
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{ID: "goal", TaskText: "resume"})
	p := &spendingProvider{max: 0}
	a := New(p, tool.NewRegistry(), sessionstore.NewSession("sys"), Options{SessionPath: path, TaskBudget: TaskBudget{Tokens: 1000}}, event.Discard)
	if err := a.Run(ctx, "continue"); err == nil {
		t.Fatal("restart renewed spent allowance")
	}
	if p.rounds.Load() != 0 {
		t.Fatal("exhausted budget reached provider")
	}
	if err := a.ResetPersistedTaskBudget("goal"); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreSharedTaskBudget(context.Background(), path, "goal")
	if err != nil {
		t.Fatal(err)
	}
	owner := restored.Value(sharedBudgetKey{}).(*sharedBudget)
	if owner.tokens != 0 || len(owner.reserved) != 0 {
		t.Fatal("explicit renewal did not reset its spend slice")
	}
	if _, _, err := owner.reserve(context.Background(), 100, 100, nil); err != nil {
		t.Fatal(err)
	}
}
