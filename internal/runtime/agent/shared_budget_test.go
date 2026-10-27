package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
	"sync"
	"testing"
)

func TestSharedBudgetCountsParallelBranchesAndSurvivesJobContext(t *testing.T) {
	b := newSharedBudget(TaskBudget{Tokens: 100}, nil)
	ctx := context.WithValue(context.Background(), sharedBudgetKey{}, b)
	job := InheritTaskBudget(context.Background(), ctx)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { b.observe(&provider.Usage{PromptTokens: 10}, nil) })
	}
	wg.Wait()
	if err := CheckSharedTaskBudget(job); err == nil {
		t.Fatal("parallel spend escaped budget")
	}
	if err := CheckSharedTaskBudget(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type budgetChildProbe struct {
	readProbe
	child *Agent
	calls int
}

func (p *budgetChildProbe) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	p.calls++
	return "child completed", p.child.Run(ctx, "inspect")
}

func TestSharedBudgetStopsParentAfterChildSpendThroughRealRuns(t *testing.T) {
	child := New(&spendingProvider{max: 0}, tool.NewRegistry(), sessionstore.NewSession("child"), Options{SubagentDepth: 1}, event.Discard)
	probe := &budgetChildProbe{child: child}
	reg := tool.NewRegistry()
	reg.Add(probe)
	prov := &spendingProvider{max: 500}
	parent := New(prov, reg, sessionstore.NewSession("parent"), Options{TaskBudget: TaskBudget{Tokens: 2000}}, event.Discard)
	err := parent.Run(context.Background(), "inspect")
	pause, ok := InspectRunPause(err)
	if !ok || pause.Key != "token" || probe.calls != 1 || prov.rounds.Load() != 1 {
		t.Fatalf("shared budget failed: pause=%+v calls=%d rounds=%d err=%v", pause, probe.calls, prov.rounds.Load(), err)
	}
}

func TestSharedCostBudgetDoesNotAddDifferentCurrencies(t *testing.T) {
	usd := &provider.Pricing{Input: 1000000, Currency: "USD"}
	cny := &provider.Pricing{Input: 1000000, Currency: "CNY"}
	b := newSharedBudget(TaskBudget{Cost: 2}, usd)
	b.observe(&provider.Usage{PromptTokens: 1}, usd)
	b.observe(&provider.Usage{PromptTokens: 9}, cny)
	if axis, _ := b.exceeded(); axis != "" {
		t.Fatal("mixed currencies crossed budget")
	}
	b.observe(&provider.Usage{PromptTokens: 1}, usd)
	if axis, _ := b.exceeded(); axis != "cost" {
		t.Fatal("known same-currency spending was lost")
	}
}
