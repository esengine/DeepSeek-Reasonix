package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// A window of 20000 at ratio 0.5 folds at 10000 tokens, so a transient
// failure releases after 1250 more tokens of input.
func newPressureAgent(t *testing.T, p provider.Provider) *Agent {
	t.Helper()
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work output. ", 3200)},
		{Role: provider.RoleUser, Content: "current"},
		{Role: provider.RoleAssistant, Content: "tail"},
	}
	a := New(p, tool.NewRegistry(), &sessionstore.Session{Messages: msgs}, Options{
		ContextWindow: 20_000, CompactRatio: 0.5, RecentKeep: 2,
		WorkspaceID: "workspace", ModelRef: "model",
	}, event.Discard)
	a.BindSessionPath(testenv.TempDir(t)+"/session.jsonl", true)
	w := a.window()
	if est := w.estimatedVisibleRequestTokens(w.modelVisibleMessages()); est < w.compactTrigger() || est >= w.hardInputCeiling() {
		t.Fatalf("fixture est=%d must sit between trigger %d and ceiling %d", est, w.compactTrigger(), w.hardInputCeiling())
	}
	return a
}

func growBy(a *Agent, tokens int) {
	a.sess.conversation.Add(provider.Message{Role: provider.RoleUser, Content: strings.Repeat("more input words. ", tokens/4)})
}

func prepare(t *testing.T, a *Agent, trigger string) error {
	t.Helper()
	_, err := a.window().contextManager().Prepare(context.Background(), ContextPreparePolicy{Trigger: trigger})
	return err
}

func TestTransientSummaryFailureRetriesOnceAfterGrowth(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	_ = prepare(t, a, CompactionTriggerPressure)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 1 {
		t.Fatalf("unchanged input retried: calls=%d", p.calls)
	}
	growBy(a, 400)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 1 {
		t.Fatalf("growth below one step retried: calls=%d", p.calls)
	}
	growBy(a, 2000)
	_ = prepare(t, a, CompactionTriggerPressure)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 2 {
		t.Fatalf("growth past one step made %d requests, want 2", p.calls)
	}
}

func TestRepeatedTransientFailureIsBoundedByGrowth(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	const turns, perTurn = 40, 100
	for range turns {
		growBy(a, perTurn)
		_ = prepare(t, a, CompactionTriggerPressure)
	}
	step := a.window().retryGrowthStep()
	if limit := 1 + turns*perTurn/step + 1; p.calls < 2 || p.calls > limit {
		t.Fatalf("summary requests over %d turns = %d, want within [2,%d]", turns, p.calls, limit)
	}
}

func TestDeterministicFailureStaysBlockedDespiteGrowth(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	a.window().recordContextMaintenanceOutcome("", CompactionTriggerPressure, "summary", "blocked", FailSummaryInputTooLarge, "too large")
	growBy(a, 8000)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 0 {
		t.Fatalf("deterministic block retried after growth: calls=%d", p.calls)
	}
}

func TestTransientFailureDoesNotBlockOverflowRecovery(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	_ = prepare(t, a, CompactionTriggerPressure)
	err := prepare(t, a, CompactionTriggerOverflow)
	if errors.Is(err, ErrCompactionRequired) && p.calls == 1 {
		t.Fatalf("overflow was refused without trying: %v", err)
	}
	if p.calls != 2 {
		t.Fatalf("overflow made %d total requests, want 2", p.calls)
	}
}

func TestLegacyReceiptWithoutSizeReleasesOnce(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	a.sess.win.compactionState.LastReceipt = &sessionstore.ContextMaintenanceReceipt{
		Status: "failed", Action: "summary", Code: string(FailSummaryTruncated),
	}
	_ = prepare(t, a, CompactionTriggerPressure)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 1 {
		t.Fatalf("legacy transient receipt made %d requests, want exactly 1", p.calls)
	}
}

func TestLegacyBlockedHashOnlyFollowsInput(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	w := a.window()
	w.sess.win.compactionState.BlockedInputHash = w.contextMaintenanceInputHash(w.modelVisibleMessages())
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 0 {
		t.Fatalf("matching legacy hash did not block: calls=%d", p.calls)
	}
	growBy(a, 500)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 1 {
		t.Fatalf("changed input under a legacy hash made %d requests, want 1", p.calls)
	}
}

func TestLegacyHashDoesNotOverrideReleasedTransientReceipt(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	w := a.window()
	w.sess.win.compactionState.BlockedInputHash = w.contextMaintenanceInputHash(w.modelVisibleMessages())
	w.sess.win.compactionState.LastReceipt = &sessionstore.ContextMaintenanceReceipt{
		Status: "failed", Action: "summary", Code: string(FailSummaryTruncated),
	}
	if a.ContextMaintenanceSnapshot().Blocked {
		t.Fatal("status reports blocked for a released transient receipt")
	}
	if blocked, _ := w.contextMaintenanceBlocked(w.contextMaintenanceInputHash(w.modelVisibleMessages()), w.estimatedVisibleRequestTokens(w.modelVisibleMessages()), false); blocked {
		t.Fatal("manager reports blocked for a released transient receipt")
	}
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 1 {
		t.Fatalf("calls=%d, want 1", p.calls)
	}
}

// One pressure attempt plus at most two overflow recoveries on unchanged input.
func TestRepeatedOverflowOnUnchangedInputIsBounded(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	_ = prepare(t, a, CompactionTriggerPressure)
	for range 20 {
		_ = prepare(t, a, CompactionTriggerOverflow)
	}
	if p.calls > 3 {
		t.Fatalf("summary requests = %d", p.calls)
	}
}
