package control

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/event"
)

func askProbeQuestions() []event.AskQuestion {
	return []event.AskQuestion{{
		ID: "q1", Header: "Fix", Prompt: "Which fix?",
		Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
	}}
}

type askProbeSink struct {
	mu      sync.Mutex
	asks    []event.Ask
	notices []event.Event
}

func (s *askProbeSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case e.Kind == event.AskRequest:
		s.asks = append(s.asks, e.Ask)
	case e.Kind == event.Notice && e.Code == event.NoticeCodePromptQueued:
		s.notices = append(s.notices, e)
	}
}

func (s *askProbeSink) counts() (asks, notices int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asks), len(s.notices)
}

func shortenPromptQueueNotice(t *testing.T) {
	t.Helper()
	old := promptQueueNoticeDelay
	promptQueueNoticeDelay = 40 * time.Millisecond
	t.Cleanup(func() { promptQueueNoticeDelay = old })
}

// A question waiting behind an earlier prompt used to be invisible in every
// direction: Ask took promptMu before registering, so no event was emitted, the
// prompt snapshot did not list it, and ReplayPendingPrompts could not recover
// it. The user saw a tool card that never opened a dialog while the turn
// blocked. It is now registered up front and announced.
func TestAskQueuedBehindAnotherPromptIsVisibleAndAnnounced(t *testing.T) {
	shortenPromptQueueNotice(t)
	sink := &askProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})

	// Stand in for an earlier prompt still awaiting the user.
	c.approval.promptMu.Lock()

	var returned atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = c.Ask(ctx, askProbeQuestions())
		returned.Store(true)
	}()

	deadline := time.After(2 * time.Second)
	for {
		if _, notices := sink.counts(); notices == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("a question queued behind another prompt never told the user why")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Registered, so it is visible to diagnostics rather than living only
	// inside a blocked goroutine.
	if got := c.approval.queuedAsks(); got != 1 {
		t.Fatalf("queued asks = %d, want the waiting question registered", got)
	}
	// Still not shown, and still not replayable: surfacing it here would put a
	// question on screen ahead of the prompt it is waiting behind.
	if asks, _ := sink.counts(); asks != 0 {
		t.Fatalf("AskRequest events = %d, want the question held until its turn", asks)
	}
	c.ReplayPendingPrompts()
	time.Sleep(30 * time.Millisecond)
	if asks, _ := sink.counts(); asks != 0 {
		t.Fatalf("replay surfaced %d queued ask(s) out of order", asks)
	}
	if _, pending := c.approval.snapshotPrompts(); len(pending) != 0 {
		t.Fatalf("snapshot listed %d queued ask(s); replay would show it early", len(pending))
	}

	// Once the earlier prompt clears, the question appears normally.
	c.approval.promptMu.Unlock()
	for {
		if asks, _ := sink.counts(); asks == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the queued question never appeared after the earlier prompt cleared")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if got := c.approval.queuedAsks(); got != 0 {
		t.Fatalf("queued asks = %d after emission, want 0", got)
	}
	// An emitted ask is replayable, which is how a tab switch rebuilds it.
	if _, pending := c.approval.snapshotPrompts(); len(pending) != 1 {
		t.Fatalf("snapshot listed %d shown ask(s), want 1 for replay", len(pending))
	}

	cancel()
	for !returned.Load() {
		time.Sleep(5 * time.Millisecond)
	}
}

// A prompt answered promptly must not produce a queue notice.
func TestPromptQueueNoticeStaysQuietWhenNothingWaits(t *testing.T) {
	shortenPromptQueueNotice(t)
	sink := &askProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})

	go func() { _, _ = c.Ask(t.Context(), askProbeQuestions()) }()

	deadline := time.After(2 * time.Second)
	for asks, _ := sink.counts(); asks != 1; asks, _ = sink.counts() {
		select {
		case <-deadline:
			t.Fatal("the uncontended ask never reached the frontend")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	time.Sleep(80 * time.Millisecond)
	if _, notices := sink.counts(); notices != 0 {
		t.Fatalf("queue notices = %d, want none when the prompt was never queued", notices)
	}
}

// Cancelling while queued must drop the registration and release the lock to
// the next prompt rather than leaking either.
func TestAskCancelledWhileQueuedLeavesNothingBehind(t *testing.T) {
	shortenPromptQueueNotice(t)
	sink := &askProbeSink{}
	c := New(Options{Sink: sink, SessionDir: t.TempDir()})
	c.approval.promptMu.Lock()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := c.Ask(ctx, askProbeQuestions())
		errc <- err
	}()

	deadline := time.After(2 * time.Second)
	for c.approval.queuedAsks() != 1 {
		select {
		case <-deadline:
			t.Fatal("the ask never registered while queued")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("cancelled Ask returned a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not unblock on cancellation while queued")
	}
	if got := c.approval.queuedAsks(); got != 0 {
		t.Fatalf("queued asks = %d after cancellation, want the registration dropped", got)
	}

	// The abandoned wait must not keep the lock from the next prompt.
	c.approval.promptMu.Unlock()
	acquired := false
	for range 200 {
		if c.approval.promptMu.TryLock() {
			acquired = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !acquired {
		t.Fatal("the prompt lock was leaked by the cancelled ask")
	}
	c.approval.promptMu.Unlock()
}

// Ask has no timeout of its own: approvalTimeout defaults to zero, so a
// question nobody answers blocks its turn until the user cancels.
func TestAskWithoutTimeoutBlocksUntilCancelled(t *testing.T) {
	c := New(Options{Sink: event.Discard, SessionDir: t.TempDir()})
	if c.approval.approvalTimeout != 0 {
		t.Skipf("approvalTimeout is %v; this test pins the unbounded default", c.approval.approvalTimeout)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := c.Ask(ctx, askProbeQuestions())
		errc <- err
	}()

	select {
	case err := <-errc:
		t.Fatalf("Ask returned %v without an answer; it is expected to block", err)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("cancelled Ask returned a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not unblock after cancellation")
	}
}
