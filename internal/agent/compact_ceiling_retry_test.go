package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"reasonix/internal/provider"
)

// failThenSucceedProvider fails the first N summary streams with a transient
// error, then serves a real summary.
type failThenSucceedProvider struct {
	failures atomic.Int32
}

func (p *failThenSucceedProvider) Name() string { return "fail-then-succeed" }
func (p *failThenSucceedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.failures.Add(1) == 1 {
		return nil, errors.New("transient network failure")
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "Digest: the work completed; the rate limiter remains."}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A transient summarizer failure at the hard ceiling must not kill an entire
// unattended turn with zero output (#8240): the prompt physically cannot ship
// unfolded, so the fold gets exactly one immediate retry before the turn is
// allowed to fail. The first Prepare attempt (under the fold trigger) consumes
// one scripted failure; the over-ceiling attempt consumes the second and the
// retry succeeds.
func TestCeilingSummaryFailureRetriesOnceBeforeFailingTurn(t *testing.T) {
	sess := foldableSessionOverForce(6)
	prov := &failThenSucceedProvider{}
	a := agentOverForce(t, prov, sess)

	// Grow past the hard ceiling with two scripted transient failures pending.
	big := bigFoldWord()
	for range 6 {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}
	if est, hard := a.estimatedPromptTokens(sess.Messages), a.hardInputCeiling(); est < hard {
		t.Fatalf("fixture estimates %d against ceiling %d; not over it", est, hard)
	}

	if err := prepareContext(context.Background(), a, CompactionTriggerPressure); err != nil {
		t.Fatalf("transient ceiling summary must recover on retry: %v", err)
	}
	if degradedFold(a) {
		t.Fatal("recovery installed a mechanical digest instead of the real summary")
	}
}

// A persistently broken endpoint still fails the turn — the retry is capped at
// one extra summarizer call, so latency does not grow without bound.
func TestCeilingSummaryFailureCapsRetryAtOne(t *testing.T) {
	sess := foldableSessionOverForce(6)
	always := &alwaysFailProvider{}
	a := agentOverForce(t, always, sess)

	big := bigFoldWord()
	for range 6 {
		sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	}

	before := always.calls.Load()
	err := prepareContext(context.Background(), a, CompactionTriggerPressure)
	if !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("persistent failure = %v, want ErrCompactionRequired", err)
	}
	if used := always.calls.Load() - before; used > 2 {
		t.Fatalf("retry used %d summarizer calls, want at most one extra", used)
	}
}

type alwaysFailProvider struct{ calls atomic.Int32 }

func (p *alwaysFailProvider) Name() string { return "always-fail" }
func (p *alwaysFailProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	return nil, errors.New("provider down")
}

func bigFoldWord() string {
	out := make([]byte, 0, 2000)
	for range 400 {
		out = append(out, "word "...)
	}
	return string(out)
}
