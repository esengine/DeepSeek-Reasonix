package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestWaitingStopsOnceBudgetExhausted(t *testing.T) {
	p := &transientHeaderProvider{}
	oldBudget, oldSleep := recoveryWaitBudget, recoverySleep
	defer func() { recoveryWaitBudget, recoverySleep = oldBudget, oldSleep }()
	recoveryWaitBudget = 3 * time.Minute
	var slept time.Duration
	recoverySleep = func(ctx context.Context, d time.Duration) bool {
		slept += d
		return ctx.Err() == nil
	}
	sink := &recordSink{}
	a := New(p, echoRegistry(), NewSession(""), Options{}, sink)
	err := a.Run(withNoClosedLoop(context.Background()), "go")
	var exhausted *provider.RecoveryWaitExhaustedError
	if !errors.As(err, &exhausted) {
		t.Fatalf("calls=%d err=%v", p.calls, err)
	}
	if exhausted.Phase != "headers" || exhausted.Status != 503 || exhausted.Attempts != 6 || p.calls != 6 {
		t.Fatalf("calls=%d exhausted=%+v", p.calls, exhausted)
	}
	if exhausted.Waited > recoveryWaitBudget || exhausted.Waited+time.Minute <= recoveryWaitBudget || slept > exhausted.Waited {
		t.Fatalf("waited=%s slept=%s budget=%s", exhausted.Waited, slept, recoveryWaitBudget)
	}
	if provider.ClassifyRecovery(err).Retryable {
		t.Fatal("exhausted wait classified as retryable")
	}
	waiting := 0
	for _, e := range sink.kinds(event.Retrying) {
		if e.Recovery == nil {
			continue
		}
		if !e.Recovery.Waiting {
			if e.Recovery.WaitBudgetMs != 0 {
				t.Fatalf("short retry advertised a wait budget: %+v", e.Recovery)
			}
			continue
		}
		waiting++
		if e.Recovery.WaitBudgetMs != recoveryWaitBudget.Milliseconds() || e.Recovery.NextAttemptAt == 0 || e.Recovery.WaitedMs < 0 {
			t.Fatalf("recovery=%+v", e.Recovery)
		}
	}
	if waiting != 2 {
		t.Fatalf("waiting retries=%d", waiting)
	}
}

func TestOversizedRetryAfterNeverStartsAnUnaffordableWait(t *testing.T) {
	p := &retryAfterProvider{after: time.Hour}
	old := recoverySleep
	defer func() { recoverySleep = old }()
	recoverySleep = func(context.Context, time.Duration) bool { return true }
	a := New(p, echoRegistry(), NewSession(""), Options{}, event.Discard)
	err := a.Run(withNoClosedLoop(context.Background()), "go")
	var exhausted *provider.RecoveryWaitExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Attempts != maxSamplingAttempts || p.calls != maxSamplingAttempts {
		t.Fatalf("calls=%d err=%v", p.calls, err)
	}
}

type retryAfterProvider struct {
	calls int
	after time.Duration
}

func (*retryAfterProvider) Name() string { return "retry-after" }
func (p *retryAfterProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	return nil, &provider.APIError{Status: 429, RetryAfter: p.after}
}

type cancelOnWaitSink struct {
	recordSink
	once   sync.Once
	cancel context.CancelFunc
}

func (s *cancelOnWaitSink) Emit(e event.Event) {
	s.recordSink.Emit(e)
	if e.Kind == event.Retrying && e.Recovery != nil && e.Recovery.Waiting {
		s.once.Do(s.cancel)
	}
}

func TestWaitingCancelsPromptlyWithRealTimer(t *testing.T) {
	p := &transientHeaderProvider{}
	old := recoverySleep
	defer func() { recoverySleep = old }()
	recoverySleep = sleepRecovery
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sink := &cancelOnWaitSink{cancel: cancel}
	a := New(p, echoRegistry(), NewSession(""), Options{}, sink)
	started := time.Now()
	err := a.Run(withNoClosedLoop(ctx), "go")
	if !errors.Is(err, context.Canceled) || p.calls != maxSamplingAttempts {
		t.Fatalf("calls=%d err=%v", p.calls, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancel during the minute-long wait took %s", elapsed)
	}
}
