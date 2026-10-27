package jobs

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
)

func TestJobFinalizationCoversInvokedAndUninvoked(t *testing.T) {
	runErr := errors.New("run refused")
	for _, mode := range []string{"success", "error", "panic", "closed", "invalid_session", "invalid_kind"} {
		t.Run(mode, func(t *testing.T) {
			m := NewManager(event.Discard)
			defer m.Close()
			session, kind, invoked, wantStatus := "session", "task", true, Done
			switch mode {
			case "closed":
				m.Close()
				invoked, wantStatus = false, Interrupted
			case "invalid_session":
				session, invoked, wantStatus = "invalid/session", false, Failed
			case "invalid_kind":
				kind, invoked, wantStatus = "", false, Failed
			case "error", "panic":
				wantStatus = Failed
			}
			var runs, cleanups atomic.Int32
			var current *Job
			states := make(chan Finalization, 1)
			j := m.StartForSession(session, kind, "fixture", func(context.Context, io.Writer) (string, error) {
				runs.Add(1)
				if mode == "panic" {
					panic("run panic")
				}
				if mode == "error" {
					return "", runErr
				}
				return "done", nil
			}, func(j *Job) { current = j }, WithFinalizer(func(state Finalization) error {
				cleanups.Add(1)
				if !m.mu.TryLock() {
					t.Error("Manager mutex covers host finalization")
				} else {
					m.mu.Unlock()
					_ = m.Running()
				}
				if !current.mu.TryLock() {
					t.Error("Job mutex covers host finalization")
				} else {
					current.mu.Unlock()
				}
				states <- state
				return nil
			}))
			<-j.done
			select {
			case state := <-states:
				if state.Invoked != invoked || (mode == "success") != (state.Err == nil) {
					t.Errorf("finalization state = %+v; invoked=%v", state, invoked)
				}
				if mode == "error" && !errors.Is(state.Err, runErr) {
					t.Errorf("original run error lost: %v", state.Err)
				}
			default:
				t.Error("job completion became visible before host cleanup")
			}
			if err := j.finalize(Finalization{Invoked: !invoked}); err != nil {
				t.Fatal(err)
			}
			if cleanups.Load() != 1 || (runs.Load() == 1) != invoked {
				t.Errorf("runs=%d cleanups=%d", runs.Load(), cleanups.Load())
			}
			j.mu.Lock()
			status := j.status
			j.mu.Unlock()
			if status != wantStatus {
				t.Errorf("status=%s want %s", status, wantStatus)
			}
		})
	}
}

type finalizationRecorder struct {
	done chan error
}

func (*finalizationRecorder) RecordStart(string, string, string) {}
func (r *finalizationRecorder) RecordDone(_ string, _ Status, err error) {
	r.done <- err
}

func TestJobFinalizationFailuresAttemptAllCallbacksAndKeepCauses(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(map[bool]string{false: "invoked", true: "uninvoked"}[closed], func(t *testing.T) {
			m := NewManager(event.Discard)
			defer m.Close()
			if closed {
				m.Close()
			}
			recorder := &finalizationRecorder{done: make(chan error, 1)}
			m.SetTaskRecorder(recorder)
			runErr, cleanupErr := errors.New("run failure"), errors.New("cleanup failure")
			var last atomic.Int32
			j := m.StartForSession("session", "task", "failure", func(context.Context, io.Writer) (string, error) {
				return "", runErr
			}, WithFinalizer(func(Finalization) error { return cleanupErr }),
				WithFinalizer(func(Finalization) error { panic("cleanup panic") }),
				WithFinalizer(func(Finalization) error { last.Add(1); return nil }))
			<-j.done
			err := <-recorder.done
			cause := runErr
			if closed {
				cause = context.Canceled
			}
			if !errors.Is(err, ErrFinalization) || !errors.Is(err, cleanupErr) || !errors.Is(err, cause) {
				t.Errorf("completion lost a cause: %v", err)
			}
			if last.Load() != 1 {
				t.Error("earlier cleanup failure skipped a sibling resource")
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if err := j.finalize(Finalization{}); !errors.Is(err, cleanupErr) || !errors.Is(err, ErrFinalization) {
						t.Errorf("repeated finalization lost its terminal error: %v", err)
					}
				})
			}
			wg.Wait()
			if last.Load() != 1 {
				t.Error("concurrent repeated finalization ran cleanup again")
			}
			j.mu.Lock()
			complete := j.artifact.complete
			j.mu.Unlock()
			if complete {
				t.Error("failed cleanup retained a complete artifact claim")
			}
		})
	}
}

func TestJobFinalizationPrecedesCompletionPublication(t *testing.T) {
	observer := &recordingObserver{}
	m := newManagerWithObserver(observer)
	defer m.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	j := m.StartForSession("session", "task", "cleanup", func(context.Context, io.Writer) (string, error) {
		return "done", nil
	}, WithFinalizer(func(Finalization) error { close(entered); <-release; return nil }))
	<-entered
	select {
	case <-j.done:
		t.Error("job completed while finalizer remains blocked")
	default:
	}
	if len(observer.seen()) != 0 || m.DrainCompletedNote() != "" || len(m.Running()) != 1 {
		t.Error("terminal publication overtook resource cleanup")
	}
	close(release)
	<-j.done
	waitForObserver(t, observer)
}

type finalizationStartGate struct {
	entered chan struct{}
	release chan struct{}
}

func (g *finalizationStartGate) RecordStart(string, string, string) {
	close(g.entered)
	<-g.release
}
func (*finalizationStartGate) RecordDone(string, Status, error) {}

func TestJobFinalizationCloseBetweenPublicationAndLaunch(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	gate := &finalizationStartGate{entered: make(chan struct{}), release: make(chan struct{})}
	m.SetTaskRecorder(gate)
	states, result := make(chan Finalization, 1), make(chan *Job, 1)
	var invoked atomic.Bool
	go func() {
		result <- m.StartForSession("session", "task", "race", func(context.Context, io.Writer) (string, error) {
			invoked.Store(true)
			return "", nil
		}, WithFinalizer(func(state Finalization) error { states <- state; return nil }))
	}()
	<-gate.entered
	m.Close()
	close(gate.release)
	j := <-result
	<-j.done
	select {
	case state := <-states:
		if state.Invoked || !errors.Is(state.Err, context.Canceled) {
			t.Errorf("close/launch state = %+v", state)
		}
	default:
		t.Error("close/launch race skipped finalization")
	}
	if invoked.Load() {
		t.Error("closed manager invoked the job closure")
	}
}

func TestJobFinalizationWaitsForClosureAfterLogicalCancellation(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var cleanups atomic.Int32
	j := m.StartForSession("session", "task", "unwinding", func(ctx context.Context, _ io.Writer) (string, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return "", ctx.Err()
	}, WithFinalizer(func(state Finalization) error {
		if !state.Invoked || !errors.Is(state.Err, context.Canceled) {
			t.Errorf("unwinding state = %+v", state)
		}
		cleanups.Add(1)
		return nil
	}))
	<-entered
	m.CloseWithGrace(0)
	<-cancelled
	if cleanups.Load() != 0 {
		t.Error("logical cancellation finalized a live closure")
	}
	select {
	case <-j.done:
		t.Error("logical cancellation published physical closure completion")
	default:
	}
	close(release)
	<-j.done
	if cleanups.Load() != 1 {
		t.Error("returned closure did not finalize")
	}
}
