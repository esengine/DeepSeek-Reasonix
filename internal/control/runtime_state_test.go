package control

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

type runtimeStateTestSink struct {
	event.Sink
	states chan event.RuntimeStateSnapshot
}

func (s *runtimeStateTestSink) RuntimeStateChanged(state event.RuntimeStateSnapshot) {
	s.states <- state
}

func runtimeStateAwait(t *testing.T, states <-chan event.RuntimeStateSnapshot, accept func(event.RuntimeStateSnapshot) bool) event.RuntimeStateSnapshot {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var last event.RuntimeStateSnapshot
	for {
		select {
		case last = <-states:
			if accept(last) {
				return last
			}
		case <-timer.C:
			t.Fatalf("runtime publication did not reach expected state; last=%+v", last)
		}
	}
}

func runtimeStateSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(description)
	}
}

func assertRuntimeStateSameVersion(t *testing.T, published, current event.RuntimeStateSnapshot) {
	t.Helper()
	if published.RuntimeEpoch == current.RuntimeEpoch && published.Revision == current.Revision && published != current {
		t.Fatalf("same runtime version has different contents: published=%+v current=%+v", published, current)
	}
}

func TestRuntimeStateSnapshotFinishingAndFinalPublication(t *testing.T) {
	isolateControlConfigHome(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	releaseDone := sync.OnceFunc(func() { close(release) })
	sink := &runtimeStateTestSink{Sink: holdFinishingWindow(release, entered, nil), states: make(chan event.RuntimeStateSnapshot, 32)}
	c := New(Options{SessionDir: t.TempDir(), Sink: sink})
	defer c.Close()
	defer releaseDone()
	initial := c.RuntimeStateSnapshot()
	if initial.SchemaVersion != 1 || initial.RuntimeEpoch == "" || initial.Revision == 0 || initial.Phase != "idle" {
		t.Fatalf("invalid initial contract: %+v", initial)
	}
	if second := c.RuntimeStateSnapshot(); second != initial {
		t.Fatalf("reading runtime state changed the snapshot: first=%+v second=%+v", initial, second)
	}
	runRelease := make(chan struct{})
	releaseBody := sync.OnceFunc(func() { close(runRelease) })
	defer releaseBody()
	c.runGuarded(func(context.Context) error { <-runRelease; return nil })
	executing := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "executing" && s.TurnID != "" })
	if !executing.Running || !executing.Cancellable || executing.Activity != "thinking" || executing.Revision <= initial.Revision {
		t.Fatalf("invalid execution state: %+v", executing)
	}
	releaseBody()
	runtimeStateSignal(t, entered, "TurnDone did not enter finishing window")
	finishing := c.RuntimeStateSnapshot()
	if finishing.Phase != "finishing" || !finishing.Running || finishing.Activity != "" || finishing.Cancellable {
		t.Fatalf("finishing must keep admission closed without presenting thought/cancel: %+v", finishing)
	}
	if !c.RuntimeStatus().Running {
		t.Fatal("legacy running guard opened during TurnDone fan-out")
	}
	publishedFinishing := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "finishing" })
	assertRuntimeStateSameVersion(t, publishedFinishing, c.RuntimeStateSnapshot())
	releaseDone()
	completed := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "idle" && s.Revision > executing.Revision })
	if completed.Running || completed.Cancellable || completed.PendingPrompt || completed.Activity != "" || completed.BackgroundJobs != 0 {
		t.Fatalf("completed publication retained active work: %+v", completed)
	}
	if completed.RuntimeEpoch != initial.RuntimeEpoch || completed.Revision <= publishedFinishing.Revision {
		t.Fatalf("completion lost instance identity or ordering: finishing=%+v completed=%+v", publishedFinishing, completed)
	}
	assertRuntimeStateSameVersion(t, completed, c.RuntimeStateSnapshot())
}

func TestRuntimeStateQueuedTurnNeverPublishesPreviousIdleOverNext(t *testing.T) {
	isolateControlConfigHome(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	releaseDone := sync.OnceFunc(func() { close(release) })
	sink := &runtimeStateTestSink{Sink: holdFinishingWindow(release, entered, nil), states: make(chan event.RuntimeStateSnapshot, 64)}
	c := New(Options{SessionDir: t.TempDir(), Sink: sink})
	defer c.Close()
	defer releaseDone()
	c.runGuarded(func(context.Context) error { return nil })
	runtimeStateSignal(t, entered, "first turn did not enter finishing window")
	first := c.RuntimeStateSnapshot()
	secondStarted, secondRelease := make(chan struct{}), make(chan struct{})
	releaseSecond := sync.OnceFunc(func() { close(secondRelease) })
	defer releaseSecond()
	if admission := c.runGuarded(func(context.Context) error { close(secondStarted); <-secondRelease; return nil }); admission != turnParked {
		t.Fatalf("follow-up admission=%v; expected parked", admission)
	}
	releaseDone()
	runtimeStateSignal(t, secondStarted, "parked second turn did not start")
	second := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool {
		if s.Revision > first.Revision && s.Phase == "idle" {
			t.Fatalf("old completion published idle while queued turn owns admission: %+v", s)
		}
		return s.Revision > first.Revision && s.Phase == "executing" && s.TurnID != "" && s.TurnID != first.TurnID
	})
	if second.RuntimeEpoch != first.RuntimeEpoch || second.Revision <= first.Revision {
		t.Fatalf("queued turn lost runtime ordering: first=%+v second=%+v", first, second)
	}
	assertRuntimeStateSameVersion(t, second, c.RuntimeStateSnapshot())
	releaseSecond()
	final := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "idle" && s.Revision > second.Revision })
	if final.TurnID != second.TurnID && final.TurnID != "" {
		t.Fatalf("old turn replaced the completed next turn: second=%+v final=%+v", second, final)
	}
}

