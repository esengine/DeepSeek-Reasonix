package delegation

import (
	"context"
	"io"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

// A background sub-agent works in its own transcript, never in the job's own
// streams. beginExecution is the single boundary every delegated run crosses,
// and it is where the child's liveness gets bound to its job: without that, the
// job's activity stamp never moves and a busy run reads as stalled for its whole
// life. This asserts the binding itself, so removing that one call fails here.
func TestBeginningExecutionBindsTheChildToItsJob(t *testing.T) {
	jm := jobs.NewManager(event.Discard)
	defer jm.Close()

	ready := make(chan context.Context, 1)
	j := jm.Start("task", "probe", func(jobCtx context.Context, _ io.Writer) (string, error) {
		ready <- jobCtx
		<-jobCtx.Done()
		return "", jobCtx.Err()
	})
	defer jm.Kill(j.ID)

	var jobCtx context.Context
	select {
	case jobCtx = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never started")
	}

	trk := tracked(t, newFakeProgressClock(time.Unix(0, 0)), chanSink{ch: make(chan event.Event, 64)})
	if trk.noteProgress != nil {
		t.Fatal("tracker acquired a notifier before execution began")
	}
	task := &TaskTool{}
	if err := task.beginExecution(jobCtx, nil, true, trk, nil); err != nil {
		t.Fatalf("beginExecution: %v", err)
	}
	if trk.noteProgress == nil {
		t.Fatal("beginning execution left the child unbound: its job will read as silent")
	}

	trk.noteProgress(1)
	res, _ := jm.Wait(context.Background(), []string{j.ID}, jobs.WaitOptions{Timeout: time.Second})
	if len(res) != 1 || res[0].Progress == nil {
		t.Fatalf("job state = %+v, want a running job with progress", res)
	}
	if got := res[0].Progress.Steps; got != 1 {
		t.Fatalf("Steps = %d, want 1: the bound notifier never reached the job", got)
	}
}

// A foreground run crosses the same boundary but has no job to tell, so it must
// stay unbound rather than acquire a notifier with nothing to report to.
func TestBeginningExecutionLeavesAForegroundRunUnbound(t *testing.T) {
	trk := tracked(t, newFakeProgressClock(time.Unix(0, 0)), chanSink{ch: make(chan event.Event, 64)})
	task := &TaskTool{}
	if err := task.beginExecution(context.Background(), nil, true, trk, nil); err != nil {
		t.Fatalf("beginExecution: %v", err)
	}
	if trk.noteProgress != nil {
		t.Fatal("a foreground run acquired a background notifier")
	}
}
