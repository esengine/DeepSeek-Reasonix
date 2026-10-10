package agent

import (
	"testing"

	"reasonix/internal/state/sessionstore"
)

// A candidate refused for the protected content it carries is answered
// differently by a larger input, and by a forced fold that waives the ceiling.
// The hold must lift on growth, or the window fills with nothing left to try.
func TestRefusedCandidateHoldLiftsOnGrowthAndOverflow(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{}).window()
	failedAt := a.compactTrigger() + 100
	for _, code := range []CompactionNoopReason{NoopCandidateAboveCeiling} {
		r := &sessionstore.ContextMaintenanceReceipt{Status: "blocked", Action: "summary", Code: string(code), InputTokens: failedAt}
		if !a.blockedReceiptHolds(r, failedAt) {
			t.Errorf("%s: an unchanged input must stay held", code)
		}
		if a.blockedReceiptHolds(r, failedAt+a.retryGrowthStep(failedAt)) {
			t.Errorf("%s: the hold survived the growth step", code)
		}
	}
	for _, code := range []CompactionNoopReason{FailPersistFailed, FailHookRefused, NoopFixedPrefixAboveTrigger, NoopCandidateAboveTrigger, NoopDigestLostEveryChange} {
		r := &sessionstore.ContextMaintenanceReceipt{Status: "blocked", Action: "summary", Code: string(code), InputTokens: failedAt}
		if !a.blockedReceiptHolds(r, failedAt+a.retryGrowthStep(failedAt)*4) {
			t.Errorf("%s: a class growth cannot change must keep holding", code)
		}
	}
}

// A second ceiling refusal at a larger size replaces the first, otherwise the release
// point never moves and every later request pays for another summary.
func TestRefusedCandidateReceiptMovesWithTheRetry(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{}).window()
	a.recordContextMaintenanceBlocked("h1", CompactionTriggerPressure, "summary", NoopCandidateAboveCeiling, "first")
	first := a.sess.win.compactionState.LastReceipt
	if first == nil || first.Code != string(NoopCandidateAboveCeiling) {
		t.Fatalf("receipt not recorded: %+v", first)
	}
	gen := a.sess.win.compactionState.Generation
	a.recordContextMaintenanceBlocked("h2", CompactionTriggerPressure, "summary", NoopCandidateAboveCeiling, "second")
	if a.sess.win.compactionState.Generation == gen {
		t.Fatal("the second refusal was dropped: the hold would keep its first release point")
	}
}

// An overflow passes a ceiling refusal only for an input that has changed since
// it: the forced fold waives the ceiling, but repeating it on the same bytes
// buys nothing.
func TestOverflowPassesCeilingRefusalOnlyForChangedInput(t *testing.T) {
	a := newPressureAgent(t, &failingSummaryProvider{}).window()
	tokens := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	a.sess.win.compactionState.LastReceipt = &sessionstore.ContextMaintenanceReceipt{
		Status: "blocked", Action: "summary", Code: string(NoopCandidateAboveCeiling), InputTokens: tokens, InputHash: "seen",
	}
	if blocked, _ := a.contextMaintenanceBlocked("seen", tokens, true); !blocked {
		t.Error("an overflow on the very input that was refused must stay held")
	}
	if blocked, _ := a.contextMaintenanceBlocked("changed", tokens, true); blocked {
		t.Error("an overflow on a changed input must be allowed to fold")
	}
}