func TestRuntimeStatePromptCancellationAndClosed(t *testing.T) {
	for _, kind := range []string{"ask", "approval"} {
		t.Run(kind, func(t *testing.T) {
			isolateControlConfigHome(t)
			requests := make(chan struct{}, 1)
			sink := &runtimeStateTestSink{Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.AskRequest || e.Kind == event.ApprovalRequest {
					requests <- struct{}{}
				}
			}), states: make(chan event.RuntimeStateSnapshot, 64)}
			c := New(Options{SessionDir: t.TempDir(), Sink: sink})
			defer c.Close()
			if kind == "ask" {
				c.runner = &askBlockingRunner{c: c}
			} else {
				c.runner = &approvalBlockingRunner{c: c}
			}
			c.Send("isolated interaction")
			runtimeStateSignal(t, requests, "prompt was not requested")
			pending := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.PendingPrompt })
			if !pending.Running || !pending.Cancellable {
				t.Fatalf("pending prompt is not actionable: %+v", pending)
			}
			c.Cancel()
			c.Cancel()
			idle := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "idle" && s.Revision > pending.Revision })
			if idle.PendingPrompt || idle.CancelRequested || idle.Cancellable || idle.Running {
				t.Fatalf("cancel left stale prompt state: %+v", idle)
			}
			c.Close()
			closed := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "closed" })
			if closed.Running || closed.PendingPrompt || closed.Cancellable || closed.Activity != "" || closed.Revision <= idle.Revision {
				t.Fatalf("invalid closed snapshot: %+v", closed)
			}
			assertRuntimeStateSameVersion(t, closed, c.RuntimeStateSnapshot())
		})
	}
}

func TestRuntimeStateCloseDuringFinishingCannotResurrectActivity(t *testing.T) {
	isolateControlConfigHome(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	releaseDone := sync.OnceFunc(func() { close(release) })
	sink := &runtimeStateTestSink{Sink: holdFinishingWindow(release, entered, nil), states: make(chan event.RuntimeStateSnapshot, 32)}
	c := New(Options{SessionDir: t.TempDir(), Sink: sink})
	defer c.Close()
	defer releaseDone()
	c.runGuarded(func(context.Context) error { return nil })
	runtimeStateSignal(t, entered, "turn did not enter finishing window")
	c.Close()
	closed := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "closed" })
	if closed.Running || closed.Cancellable || closed.PendingPrompt || closed.Activity != "" {
		t.Fatalf("closed runtime retained finishing activity: %+v", closed)
	}
	releaseDone()
	// A rejected new admission is a synchronous observation after Close. The
	// released old completion may still run, but it must never reopen the gate.
	if got := c.runGuarded(func(context.Context) error { t.Error("closed runtime admitted a new turn"); return nil }); got != turnDroppedClosed {
		t.Fatalf("admission after close=%v, want closed", got)
	}
	current := c.RuntimeStateSnapshot()
	if current.Phase != "closed" || current.Running || current.Activity != "" {
		t.Fatalf("old completion resurrected closed runtime: %+v", current)
	}
	assertRuntimeStateSameVersion(t, closed, current)
}

func TestRuntimeStateStreamingActivityClearsOnCompletion(t *testing.T) {
	isolateControlConfigHome(t)
	sink := &runtimeStateTestSink{Sink: event.Discard, states: make(chan event.RuntimeStateSnapshot, 32)}
	c := New(Options{SessionDir: t.TempDir(), Sink: sink})
	defer c.Close()
	release := make(chan struct{})
	releaseBody := sync.OnceFunc(func() { close(release) })
	defer releaseBody()
	c.runGuarded(func(context.Context) error {
		c.sink.Emit(event.Event{Kind: event.Text, Text: "isolated visible output"})
		<-release
		return nil
	})
	streaming := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Activity == "streaming" })
	if streaming.Phase != "executing" || !streaming.Running {
		t.Fatalf("streaming state is not executing: %+v", streaming)
	}
	releaseBody()
	idle := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool { return s.Phase == "idle" && s.Revision > streaming.Revision })
	if idle.Activity != "" || idle.Running {
		t.Fatalf("completed runtime retained streaming activity: %+v", idle)
	}
	assertRuntimeStateSameVersion(t, idle, c.RuntimeStateSnapshot())
}

