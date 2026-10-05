package traystate

import (
	"runtime"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
)

func emit(sink event.Sink, kinds ...event.Kind) {
	for _, k := range kinds {
		sink.Emit(event.Event{Kind: k})
	}
}

// The icon gets one glyph, so the fold has to rank: being needed outranks being
// busy. A window that is hidden while one of three panes waits for an answer has
// to say "waiting", not "busy".
func TestAttentionOutranksWorkAcrossPanes(t *testing.T) {
	tracker := New(nil)
	first := tracker.Watch("a", event.Discard)
	second := tracker.Watch("b", event.Discard)

	emit(first, event.TurnStarted)
	emit(second, event.TurnStarted)
	if got := tracker.State(); got.Mood() != MoodWorking || got.Working != 2 || got.Panes != 2 {
		t.Fatalf("state = %+v, want two panes working", got)
	}

	emit(second, event.ApprovalRequest)
	got := tracker.State()
	if got.Mood() != MoodAttention || got.Attention != 1 || got.Working != 2 {
		t.Fatalf("state = %+v, want one pane waiting while both still run", got)
	}
}

// Nothing on the wire announces an answer, so the fold reads the first thing the
// run produces afterwards as the resolution. A turn blocked on approval produces
// nothing until it is answered, which is what makes that sound.
func TestTheNextThingTheRunDoesClearsTheQuestion(t *testing.T) {
	tracker := New(nil)
	sink := tracker.Watch("a", event.Discard)
	emit(sink, event.TurnStarted, event.ApprovalRequest)
	if tracker.State().Mood() != MoodAttention {
		t.Fatal("a pending approval did not reach the fold")
	}
	emit(sink, event.ToolDispatch)
	if got := tracker.State(); got.Mood() != MoodWorking || got.Attention != 0 {
		t.Fatalf("state = %+v, want the question settled and the turn still running", got)
	}
	emit(sink, event.TurnDone)
	if got := tracker.State(); got.Mood() != MoodIdle || got.Working != 0 {
		t.Fatalf("state = %+v, want idle after the turn", got)
	}
}

// A pane that is gone stops counting at once: the icon speaks for what is open,
// and a closed pane's last state would otherwise sit in it forever.
func TestAClosedPaneStopsCounting(t *testing.T) {
	tracker := New(nil)
	sink := tracker.Watch("a", event.Discard)
	emit(sink, event.TurnStarted, event.ApprovalRequest)
	tracker.Drop("a")
	if got := tracker.State(); got != (State{}) {
		t.Fatalf("state = %+v, want nothing left after the pane closed", got)
	}
}

// A window with no turn running is not idle if it left a dev server behind, and
// nothing else on screen would say so once the window is hidden.
func TestBackgroundJobsCountAsWorkLeftBehind(t *testing.T) {
	tracker := New(nil)
	tracker.Watch("a", event.Discard)
	tracker.SetJobs(2)
	got := tracker.State()
	if got.Jobs != 2 || !got.Busy() {
		t.Fatalf("state = %+v, want an idle pane with jobs to read as busy", got)
	}
	if got.Mood() != MoodIdle {
		t.Fatalf("mood = %v, want idle: a background job is not something to interrupt for", got.Mood())
	}
}

// The callback paints an icon. Calling it from inside the fold would run it on
// every pane's emitting goroutine while the lock is held, and only when
// something actually changed is it worth running at all.
func TestChangesAreReportedOnceAndOutsideTheLock(t *testing.T) {
	var mu sync.Mutex
	var seen []State
	var tracker *Tracker
	tracker = New(func(s State) {
		// Re-entering the tracker from the callback deadlocks if publish still
		// holds the lock, which is exactly what this is here to catch.
		_ = tracker.State()
		mu.Lock()
		seen = append(seen, s)
		mu.Unlock()
	})
	sink := tracker.Watch("a", event.Discard)
	emit(sink, event.TurnStarted, event.Reasoning, event.Reasoning, event.TurnDone)

	mu.Lock()
	defer mu.Unlock()
	// Watch, TurnStarted, TurnDone. The reasoning deltas changed nothing.
	if len(seen) != 3 {
		t.Fatalf("reported %d changes (%+v), want one per actual change", len(seen), seen)
	}
}

func TestConcurrentPanesFoldSafely(t *testing.T) {
	tracker := New(func(State) {})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sink := tracker.Watch(string(rune('a'+n)), event.Discard)
			for range 50 {
				emit(sink, event.TurnStarted, event.ToolDispatch, event.TurnDone)
			}
		}(i)
	}
	wg.Wait()
	if got := tracker.State(); got.Panes != 8 || got.Working != 0 || got.Attention != 0 {
		t.Fatalf("state = %+v, want eight quiet panes", got)
	}
}

// The icon has to end up describing the fold the tracker holds. Folds are taken
// under the lock and painted outside it, so two panes changing at once can
// reach the callback in the opposite order, and the loser strands the icon on a
// state already moved past — with nothing to correct it, since the fold reports
// only what changed.
func TestTheLastPaintDescribesTheCurrentFold(t *testing.T) {
	for attempt := range 200 {
		var mu sync.Mutex
		var last State
		tracker := New(func(s State) {
			// The real callback paints through a platform call: not fast, and
			// that is what opens the window between fold and paint.
			runtime.Gosched()
			mu.Lock()
			last = s
			mu.Unlock()
		})
		a := tracker.Watch("a", event.Discard)
		b := tracker.Watch("b", event.Discard)
		emit(a, event.TurnStarted)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); emit(a, event.TurnDone) }()
		go func() { defer wg.Done(); emit(b, event.TurnStarted) }()
		wg.Wait()

		mu.Lock()
		painted := last
		mu.Unlock()
		if got := tracker.State(); painted != got {
			t.Fatalf("attempt %d: icon shows %+v, tracker holds %+v", attempt, painted, got)
		}
	}
}
