package agent

import (
	"strings"
	"testing"
	"time"
)

func TestSoftBudgetUsesRollingMedianElapsedTime(t *testing.T) {
	a := &Agent{}
	a.modelRef = t.Name()
	key := a.softBudgetHistoryKey()
	t.Cleanup(func() {
		readonlySoftBudgetHistory.Lock()
		delete(readonlySoftBudgetHistory.byKey, key)
		readonlySoftBudgetHistory.Unlock()
	})
	for _, sample := range []time.Duration{90, 95, 100, 105, 110} {
		recordReadonlySoftBudgetDuration(key, sample*time.Millisecond)
	}
	a.turn.budget = runBudget{started: time.Now().Add(-250 * time.Millisecond), rounds: 2}
	first := a.applySoftBudget(nil)
	if first.verdict != verdictRedirect || !strings.Contains(first.notice.Detail, "2x rolling median") {
		t.Fatalf("elapsed-time nudge = %+v", first)
	}

	a.turn.budget.rounds = 4
	second := a.applySoftBudget(nil)
	if second.verdict != verdictRedirect || !strings.Contains(second.guidance, "two further rounds") {
		t.Fatalf("hard follow-up = %+v", second)
	}
}
