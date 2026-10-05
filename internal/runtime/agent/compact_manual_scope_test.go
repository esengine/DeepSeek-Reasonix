package agent

import (
	"context"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// manualAgent sits well below the trigger, which is where a hand-typed compact
// lives: a session that has reached the automatic threshold would have been
// folded without anyone asking.
func manualAgent(t *testing.T, sess *sessionstore.Session) (*Agent, *countingProvider) {
	t.Helper()
	prov := &countingProvider{reply: "## Standing facts\n- never change the public API"}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 200_000, CompactRatio: 0.8, RecentKeep: 2,
		SessionPath: filepath.Join(testenv.TempDir(t), "session.jsonl"),
		WorkspaceID: "ws", ModelRef: "p/m",
		ArchiveDir: testenv.TempDir(t),
	}, event.Discard)
	return a, prov
}

func addTurns(sess *sessionstore.Session, n, words int) {
	body := strings.Repeat("word ", words)
	for range n {
		sess.Messages = append(sess.Messages,
			provider.Message{Role: provider.RoleAssistant, Content: body},
			provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
}

// Asking is what makes a compaction manual, so the automatic threshold is
// waived — otherwise the command does nothing until the session is already
// large enough that it would have folded on its own.
func TestManualCompactRunsBelowTheAutomaticTrigger(t *testing.T) {
	a, prov := manualAgent(t, foldableSessionOverForce(60))
	if est := a.window().estimatedPromptTokens(a.window().modelVisibleMessages()); est >= a.window().compactTrigger() {
		t.Fatalf("fixture is not below the trigger: %d >= %d", est, a.window().compactTrigger())
	}
	verdict, err := a.CompactNow(context.Background(), CompactRequest{})
	if err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if !verdict.Compacted() || len(prov.got) != 1 {
		t.Fatalf("verdict=%+v calls=%d, want one fold below the trigger", verdict, len(prov.got))
	}
}

// The complaint this whole cut is about: asking twice over the same context
// bought a second summary of it. Requesting waives the threshold, never this.
func TestManualCompactOnAnUnchangedContextBuysNothing(t *testing.T) {
	a, prov := manualAgent(t, foldableSessionOverForce(60))
	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("first CompactNow: %v", err)
	}
	first := len(prov.got)

	verdict, err := a.CompactNow(context.Background(), CompactRequest{})
	if err != nil {
		t.Fatalf("second CompactNow: %v", err)
	}
	if len(prov.got) != first {
		t.Errorf("the second request made %d more summarizer calls", len(prov.got)-first)
	}
	if verdict.Compacted() {
		t.Error("an unchanged context reported a fold")
	}
	// Which gate declines depends on whether the first fold moved the visible
	// input: an unchanged view is caught by the hash, a fresh checkpoint by the
	// new-history rule. The contract is that one of them catches it and says so.
	if verdict.Reason == "" || CompactDeclineText(verdict.Reason) == "" {
		t.Errorf("verdict = %+v, want a reason a frontend can show", verdict)
	}
}

// A checkpoint costs the whole prefix cache, so a second one waits for enough
// new closed history to pay for it — the same rule the automatic path uses,
// now applied to the path that actually fires in practice.
func TestManualCompactWaitsForEnoughNewHistory(t *testing.T) {
	sess := foldableSessionOverForce(60)
	a, prov := manualAgent(t, sess)
	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("first CompactNow: %v", err)
	}
	first := len(prov.got)

	addTurns(sess, 1, 5)
	verdict, err := a.CompactNow(context.Background(), CompactRequest{})
	if err != nil {
		t.Fatalf("CompactNow after a little more history: %v", err)
	}
	if len(prov.got) != first {
		t.Errorf("a few hundred new tokens bought %d more summarizer calls", len(prov.got)-first)
	}
	if verdict.Compacted() || verdict.Reason == "" {
		t.Errorf("verdict = %+v, want a declined one carrying its reason", verdict)
	}
	if CompactDeclineText(verdict.Reason) == "" {
		t.Errorf("no words for reason %q", verdict.Reason)
	}
}

// Declining is not refusing to work: once enough has been added, asking folds.
func TestManualCompactFoldsOnceEnoughHasBeenAdded(t *testing.T) {
	sess := foldableSessionOverForce(60)
	a, prov := manualAgent(t, sess)
	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("first CompactNow: %v", err)
	}
	first := len(prov.got)

	// More than the recent tail keeps back: only what closes behind the tail
	// counts as new foldable history, so a session's worth means well over one
	// tail's worth of tokens.
	addTurns(sess, 150, 400)
	verdict, err := a.CompactNow(context.Background(), CompactRequest{})
	if err != nil {
		t.Fatalf("CompactNow after real work: %v", err)
	}
	if !verdict.Compacted() {
		t.Fatalf("verdict = %+v, want a fold once a session's worth of work was added", verdict)
	}
	if got := len(prov.got) - first; got != 1 {
		t.Errorf("summarizer calls for the second fold = %d, want 1", got)
	}
}

// The escape hatch stays an escape hatch: it waives economics and nothing else.
func TestForcedCompactWaivesEconomicsOnly(t *testing.T) {
	a, prov := manualAgent(t, foldableSessionOverForce(60))
	if _, err := a.CompactNow(context.Background(), CompactRequest{}); err != nil {
		t.Fatalf("first CompactNow: %v", err)
	}
	first := len(prov.got)

	verdict, err := a.CompactNow(context.Background(), CompactRequest{IgnoreEconomics: true})
	if err != nil && !IsCompactionDeclined(err) {
		t.Fatalf("forced CompactNow: %v", err)
	}
	if len(prov.got) == first {
		t.Error("a forced request over the same context made no summarizer call")
	}
	// Whatever it bought, the projection stays a projection: a candidate that
	// is not smaller than what it replaces is still refused.
	if err == nil && verdict.Compacted() {
		state := a.sess.win.compactionState
		if state.LastReceipt == nil || state.LastReceipt.ResultTokens >= state.LastReceipt.InputTokens {
			t.Errorf("forced fold installed a projection that is not smaller: %+v", state.LastReceipt)
		}
	}
}
