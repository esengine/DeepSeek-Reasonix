package agent

import (
	"testing"
)

func TestRetentionBudgetsKeepTheirWindowShareWhenTheTriggerIsLoose(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 128_000, compactRatio: defaultCompactRatio}}
	w := a.window()
	if got, want := w.keptUserTurnsBudget(), 6_400; got != want {
		t.Fatalf("keptUserTurnsBudget = %d, want %d", got, want)
	}
	if got, want := w.keptFailuresBudget(), 6_400; got != want {
		t.Fatalf("keptFailuresBudget = %d, want %d", got, want)
	}
	if got, want := w.keptMarkedBudget(), 19_200; got != want {
		t.Fatalf("keptMarkedBudget = %d, want %d", got, want)
	}
}
