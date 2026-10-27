package agent

import (
	"context"
	"fmt"
	"os"
	"reasonix/internal/contract/pricing"
	"time"
)

func RestoreSharedTaskBudget(ctx context.Context, sessionPath, scope string) (context.Context, error) {
	if scope == "" {
		return ctx, nil
	}
	path := budgetLedgerPath(sessionPath, scope)
	if path == "" {
		return ctx, fmt.Errorf("persisted task budget needs its owning conversation")
	}
	if _, err := os.Stat(path); err != nil {
		return ctx, fmt.Errorf("restore task budget: %w", err)
	}
	b := newSharedBudget(TaskBudget{}, nil)
	b.path, b.scope = path, scope
	b.requireExisting = true
	if err := b.transact(ctx, func() error { return nil }); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, sharedBudgetKey{}, b), nil
}

// ResetPersistedTaskBudget opens a new explicitly approved spend slice.
func (a *Agent) ResetPersistedTaskBudget(scope string) error {
	path := budgetLedgerPath(a.sess.path, scope)
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			b := newSharedBudget(a.task.budget.limit, a.svc.pricing)
			b.path, b.scope = path, scope
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := b.transact(ctx, func() error {
				b.started = time.Now()
				b.tokens = 0
				b.cost = map[string]pricing.Amount{}
				b.reserved = map[string]budgetReservation{}
				return nil
			}); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	a.ResetTaskBudget()
	return nil
}
