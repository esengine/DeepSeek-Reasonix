package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

type pacedProvider struct {
	interval time.Duration
	chunks   int
	stall    bool
}

func (p *pacedProvider) Name() string { return "paced" }

func (p *pacedProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		for range p.chunks {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.interval):
			}
			select {
			case ch <- provider.Chunk{Type: provider.ChunkText, Text: "word "}:
			case <-ctx.Done():
				return
			}
		}
		if p.stall {
			<-ctx.Done()
		}
	}()
	return ch, nil
}

func summarizeWith(t *testing.T, prov provider.Provider, ctx context.Context, idle, ceiling time.Duration) (string, time.Duration, error) {
	t.Helper()
	prev := SummaryBounds
	SummaryBounds = SummaryLimits{Idle: idle, Ceiling: ceiling}
	t.Cleanup(func() { SummaryBounds = prev })
	a := New(prov, tool.NewRegistry(), &sessionstore.Session{}, Options{}, event.Discard)
	start := time.Now()
	s, _, err := a.window().summarize(ctx, []provider.Message{{Role: provider.RoleUser, Content: "x"}}, "")
	return s, time.Since(start), err
}

func TestSteadySlowSummaryOutlivesTheIdleBound(t *testing.T) {
	prov := &pacedProvider{interval: 40 * time.Millisecond, chunks: 30}
	s, took, err := summarizeWith(t, prov, context.Background(), 500*time.Millisecond, 30*time.Second)
	if err != nil {
		t.Fatalf("steady stream failed after %v: %v", took, err)
	}
	if took < time.Second {
		t.Fatalf("stream finished in %v; the case must outlast the idle bound", took)
	}
	if got := strings.Count(s, "word"); got != 30 {
		t.Fatalf("summary has %d words, want 30", got)
	}
}

func TestStalledSummaryFailsAtTheIdleBound(t *testing.T) {
	prov := &pacedProvider{interval: 20 * time.Millisecond, chunks: 3, stall: true}
	_, took, err := summarizeWith(t, prov, context.Background(), 150*time.Millisecond, 5*time.Second)
	if !errors.Is(err, errSummaryTimeout) || errors.Is(err, errSummaryCeiling) {
		t.Fatalf("err = %v, want the stall class", err)
	}
	if got := compactionFailureCode(err); got != FailSummaryTimeout {
		t.Fatalf("code = %q, want %q", got, FailSummaryTimeout)
	}
	if took < 150*time.Millisecond || took > 2*time.Second {
		t.Fatalf("stall surfaced after %v, want about the idle bound after the last chunk", took)
	}
}

func TestSilenceBeforeTheFirstChunkIsAStall(t *testing.T) {
	_, _, err := summarizeWith(t, &fakeProvider{hang: true}, context.Background(), 80*time.Millisecond, 5*time.Second)
	if got := compactionFailureCode(err); got != FailSummaryTimeout {
		t.Fatalf("code = %q (err %v), want %q", got, err, FailSummaryTimeout)
	}
}

func TestTrickleIsStoppedByTheCeiling(t *testing.T) {
	prov := &pacedProvider{interval: 20 * time.Millisecond, chunks: 1 << 20}
	guard, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_, took, err := summarizeWith(t, prov, guard, time.Second, 200*time.Millisecond)
	if !errors.Is(err, errSummaryCeiling) || errors.Is(err, errSummaryTimeout) {
		t.Fatalf("err = %v, want the ceiling class", err)
	}
	if got := compactionFailureCode(err); got != FailSummaryCeiling {
		t.Fatalf("code = %q, want %q", got, FailSummaryCeiling)
	}
	if took > 5*time.Second {
		t.Fatalf("ceiling fired after %v", took)
	}
}

func TestCancellingASlowSummaryIsImmediateAndNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	prov := &pacedProvider{interval: 20 * time.Millisecond, chunks: 1 << 20}
	_, took, err := summarizeWith(t, prov, ctx, time.Minute, time.Hour)
	if got := compactionFailureCode(err); got != FailCancelled {
		t.Fatalf("code = %q (err %v), want %q", got, err, FailCancelled)
	}
	if took > time.Second {
		t.Fatalf("cancel took %v", took)
	}
}

func TestCeilingFailureIsTransientLikeAStall(t *testing.T) {
	if !transientSummaryFailure(string(FailSummaryCeiling)) {
		t.Fatal("a ceiling failure must release once the input has grown, like a stall")
	}
}

type deadlineProbe struct{ remaining time.Duration }

func (p *deadlineProbe) Name() string { return "deadline-probe" }

func (p *deadlineProbe) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	if d, ok := ctx.Deadline(); ok {
		p.remaining = time.Until(d)
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Digest\ndigest"}
	close(ch)
	return ch, nil
}

// The request carries only the overall ceiling as a deadline, so a steady slow
// stream is never cut by a total-time bound shorter than it.
func TestSummaryRequestDeadlineIsTheCeilingNotAShortTotalBound(t *testing.T) {
	prov := &deadlineProbe{}
	a := New(prov, tool.NewRegistry(), &sessionstore.Session{}, Options{}, event.Discard)
	if _, _, err := a.window().summarize(context.Background(), []provider.Message{{Role: provider.RoleUser, Content: "x"}}, ""); err != nil {
		t.Fatal(err)
	}
	if prov.remaining < SummaryCeiling-time.Minute || prov.remaining > SummaryCeiling {
		t.Fatalf("request deadline is %v away, want about the %v ceiling", prov.remaining, SummaryCeiling)
	}
	if SummaryIdleTimeout <= provider.StreamIdleTimeout {
		t.Fatalf("idle bound %v must sit past the provider watchdog %v", SummaryIdleTimeout, provider.StreamIdleTimeout)
	}
}