type runtimeStateSlowTestSink struct {
	entered chan struct{}
	release chan struct{}
	done    chan struct{}
	states  chan event.RuntimeStateSnapshot
	once    sync.Once
}

func (s *runtimeStateSlowTestSink) Emit(e event.Event) {
	if e.Kind == event.TurnDone {
		s.done <- struct{}{}
	}
}
func (s *runtimeStateSlowTestSink) RuntimeStateChanged(state event.RuntimeStateSnapshot) {
	s.once.Do(func() { close(s.entered); <-s.release })
	s.states <- state
}

func TestRuntimeStateSlowObserverDoesNotBlockTurnAndRetainsFinalSnapshot(t *testing.T) {
	isolateControlConfigHome(t)
	sink := &runtimeStateSlowTestSink{entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{}, 1), states: make(chan event.RuntimeStateSnapshot, 32)}
	releaseObserver := sync.OnceFunc(func() { close(sink.release) })
	c := New(Options{SessionDir: t.TempDir(), Sink: sink})
	defer c.Close()
	defer releaseObserver()
	bodyRelease := make(chan struct{})
	releaseBody := sync.OnceFunc(func() { close(bodyRelease) })
	defer releaseBody()
	c.runGuarded(func(context.Context) error { <-bodyRelease; return nil })
	runtimeStateSignal(t, sink.entered, "runtime observer did not enter")
	releaseBody()
	runtimeStateSignal(t, sink.done, "slow runtime observer blocked turn completion")
	releaseObserver()
	final := runtimeStateAwait(t, sink.states, func(state event.RuntimeStateSnapshot) bool {
		return state.Phase == "idle" && state.TurnStatus == event.TurnCompleted
	})
	if final.Running || final.Activity != "" {
		t.Fatalf("slow observer lost final idle: %+v", final)
	}
	assertRuntimeStateSameVersion(t, final, c.RuntimeStateSnapshot())
}

type runtimeStateFailureRunner func(context.Context, string) error

func (r runtimeStateFailureRunner) Run(ctx context.Context, input string) error {
	return r(ctx, input)
}

func TestRuntimeStateRunnerFailuresPublishIdleAndPreserveFailure(t *testing.T) {
	for _, failure := range []string{"error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			isolateControlConfigHome(t)
			started, release := make(chan struct{}), make(chan struct{})
			releaseRunner := sync.OnceFunc(func() { close(release) })
			done := make(chan event.Event, 2)
			sink := &runtimeStateTestSink{states: make(chan event.RuntimeStateSnapshot, 64), Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.TurnDone {
					done <- e
				}
			})}
			message := "runtime-state " + failure + " fixture"
			runner := runtimeStateFailureRunner(func(context.Context, string) error {
				close(started)
				<-release
				if failure == "panic" {
					panic(message)
				}
				return errors.New(message)
			})
			c := New(Options{SessionDir: t.TempDir(), Sink: sink, Runner: runner})
			defer c.Close()
			defer releaseRunner()
			c.Send("exercise isolated runner failure")
			runtimeStateSignal(t, started, "runner did not start")
			executing := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool {
				return s.Phase == "executing" && s.Running && s.Cancellable && s.TurnID != ""
			})
			releaseRunner()
			idle := runtimeStateAwait(t, sink.states, func(s event.RuntimeStateSnapshot) bool {
				return s.Phase == "idle" && s.Revision > executing.Revision
			})
			if idle.Running || idle.PendingPrompt || idle.CancelRequested || idle.Cancellable || idle.Activity != "" || idle.BackgroundJobs != 0 {
				t.Fatalf("%s retained active state in final publication: %+v", failure, idle)
			}
			if idle.RuntimeEpoch != executing.RuntimeEpoch || idle.TurnStatus != event.TurnFailed {
				t.Fatalf("%s lost failure identity/status: executing=%+v idle=%+v", failure, executing, idle)
			}
			terminal := waitTurnDoneEvent(t, done)
			if terminal.Err == nil || !strings.Contains(terminal.Err.Error(), message) || terminal.Status != event.TurnFailed || terminal.Cancelled {
				t.Fatalf("%s failure notification was lost or reclassified: %+v", failure, terminal)
			}
			// The final idle publication follows synchronous TurnDone delivery.
			// Therefore another queued terminal notification would be a duplicate.
			select {
			case extra := <-done:
				t.Fatalf("%s emitted duplicate terminal notification: %+v", failure, extra)
			default:
			}
			assertRuntimeStateSameVersion(t, idle, c.RuntimeStateSnapshot())
		})
	}
}
