package jobs

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func receiveRuntimeState(t *testing.T, states <-chan RuntimeState) RuntimeState {
	t.Helper()
	select {
	case state := <-states:
		return state
	case <-time.After(5 * time.Second):
		t.Fatal("runtime subscriber did not receive a committed state")
		return RuntimeState{}
	}
}

// Completion notices are emitted after the drain note is queued, but before
// the job's lifetime ends. Runtime subscribers must observe a separate settled
// lifecycle notification; treating this notice as idle would release guards
// while the job is still unwinding.
func TestRuntimeStateCompletionNoticePrecedesJobExit(t *testing.T) {
	sink := &blockingFinishedSink{entered: make(chan struct{}), released: make(chan struct{})}
	m := NewManager(sink)
	defer m.Close()
	m.SetActiveSessionPath("runtime-session", filepath.Join(t.TempDir(), "session.jsonl"))
	job := m.StartForSession("runtime-session", "bash", "completion boundary", func(context.Context, io.Writer) (string, error) {
		return "isolated result", nil
	})
	released := false
	defer func() {
		if !released {
			close(sink.released)
		}
	}()
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("completion notice was not delivered")
	}
	select {
	case <-job.done:
		t.Fatal("job lifetime ended before completion bookkeeping returned")
	default:
	}
	if got := m.RunningForSession("runtime-session"); len(got) != 1 || got[0].ID != job.ID {
		t.Fatalf("notice must not prematurely release runtime protection: %+v", got)
	}
	if note := m.DrainCompletedNoteForSession("runtime-session"); note == "" {
		t.Fatal("completion notice became visible before its drain note")
	}
	close(sink.released)
	released = true
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("job failed to unwind after notice delivery")
	}
	if got := m.RunningForSession("runtime-session"); len(got) != 0 {
		t.Fatalf("completed job remains running: %+v", got)
	}
}

func TestRuntimeStateCompletionPublishedAfterJobExit(t *testing.T) {
	sink := &blockingFinishedSink{entered: make(chan struct{}), released: make(chan struct{})}
	m := NewManager(sink)
	defer m.Close()
	m.SetActiveSessionPath("runtime-session", filepath.Join(t.TempDir(), "session.jsonl"))
	states := make(chan RuntimeState, 4)
	initial, unsubscribe := m.SubscribeRuntime("runtime-session", func(state RuntimeState) { states <- state })
	defer unsubscribe()
	if initial.Running != 0 || initial.SessionID != "runtime-session" {
		t.Fatalf("unexpected initial snapshot: %+v", initial)
	}
	runRelease := make(chan struct{})
	job := m.StartForSession("runtime-session", "bash", "settled notification", func(context.Context, io.Writer) (string, error) {
		<-runRelease
		return "done", nil
	})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(sink.released) })
	started := receiveRuntimeState(t, states)
	close(runRelease)
	if started.Running != 1 || started.JobID != job.ID || started.Revision <= initial.Revision {
		t.Fatalf("invalid started snapshot: initial=%+v started=%+v", initial, started)
	}
	select {
	case <-sink.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("completion notice was not delivered")
	}
	select {
	case state := <-states:
		t.Fatalf("completion published before job exit: %+v", state)
	default:
	}
	releaseOnce.Do(func() { close(sink.released) })
	completed := receiveRuntimeState(t, states)
	if completed.Running != 0 || completed.JobID != job.ID || completed.Revision <= started.Revision {
		t.Fatalf("invalid completion snapshot: started=%+v completed=%+v", started, completed)
	}
	select {
	case <-job.done:
	default:
		t.Fatal("idle snapshot was published before closing the job lifetime")
	}
	if got := m.RunningForSession("runtime-session"); len(got) != 0 {
		t.Fatalf("published idle disagrees with running query: %+v", got)
	}
}

func TestRuntimeStateCancelledJobRetainsProtectionUntilExit(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	states := make(chan RuntimeState, 4)
	_, unsubscribe := m.SubscribeRuntime("runtime-session", func(state RuntimeState) { states <- state })
	defer unsubscribe()
	cancelled := make(chan struct{})
	runRelease := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(runRelease) })
	job := m.StartForSession("runtime-session", "bash", "cancel unwind", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		close(cancelled)
		<-runRelease
		return "", ctx.Err()
	})
	started := receiveRuntimeState(t, states)
	if !m.KillForSession("runtime-session", job.ID) {
		t.Fatal("cancel request was rejected")
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not receive cancellation")
	}
	current, stopProbe := m.SubscribeRuntime("runtime-session", func(RuntimeState) {})
	stopProbe()
	if started.Running != 1 || current.Running != 1 {
		t.Fatalf("cancellation released protection before exit: started=%+v current=%+v", started, current)
	}
	releaseOnce.Do(func() { close(runRelease) })
	for {
		completed := receiveRuntimeState(t, states)
		if completed.Running != 0 {
			continue
		}
		select {
		case <-job.done:
		default:
			t.Fatal("cancelled job published idle before exiting")
		}
		if completed.Revision <= started.Revision {
			t.Fatalf("completion revision did not advance: %+v", completed)
		}
		break
	}
}

func TestRuntimeStatePublishesForNonActiveSession(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	m.SetActiveSession("visible-session")
	states := make(chan RuntimeState, 4)
	_, unsubscribe := m.SubscribeRuntime("background-session", func(state RuntimeState) { states <- state })
	defer unsubscribe()
	runRelease := make(chan struct{})
	job := m.StartForSession("background-session", "bash", "hidden session", func(context.Context, io.Writer) (string, error) {
		<-runRelease
		return "done", nil
	})
	started := receiveRuntimeState(t, states)
	close(runRelease)
	completed := receiveRuntimeState(t, states)
	if started.SessionID != "background-session" || started.Running != 1 || started.JobID != job.ID {
		t.Fatalf("non-active start was misrouted: %+v", started)
	}
	if completed.SessionID != "background-session" || completed.Running != 0 || completed.JobID != job.ID || completed.Revision <= started.Revision {
		t.Fatalf("non-active completion was missing or misrouted: %+v", completed)
	}
}

func TestRuntimeStateUnsubscribeDiscardsPendingCallbacks(t *testing.T) {
	m := NewManager(nil)
	defer m.Close()
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	_, unsubscribe := m.SubscribeRuntime("runtime-session", func(RuntimeState) {
		if count.Add(1) == 1 {
			close(entered)
			<-release
			close(returned)
		}
	})
	defer unsubscribe()
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	job := m.StartForSession("runtime-session", "bash", "unsubscribed job", func(context.Context, io.Writer) (string, error) { return "done", nil })
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("subscriber did not enter")
	}
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("slow subscriber blocked job completion")
	}
	m.runtimeObservers.mu.Lock()
	var subscription *runtimeSubscription
	for _, candidate := range m.runtimeObservers.listeners {
		subscription = candidate
	}
	m.runtimeObservers.mu.Unlock()
	unsubscribe()
	releaseOnce.Do(func() { close(release) })
	<-returned
	// Wait for the already-entered callback to leave the dispatcher. This is
	// an observation barrier, not a delay used to infer no future callbacks.
	waitFor(t, func() bool {
		subscription.mu.Lock()
		defer subscription.mu.Unlock()
		return !subscription.draining
	})
	if got := count.Load(); got != 1 {
		t.Fatalf("unsubscribe allowed %d callbacks; only the entered callback may finish", got)
	}
}
