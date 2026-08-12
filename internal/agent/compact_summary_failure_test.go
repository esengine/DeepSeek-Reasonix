package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// foldableSessionOverForce builds a transcript whose bulk is assistant text, so
// the free prune pass cannot reclaim it and Prepare must reach the summarizer.
func foldableSessionOverForce(turns int) *Session {
	big := strings.Repeat("word ", 400)
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "standing constraint: never change the public API"},
	}
	for range turns {
		msgs = append(msgs,
			provider.Message{Role: provider.RoleAssistant, Content: big},
			provider.Message{Role: provider.RoleUser, Content: "continue"},
		)
	}
	return &Session{Messages: msgs}
}

func agentOverForce(t *testing.T, prov provider.Provider, sess *Session) *Agent {
	t.Helper()
	return agentOverForceWindow(t, prov, sess, 5000)
}

// agentOverForceWindow sits the session above the force ratio. A folded
// transcript lands back under the trigger, so a blocked turn can only mean the
// fold itself failed.
func agentOverForceWindow(t *testing.T, prov provider.Provider, sess *Session, window int) *Agent {
	t.Helper()
	return New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow:     window,
		CompactRatio:      0.5,
		CompactForceRatio: 0.5,
		RecentKeep:        2,
		ArchiveDir:        t.TempDir(),
	}, event.Discard)
}

// degradedFold reports whether a fold was committed with the mechanical digest
// standing in for the summary. The receipt is the host record that the
// projection was installed; the digest text is what the model is actually told.
func degradedFold(a *Agent) bool {
	r := a.compactionState.LastReceipt
	return r != nil && r.Status == "applied" &&
		strings.Contains(latestDigest(a.compactionState.Projection.Messages), "summary was unavailable")
}

func prepareContext(ctx context.Context, a *Agent, trigger string) error {
	_, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: trigger})
	return err
}

// foldRegionOf is the region the next compaction would hand the summarizer.
func foldRegionOf(a *Agent) []provider.Message {
	canonical, version := a.session.snapshotMessagesVersion()
	msgs := a.visibleInputForFold(a.compactionState, canonical, version)
	head, start, ok := a.planFoldRegion(msgs, false)
	if !ok {
		return nil
	}
	_, fold, _ := a.partitionFoldForProjection(msgs[head:start])
	return fold
}

// latestDigest returns the text of the last compaction digest in a projection.
func latestDigest(msgs []provider.Message) string {
	for _, m := range slices.Backward(msgs) {
		if isCompactionSummary(m) {
			return m.Content
		}
	}
	return ""
}

// projectionTokens reports what the model would actually see.
func projectionTokens(a *Agent) int {
	msgs, _ := a.session.snapshotMessagesVersion()
	return estimateMessagesTokens(provider.ModelMessages(modelVisibleFromProjection(a.compactionState.Projection, msgs)))
}

// The 90s summary bound is deliberately not retried, so a summarizer that never
// answers is the original reason a mechanical fold exists. Where the fold is the
// only way out, its timeout must free the context rather than strand the turn.
func TestSummarizerTimeoutWhereFoldIsTheOnlyWayOutDegrades(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{hang: true}, sess)
	before := estimateMessagesTokens(provider.ModelMessages(sess.Messages))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := prepareContext(ctx, a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v, want a degraded fold instead of a hard failure", err)
	}
	after := projectionTokens(a)
	t.Logf("degraded fold: est %d -> %d tokens", before, after)
	if after >= before {
		t.Fatalf("degraded fold freed no context: %d -> %d", before, after)
	}
	if !degradedFold(a) {
		t.Errorf("no degraded fold committed: receipt=%+v digest=%q",
			a.compactionState.LastReceipt, latestDigest(a.compactionState.Projection.Messages))
	}
}

// Overflow is the trigger that reports ErrCompactionRequired, so it is where a
// failed summary turns into "context exceeds provider limit and compaction
// failed" and blocks every further message. A mechanical fold answers it.
func TestOverflowSummarizerFailureDegradesInsteadOfBlockingTheTurn(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{streamErr: errors.New("provider down")}, sess)
	before := estimateMessagesTokens(provider.ModelMessages(sess.Messages))

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v, want a degraded fold instead of ErrCompactionRequired", err)
	}
	if after := projectionTokens(a); after >= before {
		t.Fatalf("degraded fold freed no context: %d -> %d", before, after)
	}
	if !degradedFold(a) {
		t.Errorf("no degraded fold committed: receipt=%+v", a.compactionState.LastReceipt)
	}
}

// A fold too large for one request is shortened before it is sent, which is a
// second way into the same failure. That path must degrade like a plain
// summarizer failure rather than strand the turn.
func TestSummarizerFailureOnOversizedFoldDegrades(t *testing.T) {
	sess := foldableSessionOverForce(120)
	a := agentOverForceWindow(t, &fakeProvider{streamErr: errors.New("provider exploded")}, sess, 60000)
	if tokens, budget := a.guardedSummaryInputTokens(foldRegionOf(a)), a.summaryInputBudget(""); budget <= 0 || tokens <= budget {
		t.Fatalf("fixture fold is %d tokens against a %d budget; the shortening path is not exercised", tokens, budget)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); err != nil {
		t.Fatalf("prepare = %v, want a degraded fold", err)
	}
	if !degradedFold(a) {
		t.Errorf("no degraded fold committed: receipt=%+v", a.compactionState.LastReceipt)
	}
}

// Below the hard ceiling the turn still goes out, so a failed summary must stay
// a failure: degrading here would fold a recoverable view without a summary and
// spend the mechanical fold on a turn that never needed it.
func TestPressureBelowHardCeilingKeepsTheFailure(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{streamErr: errors.New("provider down")}, sess)
	if est, hard := a.estimatedPromptTokens(sess.Messages), a.hardInputCeiling(); est >= hard {
		t.Fatalf("fixture estimates %d tokens against a %d ceiling; it is not below it", est, hard)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("prepare = %v, want the turn to proceed unfolded", err)
	}
	if degradedFold(a) {
		t.Error("a recoverable view was folded without a summary")
	}
	if r := a.compactionState.LastReceipt; r == nil || (r.Status != "blocked" && r.Status != "failed") {
		t.Errorf("receipt = %+v, want the failure recorded so the summary is not paid for twice", r)
	}
}

// Cancellation is the user's decision, not a summarizer failure: it must keep
// its error and leave the projection alone.
func TestCallerCancellationDoesNotDegrade(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{hang: true}, sess)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := prepareContext(ctx, a, CompactionTriggerOverflow); err == nil {
		t.Fatal("cancelled prepare reported success")
	}
	if degradedFold(a) {
		t.Error("cancellation installed a degraded fold; it should change nothing")
	}
}
