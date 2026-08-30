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
	r := a.sess.compactionState.LastReceipt
	return r != nil && r.Status == "applied" &&
		strings.Contains(latestDigest(a.sess.compactionState.Projection.Messages), "summary was unavailable")
}

func prepareContext(ctx context.Context, a *Agent, trigger string) error {
	_, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: trigger})
	return err
}

// foldRegionOf is the region the next compaction would hand the summarizer.
func foldRegionOf(a *Agent) []provider.Message {
	canonical, version := a.sess.conversation.snapshotMessagesVersion()
	msgs, _ := a.visibleInputForFold(a.sess.compactionState, canonical, version)
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
	msgs, _ := a.sess.conversation.snapshotMessagesVersion()
	return estimateMessagesTokens(provider.ModelMessages(modelVisibleFromProjection(a.sess.compactionState.Projection, msgs)))
}

func TestSummarizerCancellationAtOverflowPropagatesWithoutFallback(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{hang: true}, sess)
	before := estimateMessagesTokens(provider.ModelMessages(sess.Messages))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := prepareContext(ctx, a, CompactionTriggerOverflow); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("prepare = %v, want context deadline", err)
	}
	if after := projectionTokens(a); after != 0 {
		t.Fatalf("cancellation installed projection tokens=%d (source=%d)", after, before)
	}
}

// Overflow is the trigger that reports ErrCompactionRequired, so it is where a
// failed summary turns into "context exceeds provider limit and compaction
// failed" without installing fabricated fallback content.
func TestOverflowSummarizerFailureRequiresCompaction(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{streamErr: errors.New("provider down")}, sess)
	before := estimateMessagesTokens(provider.ModelMessages(sess.Messages))

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("prepare = %v, want ErrCompactionRequired", err)
	}
	if after := projectionTokens(a); after != 0 {
		t.Fatalf("failed summary installed projection: %d (source=%d)", after, before)
	}
}

// An oversized complete-prefix request fails admission and must not fabricate
// a summary or privately shorten its input.
func TestSummarizerFailureOnOversizedFoldDoesNotFabricateDigest(t *testing.T) {
	sess := foldableSessionOverForce(120)
	a := agentOverForceWindow(t, &fakeProvider{streamErr: errors.New("provider exploded")}, sess, 60000)
	if tokens, budget := a.guardedSummaryInputTokens(foldRegionOf(a)), a.summaryInputBudget(""); budget <= 0 || tokens <= budget {
		t.Fatalf("fixture fold is %d tokens against a %d budget; the shortening path is not exercised", tokens, budget)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerOverflow); !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("prepare = %v, want ErrCompactionRequired", err)
	}
	if degradedFold(a) || latestDigest(a.sess.compactionState.Projection.Messages) != "" {
		t.Errorf("failed summary fabricated a digest: receipt=%+v", a.sess.compactionState.LastReceipt)
	}
}

// Below the hard ceiling the turn still goes out, so a failed summary must stay
// a failure: the recoverable view proceeds unchanged below the hard ceiling.
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
	if r := a.sess.compactionState.LastReceipt; r == nil || (r.Status != "blocked" && r.Status != "failed") {
		t.Errorf("receipt = %+v, want the failure recorded so the summary is not paid for twice", r)
	}
}

// The receipt recorded below the ceiling must not outlive the ceiling itself:
// once growing usage crosses the hard ceiling the fold is the only way out, so
// recovery has to run even with a standing failed receipt. If the summarizer is
// still down, hard pressure returns ErrCompactionRequired without a fake digest.
func TestFailedSummaryReceiptRetriesAtHardCeilingWithoutFallback(t *testing.T) {
	sess := foldableSessionOverForce(6)
	a := agentOverForce(t, &fakeProvider{streamErr: errors.New("provider down")}, sess)
	a.activeTurnCreatedAt.Store(42)
	if est, hard := a.estimatedPromptTokens(sess.Messages), a.hardInputCeiling(); est >= hard {
		t.Fatalf("fixture estimates %d tokens against a %d ceiling; it is not below it", est, hard)
	}

	// The failed pressure summary is recorded, not fatal: the turn goes out.
	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("prepare = %v, want the turn to proceed unfolded", err)
	}
	if r := a.sess.compactionState.LastReceipt; r == nil || (r.Status != "blocked" && r.Status != "failed") {
		t.Fatalf("receipt = %+v, want the failure recorded", r)
	}

	// The session keeps growing past the ceiling while the receipt stands.
	big := strings.Repeat("word ", 400)
	for range 4 {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	if est, hard := a.estimatedPromptTokens(sess.Messages), a.hardInputCeiling(); est < hard {
		t.Fatalf("grown fixture estimates %d tokens against a %d ceiling; it is not past it", est, hard)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("over-ceiling prepare = %v, want ErrCompactionRequired", err)
	}
	if degradedFold(a) {
		t.Fatal("hard-ceiling failure installed a mechanical digest")
	}
}

// A ceiling recovery that lands under the fold trigger clears the stuck
// latch: the next pressure round above the trigger must compact again instead
// of coasting back to the physical ceiling.
func TestCeilingRecoveryClearsStuckLatch(t *testing.T) {
	sess := foldableSessionOverForce(10)
	a := agentOverForce(t, &fakeProvider{reply: "digest"}, sess)
	if est, hard := a.estimatedPromptTokens(sess.Messages), a.hardInputCeiling(); est < hard {
		t.Fatalf("fixture estimates %d tokens against a %d ceiling; it is not past it", est, hard)
	}
	a.sess.compaction.stuck = true

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("ceiling recovery = %v, want a fold despite the latch", err)
	}
	if a.sess.compaction.stuck {
		t.Fatal("stale stuck latch survived a recovery that landed under the trigger")
	}
	version := a.currentProjectionVersion()
	if version == 0 {
		t.Fatal("recovery installed no projection")
	}

	// One big append jumps from under the trigger straight past it, still
	// below the ceiling: the pressure round must compact, not coast.
	big := strings.Repeat("word ", 400)
	for range 20 {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
		current := a.contextManager().currentPrepared()
		if current.InputTokens >= a.compactTrigger() && current.InputTokens < a.hardInputCeiling() {
			break
		}
	}
	if est, fold := a.contextManager().currentPrepared().InputTokens, a.compactTrigger(); est < fold {
		t.Fatalf("grown fixture estimates %d tokens, below fold %d", est, fold)
	}
	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("pressure round above the trigger: %v", err)
	}
	if a.currentProjectionVersion() == version {
		t.Fatal("pressure round above the trigger did not compact; the stale latch suppressed it")
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
