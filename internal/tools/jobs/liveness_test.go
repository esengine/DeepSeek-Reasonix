package jobs

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"reasonix/internal/contract/event"
)

// A sub-agent's work reaches its transcript and tool calls, never its job's own
// streams. Judging liveness by bytes alone therefore calls a busy job silent for
// its whole life; what it reports instead is that it is still working.
func TestAJobThatWritesNothingStaysAliveWhileItReportsProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := &recordingSink{}
		m := NewManager(sink, WithStalledWarningAfter(60*time.Millisecond))
		defer m.Close()

		j := m.Start("task", "silent worker", func(ctx context.Context, _ io.Writer) (string, error) {
			// Six ticks over ~180ms: three warning windows, none of them quiet
			// enough to deserve a report.
			for range 6 {
				NoteProgress(ctx, 1)
				time.Sleep(30 * time.Millisecond)
			}
			return "done", nil
		})
		res, _ := m.Wait(context.Background(), []string{j.ID}, WaitOptions{Timeout: 5 * time.Second})
		if len(res) != 1 || res[0].Status != Done {
			t.Fatalf("result = %+v, want one finished job", res)
		}
		for _, text := range sink.texts() {
			if strings.Contains(text, "has been inactive for") {
				t.Fatalf("a job reporting progress was reported inactive: %q", text)
			}
		}
	})
}

// The reading the caller gets must separate "wrote nothing" from "did nothing":
// a sub-agent has steps and no output text, and that pair is not silence.
func TestWaitReadsStepsSeparatelyFromOutputText(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	defer close(release)
	j := m.Start("task", "busy", func(ctx context.Context, _ io.Writer) (string, error) {
		NoteProgress(ctx, 3)
		<-release
		return "", nil
	})
	defer m.Kill(j.ID)

	var got *Progress
	waitFor(t, func() bool {
		res, _ := m.Wait(context.Background(), []string{j.ID}, WaitOptions{Timeout: time.Millisecond})
		if len(res) == 1 && res[0].Progress != nil && res[0].Progress.Steps == 3 {
			got = res[0].Progress
			return true
		}
		return false
	})
	if got.Produced != 0 || got.Spoke {
		t.Fatalf("progress = %+v, want no output text: the job wrote none", got)
	}
	if got.Silent > time.Second {
		t.Fatalf("Silent = %s, want recent activity from the reported steps", got.Silent)
	}
}

// A job that stops reporting is still reported as inactive, so the fix does not
// trade a false positive for a false negative.
func TestAJobThatStopsReportingIsStillCalledInactive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := &recordingSink{}
		m := NewManager(sink, WithStalledWarningAfter(40*time.Millisecond))
		defer m.Close()

		release := make(chan struct{})
		defer close(release)
		j := m.Start("task", "stops", func(ctx context.Context, _ io.Writer) (string, error) {
			NoteProgress(ctx, 2) // one step, then silence
			<-release
			return "", nil
		})
		defer m.Kill(j.ID)

		waitFor(t, func() bool {
			for _, text := range sink.texts() {
				if strings.Contains(text, "has been inactive for") && strings.Contains(text, j.ID) {
					return true
				}
			}
			return false
		})
		note := m.DrainCompletedNote()
		if !strings.Contains(note, "2 step(s) were reported") {
			t.Fatalf("drain note = %q, want the steps it reported before going quiet", note)
		}
	})
}

// A cancelled job settles at once: reporting progress must not keep a killed
// job's status from being observable.
func TestKilledJobStopsBeingReportedAsRunning(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.Start("task", "cancel me", func(ctx context.Context, _ io.Writer) (string, error) {
		for {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Millisecond):
				NoteProgress(ctx, 1)
			}
		}
	})
	time.Sleep(20 * time.Millisecond)
	if !m.Kill(j.ID) {
		t.Fatal("Kill reported the job was not running")
	}
	res, _ := m.Wait(context.Background(), []string{j.ID}, WaitOptions{Timeout: 2 * time.Second})
	if len(res) != 1 {
		t.Fatalf("results = %+v, want one", res)
	}
	if res[0].Status != Killed {
		t.Fatalf("status = %q, want %q", res[0].Status, Killed)
	}
	if res[0].Progress != nil {
		t.Fatalf("progress = %+v, want none for a settled job", res[0].Progress)
	}
}
