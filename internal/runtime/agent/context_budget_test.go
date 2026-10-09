package agent

import "testing"

func TestContextBudgetUnmeasuredWithoutWindow(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 0, compactRatio: defaultCompactRatio}}
	budget := a.ContextBudget()
	if budget.Known() {
		t.Fatalf("budget reported as known without a window: %+v", budget)
	}
	if budget.Status != "unmeasured" || budget.Reason == "" {
		t.Fatalf("unmeasured budget must name a reason: %+v", budget)
	}
}
