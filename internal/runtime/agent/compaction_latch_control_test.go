package agent

import (
	"testing"

	"reasonix/internal/state/sessionstore"
)

// The growth release belongs to the stale-context race alone, not to every
// refusal: a fold a plan or provider hook refused is a property of the input,
// so a turn's worth of growth must not re-ask it.
func TestPlanRefusalReceiptStillHoldsAfterGrowth(t *testing.T) {
	p := &fakeProvider{reply: "digest"}
	a := newPressureAgent(t, p)
	w := a.window()
	w.recordContextMaintenanceOutcome("", CompactionTriggerPressure, "summary", "blocked", FailHookRefused, "compaction extension refused the fold")
	for _, code := range []CompactionNoopReason{FailHookRefused, FailPersistFailed, NoopCandidateAboveTrigger} {
		r := &sessionstore.ContextMaintenanceReceipt{Status: "blocked", Action: "summary", Code: string(code), InputTokens: 1000}
		if !w.blockedReceiptHolds(r, 1000+w.retryGrowthStep(1000)*4) {
			t.Errorf("%s: a refusal growth cannot change was released", code)
		}
	}
	growBy(a, 6000)
	_ = prepare(t, a, CompactionTriggerPressure)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.got != nil {
		t.Fatal("a plan-refused fold was retried after growth: the release is not scoped to the stale-context race")
	}
}
