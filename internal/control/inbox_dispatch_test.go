package control

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

type inboxDispatchRunner struct {
	inputs chan string
}

func (r *inboxDispatchRunner) Run(_ context.Context, input string) error {
	r.inputs <- input
	return nil
}

func newInboxDispatchController(t *testing.T) (*Controller, *inboxDispatchRunner, <-chan struct{}) {
	t.Helper()
	dir := t.TempDir()
	runner := &inboxDispatchRunner{inputs: make(chan string, 8)}
	done := make(chan struct{}, 8)
	c := New(Options{
		Runner: runner,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				done <- struct{}{}
			}
		}),
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "session.jsonl"),
	})
	t.Cleanup(func() {
		c.Close()
		c.autosaveWG.Wait()
	})
	return c, runner, done
}

func waitForInboxDispatch(t *testing.T, runner *inboxDispatchRunner) string {
	t.Helper()
	select {
	case input := <-runner.inputs:
		return input
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbox dispatch")
		return ""
	}
}

func waitForInboxTurnDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbox turn completion")
	}
}

func TestEndRotationDispatchesQueuedInboxItem(t *testing.T) {
	c, runner, done := newInboxDispatchController(t)
	if err := c.beginRotation(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TryEnqueueFollowup(InboxRequest{
		Intent: sessioninbox.IntentFollowup,
		Submit: "queued during rotation",
	}); err != nil {
		t.Fatal(err)
	}
	c.endRotation()

	if got := waitForInboxDispatch(t, runner); got != "queued during rotation" {
		t.Fatalf("dispatched input = %q", got)
	}
	waitForInboxTurnDone(t, done)
}

func TestRejectedIdleSteerDispatchesAsFollowup(t *testing.T) {
	c, runner, done := newInboxDispatchController(t)
	rec, err := c.EnqueueInbox(InboxRequest{
		Intent: sessioninbox.IntentSteer,
		Submit: "late steer becomes follow-up",
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := c.TrySteerInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != sessioninbox.DispositionQueuedFollowup {
		t.Fatalf("disposition = %q", receipt.Disposition)
	}

	if got := waitForInboxDispatch(t, runner); got != "late steer becomes follow-up" {
		t.Fatalf("dispatched input = %q", got)
	}
	waitForInboxTurnDone(t, done)
}

func TestInboxDispatchKickDuringEmptyScanIsNotLost(t *testing.T) {
	c, runner, done := newInboxDispatchController(t)
	scanReached := make(chan struct{})
	releaseScan := make(chan struct{})
	var once sync.Once
	c.inbox.mu.Lock()
	c.inbox.afterDispatchScan = func(found bool) {
		if found {
			return
		}
		once.Do(func() {
			close(scanReached)
			<-releaseScan
		})
	}
	c.inbox.mu.Unlock()

	dispatchReturned := make(chan struct{})
	go func() {
		c.maybeDispatchInbox()
		close(dispatchReturned)
	}()
	select {
	case <-scanReached:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not reach empty scan")
	}
	if _, err := c.EnqueueInbox(InboxRequest{Submit: "arrived during empty scan"}); err != nil {
		t.Fatal(err)
	}
	// This kick lands while the first dispatcher still owns the handoff. The
	// pending level must make that dispatcher scan again before it exits.
	c.maybeDispatchInbox()
	close(releaseScan)

	select {
	case <-dispatchReturned:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not return")
	}
	if got := waitForInboxDispatch(t, runner); got != "arrived during empty scan" {
		t.Fatalf("dispatched input = %q", got)
	}
	waitForInboxTurnDone(t, done)
}

func TestInboxDispatchRetriesTransientOwnerFailure(t *testing.T) {
	c, runner, done := newInboxDispatchController(t)
	retryReady := make(chan func(), 1)
	failedOnce := false
	c.inbox.mu.Lock()
	c.inbox.beforeDispatchSubmit = func(string) error {
		if failedOnce {
			return nil
		}
		failedOnce = true
		return errors.New("temporary dispatch failure")
	}
	c.inbox.scheduleDispatchRetry = func(_ time.Duration, retry func()) {
		retryReady <- retry
	}
	c.inbox.mu.Unlock()
	if _, err := c.EnqueueInbox(InboxRequest{Submit: "retry me"}); err != nil {
		t.Fatal(err)
	}
	c.maybeDispatchInbox()

	var retry func()
	select {
	case retry = <-retryReady:
	case <-time.After(time.Second):
		t.Fatal("transient failure did not schedule a retry")
	}
	select {
	case got := <-runner.inputs:
		t.Fatalf("item dispatched before scheduled retry: %q", got)
	default:
	}
	retry()
	if got := waitForInboxDispatch(t, runner); got != "retry me" {
		t.Fatalf("retried input = %q", got)
	}
	waitForInboxTurnDone(t, done)
}

type gatedInboxDispatchRunner struct {
	inputs       chan string
	firstStarted chan struct{}
	releaseFirst chan struct{}
	once         sync.Once
}

func (r *gatedInboxDispatchRunner) Run(ctx context.Context, input string) error {
	r.inputs <- input
	blocked := false
	r.once.Do(func() {
		blocked = true
		close(r.firstStarted)
	})
	if !blocked {
		return nil
	}
	select {
	case <-r.releaseFirst:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestNaturalCompletionAutoDispatchesDurableFIFO(t *testing.T) {
	dir := t.TempDir()
	runner := &gatedInboxDispatchRunner{
		inputs:       make(chan string, 8),
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	done := make(chan struct{}, 8)
	c := New(Options{
		Runner: runner,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				done <- struct{}{}
			}
		}),
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "session.jsonl"),
	})
	t.Cleanup(func() {
		c.Close()
		c.autosaveWG.Wait()
	})

	c.Submit("active turn")
	select {
	case <-runner.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("active turn did not start")
	}
	if got := <-runner.inputs; got != "active turn" {
		t.Fatalf("initial input = %q", got)
	}
	for _, input := range []string{"queued one", "queued two"} {
		if _, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: input}); err != nil {
			t.Fatal(err)
		}
	}
	close(runner.releaseFirst)
	waitForInboxTurnDone(t, done)
	for _, want := range []string{"queued one", "queued two"} {
		if got := waitForInboxDispatch(t, &inboxDispatchRunner{inputs: runner.inputs}); got != want {
			t.Fatalf("FIFO input = %q, want %q", got, want)
		}
		waitForInboxTurnDone(t, done)
	}
	if snap := c.InboxSnapshot(); len(snap.Items) != 0 || snap.Paused {
		t.Fatalf("completed FIFO left inbox state: %+v", snap)
	}
}
