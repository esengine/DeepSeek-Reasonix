package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
)

// resettingProvider fails the first resets summary requests the way a peer that
// went away mid-stream does, then answers.
type resettingProvider struct {
	mu     sync.Mutex
	resets int
	err    error
	calls  int
}

func (p *resettingProvider) Name() string { return "resetting" }

func (p *resettingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	failing := p.calls <= p.resets
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if failing {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: p.err}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: testutil.SummaryReply(req, "complete briefing")}
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 40, TotalTokens: 140, FinishReason: "stop"}}
	}
	close(ch)
	return ch, nil
}

func (p *resettingProvider) seen() int { p.mu.Lock(); defer p.mu.Unlock(); return p.calls }

func connectionReset() error {
	return provider.StreamInterrupt(errors.New("read stream: the peer closed the connection"), provider.StreamInterruptConnectionReset)
}

func withNoResetBackoff(t *testing.T) {
	t.Helper()
	was := summaryResetBackoff
	summaryResetBackoff = 0
	t.Cleanup(func() { summaryResetBackoff = was })
}

func TestASummaryCutByAConnectionResetIsRetriedOnce(t *testing.T) {
	withNoResetBackoff(t)
	p := &resettingProvider{resets: 1, err: connectionReset()}
	a := truncationAgent(t, p, 4*summaryOutputMaxTokens)

	if err := prepareContext(context.Background(), a, CompactionTriggerManual); err != nil {
		t.Fatalf("prepare = %v, want the retry after the reset to succeed", err)
	}
	if p.seen() != 2 {
		t.Fatalf("summary requests = %d, want the reset one and one retry", p.seen())
	}
	if got := latestDigest(a.sess.win.compactionState.Projection.Messages); !strings.Contains(got, "complete briefing") {
		t.Fatalf("digest = %q, want the retried summary installed", got)
	}
}

func TestTheConnectionResetRetryIsBoundedAndKeepsItsIdentity(t *testing.T) {
	withNoResetBackoff(t)
	p := &resettingProvider{resets: 10, err: connectionReset()}
	a := truncationAgent(t, p, 4*summaryOutputMaxTokens)

	err := prepareContext(context.Background(), a, CompactionTriggerManual)
	if !errors.Is(err, errSummaryRequestFailed) || compactionFailureCode(err) != FailSummaryFailed {
		t.Fatalf("prepare = %v, want the request-failed identity", err)
	}
	if provider.StreamInterruptReason(err) != provider.StreamInterruptConnectionReset {
		t.Fatalf("the failure lost the reset it carried: %v", err)
	}
	if p.seen() != 2 {
		t.Fatalf("summary requests = %d, want exactly one retry", p.seen())
	}
}

func TestAFailureThatIsNotAResetIsNotRetried(t *testing.T) {
	withNoResetBackoff(t)
	p := &resettingProvider{resets: 10, err: errors.New("the endpoint refused the request")}
	a := truncationAgent(t, p, 4*summaryOutputMaxTokens)

	if err := prepareContext(context.Background(), a, CompactionTriggerManual); !errors.Is(err, errSummaryRequestFailed) {
		t.Fatalf("prepare = %v, want the request-failed identity", err)
	}
	if p.seen() != 1 {
		t.Fatalf("summary requests = %d, want one: only a reset is worth asking again", p.seen())
	}
}

func TestCancellingDuringTheResetBackoffStopsTheRetry(t *testing.T) {
	was := summaryResetBackoff
	summaryResetBackoff = time.Hour
	t.Cleanup(func() { summaryResetBackoff = was })
	p := &resettingProvider{resets: 10, err: connectionReset()}
	a := truncationAgent(t, p, 4*summaryOutputMaxTokens)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	done := make(chan error, 1)
	go func() { done <- prepareContext(ctx, a, CompactionTriggerManual) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the retry kept waiting after the run was cancelled")
	}
	if p.seen() != 1 {
		t.Fatalf("summary requests = %d, want no retry once cancelled", p.seen())
	}
}
