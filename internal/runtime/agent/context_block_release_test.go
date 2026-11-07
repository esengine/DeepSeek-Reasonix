package agent

import (
	"context"
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
		ContextWindow: 20_000, CompactRatio: 0.5, WorkspaceID: "workspace", ModelRef: "model",
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

func TestDeterministicFailureStaysBlockedDespiteGrowth(t *testing.T) {
	p := &failingSummaryProvider{}
	a := newPressureAgent(t, p)
	a.window().recordContextMaintenanceOutcome("", CompactionTriggerPressure, "summary", "blocked", NoopFixedPrefixAboveTrigger, "too large")
	growBy(a, 8000)
	_ = prepare(t, a, CompactionTriggerPressure)
	if p.calls != 0 {
		t.Fatalf("deterministic block retried after growth: calls=%d", p.calls)
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
