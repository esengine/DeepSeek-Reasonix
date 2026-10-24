package delegation

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/tools/jobs"
)

// noteRecorder collects what a tracker told its job.
type noteRecorder struct {
	mu    sync.Mutex
	steps []int
	calls int
}

func (r *noteRecorder) note(steps int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.steps = append(r.steps, steps)
}

func (r *noteRecorder) snapshot() (calls int, steps []int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, append([]int(nil), r.steps...)
}

// tracked builds a tracker joined to a shared merger, so tests control the clock.
func tracked(t *testing.T, clock progressClock, sink event.Sink) *subagentProgressTracker {
	t.Helper()
	merger := newSubagentProgressMerger(clock, sink, "group-1")
	t.Cleanup(merger.Close)
	ctx := withSubagentProgressMerger(agent.WithCallContext(context.Background(), "child-1", sink, nil, false), merger)
	return newSubagentProgressTracker(ctx, subSink(ctx))
}

// A finished tool call is a completed unit of work and is reported as such, at
// whatever rate the child produces them: it is the only signal a job whose
// streams stay empty ever gets.
func TestAFinishedToolCallReportsOneStepToTheJob(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	trk := tracked(t, clock, chanSink{ch: make(chan event.Event, 256)})
	rec := &noteRecorder{}
	trk.noteProgress = rec.note
	wrap := trk.wrap()

	for range 3 {
		wrap.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "bash_1", Name: "bash"}})
	}
	calls, steps := rec.snapshot()
	if calls != 3 {
		t.Fatalf("notified %d times, want one per finished tool call", calls)
	}
	for i, s := range steps {
		if s != 1 {
			t.Fatalf("step report %d = %d, want 1", i, s)
		}
	}
}

// Token deltas arrive far more often than a watchdog reading minutes needs, so
// they are paced. What matters is that the job keeps hearing the run is alive.
func TestTokenDeltasRefreshLivenessAtMostOncePerPace(t *testing.T) {
	clock := newFakeProgressClock(time.Unix(0, 0))
	trk := tracked(t, clock, chanSink{ch: make(chan event.Event, 256)})
	rec := &noteRecorder{}
	trk.noteProgress = rec.note
	wrap := trk.wrap()

	wrap.Emit(event.Event{Kind: event.Text, Text: "a"})
	if calls, _ := rec.snapshot(); calls != 1 {
		t.Fatalf("first delta notified %d times, want 1", calls)
	}
	clock.Advance(noteProgressPace / 2)
	wrap.Emit(event.Event{Kind: event.Text, Text: "b"})
	if calls, _ := rec.snapshot(); calls != 1 {
		t.Fatalf("delta inside the window notified again: %d calls", calls)
	}
	clock.Advance(noteProgressPace)
	wrap.Emit(event.Event{Kind: event.Text, Text: "c"})
	if calls, steps := rec.snapshot(); calls != 2 {
		t.Fatalf("delta past the window notified %d times, want 2 (steps %v)", calls, steps)
	}
}

// A foreground run has no job to tell, and must not acquire a notifier.
func TestAForegroundRunReportsNothing(t *testing.T) {
	trk := tracked(t, newFakeProgressClock(time.Unix(0, 0)), chanSink{ch: make(chan event.Event, 256)})
	bindJobLiveness(context.Background(), trk)
	trk.wrap().Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "bash_1", Name: "bash"}})
	if trk.noteProgress != nil {
		t.Fatal("a foreground tracker acquired a background notifier")
	}
}

// End to end through the real registry: child tool results reach the job's step
// count, which is what keeps a job that writes nothing from reading as idle.
func TestChildToolResultsReachTheOwningJob(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	defer close(release)
	ctxSink := make(chan event.Event, 256)
	started := make(chan struct{})
	j := m.Start("task", "audit", func(jobCtx context.Context, _ io.Writer) (string, error) {
		trk := tracked(t, newFakeProgressClock(time.Unix(0, 0)), chanSink{ch: ctxSink})
		bindJobLiveness(jobCtx, trk)
		if trk.noteProgress == nil {
			t.Error("a background run did not acquire a notifier")
		}
		wrap := trk.wrap()
		for range 4 {
			wrap.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "bash_1", Name: "bash"}})
		}
		close(started)
		<-release
		return "", nil
	})
	defer m.Kill(j.ID)

	<-started
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, _ := m.Wait(context.Background(), []string{j.ID}, jobs.WaitOptions{Timeout: 10 * time.Millisecond})
		if len(res) == 1 && res[0].Progress != nil && res[0].Progress.Steps == 4 {
			if res[0].Progress.Produced != 0 {
				t.Fatalf("Produced = %d, want no output text: the child writes none", res[0].Progress.Produced)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("child tool results never reached the job's step count")
}
