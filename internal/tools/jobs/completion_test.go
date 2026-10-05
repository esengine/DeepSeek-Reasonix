package jobs

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

type recordingObserver struct {
	mu     sync.Mutex
	events []CompletionEvent
	err    error
	// observe runs inside OnJobCompletion, where what the rest of the manager
	// reports about the job is what a woken turn would see.
	observe func(CompletionEvent)
}

// The event is recorded only after observe returns, so a test that sees the
// event also sees everything observe wrote.
func (o *recordingObserver) OnJobCompletion(_ context.Context, ev CompletionEvent) error {
	o.mu.Lock()
	observe, err := o.observe, o.err
	o.mu.Unlock()
	if observe != nil {
		observe(ev)
	}
	o.mu.Lock()
	o.events = append(o.events, ev)
	o.mu.Unlock()
	return err
}

func (o *recordingObserver) seen() []CompletionEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]CompletionEvent(nil), o.events...)
}

func newManagerWithObserver(o CompletionObserver) *Manager {
	m := NewManager(event.Discard)
	m.SetCompletionObserver(o)
	return m
}

func waitForObserver(t *testing.T, o *recordingObserver) CompletionEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if seen := o.seen(); len(seen) > 0 {
			return seen[0]
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for a completion event")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// The observer is told only once the job's terminal status is observable
// everywhere else: a woken turn's first question is usually about this job.
func TestObserverSeesTerminalStatusAndClosedDone(t *testing.T) {
	obs := &recordingObserver{}
	m := newManagerWithObserver(obs)
	defer m.Close()

	var (
		statusAtObserve Status
		doneAtObserve   bool
	)
	var j *Job
	ready := make(chan struct{})
	obs.observe = func(CompletionEvent) {
		<-ready
		j.mu.Lock()
		statusAtObserve = j.status
		j.mu.Unlock()
		select {
		case <-j.done:
			doneAtObserve = true
		default:
		}
	}
	j = m.Start("bash", "echo", func(_ context.Context, out io.Writer) (string, error) {
		io.WriteString(out, "hi\n")
		return "", nil
	})
	close(ready)
	waitForObserver(t, obs)
	if statusAtObserve != Done {
		t.Errorf("status at observe = %q, want %q", statusAtObserve, Done)
	}
	if !doneAtObserve {
		t.Error("done channel was still open when the observer ran")
	}
}

// A delivered event is the manager's to forget: the next user turn must not
// carry a completion the model was already woken for.
func TestDeliveredCompletionLeavesNoLegacyNote(t *testing.T) {
	obs := &recordingObserver{}
	m := newManagerWithObserver(obs)
	defer m.Close()

	j := m.Start("bash", "echo", func(context.Context, io.Writer) (string, error) { return "", nil })
	ev := waitForObserver(t, obs)
	if ev.JobID != j.ID {
		t.Fatalf("event job = %q, want %q", ev.JobID, j.ID)
	}
	if ev.ID != CompletionEventID(j.ID) {
		t.Errorf("event id = %q, want %q", ev.ID, CompletionEventID(j.ID))
	}
	if ev.Status != Done {
		t.Errorf("event status = %q, want %q", ev.Status, Done)
	}
	if note := m.DrainCompletedNote(); note != "" {
		t.Errorf("drain after delivery = %q, want empty", note)
	}
}

// An observer that could not take the event leaves the old path intact, so the
// completion still reaches the model on the next real turn.
func TestUndeliveredCompletionKeepsLegacyNote(t *testing.T) {
	obs := &recordingObserver{err: context.Canceled}
	m := newManagerWithObserver(obs)
	defer m.Close()

	j := m.Start("bash", "echo", func(context.Context, io.Writer) (string, error) { return "", nil })
	waitForObserver(t, obs)
	note := m.DrainCompletedNote()
	if !strings.Contains(note, j.ID) {
		t.Errorf("drain after failed delivery = %q, want it to mention %s", note, j.ID)
	}
}

// No observer at all is the pre-bridge world, and it still works.
func TestNoObserverKeepsLegacyNote(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.Start("bash", "echo", func(context.Context, io.Writer) (string, error) { return "", nil })
	if _, outcome := m.Wait(context.Background(), []string{j.ID}, WaitOptions{Timeout: 2 * time.Second}); outcome == WaitTimedOut {
		t.Fatal("job did not finish")
	}
	if note := m.DrainCompletedNote(); !strings.Contains(note, j.ID) {
		t.Errorf("drain = %q, want it to mention %s", note, j.ID)
	}
}

// A failed job is still a completion: the wake-up is what carries the failure
// to the model, not a Notice the model never sees.
func TestFailedJobPublishesCompletionEvent(t *testing.T) {
	obs := &recordingObserver{}
	m := newManagerWithObserver(obs)
	defer m.Close()

	m.Start("task", "broken", func(context.Context, io.Writer) (string, error) {
		return "", context.DeadlineExceeded
	})
	ev := waitForObserver(t, obs)
	if ev.Status != Failed {
		t.Errorf("event status = %q, want %q", ev.Status, Failed)
	}
	if ev.Kind != "task" || ev.Label != "broken" {
		t.Errorf("event kind/label = %q/%q, want task/broken", ev.Kind, ev.Label)
	}
}

// The identity is derived from the job, not minted per publish, so a delivery
// path that runs twice can tell a replay from a second completion.
func TestCompletionEventIDIsDeterministic(t *testing.T) {
	if a, b := CompletionEventID("task-12"), CompletionEventID("task-12"); a != b {
		t.Fatalf("two ids for one job: %q, %q", a, b)
	}
	if got, want := CompletionEventID("task-12"), "completion/task-12/terminal-v1"; got != want {
		t.Errorf("id = %q, want %q", got, want)
	}
}
