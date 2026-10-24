package builtin

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

// The ceiling is the rule the schema states, and it applies to the default too.
// Omitting the timeout means "wait for the jobs", not "wait forever": an
// unbounded wait on a job that never finishes is what a hung session looks like.
func TestWaitCeilingIsAppliedAndReported(t *testing.T) {
	if secs, capped := clampWaitSeconds(30); secs != 30 || capped {
		t.Fatalf("clampWaitSeconds(30) = %d, %v; want 30, false", secs, capped)
	}
	if secs, capped := clampWaitSeconds(900); secs != waitCeilingSeconds || !capped {
		t.Fatalf("clampWaitSeconds(900) = %d, %v; want %d, true", secs, capped, waitCeilingSeconds)
	}
	if secs, capped := clampWaitSeconds(0); secs != waitCeilingSeconds || !capped {
		t.Fatalf("clampWaitSeconds(0) = %d, %v; want the ceiling %d, true", secs, capped, waitCeilingSeconds)
	}

	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	// A job that finishes at once. Reaching the ceiling is a property of the
	// wait, not of the request: this one returned before any timeout, so the
	// result must not claim a 300s wait that never happened.
	j := m.StartForSession("session", "task", "quick", func(jobCtx context.Context, _ io.Writer) (string, error) {
		return "done", nil
	})
	res, _ := m.Wait(context.Background(), []string{j.ID}, jobs.WaitOptions{Timeout: time.Second})
	if len(res) == 0 || res[0].Status == jobs.Running {
		t.Fatalf("job did not finish: %+v", res)
	}

	out, err := (waitJob{}).Execute(ctx, []byte(`{"job_ids":["`+j.ID+`"],"timeout_seconds":900}`))
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.Contains(out, "the ceiling for one wait") {
		t.Fatalf("wait output = %q, want no claim about a wait that never ran out", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("wait output = %q, want the job's result", out)
	}
}

// A bounded wait returns instead of holding the turn open, which is what lets
// the caller look at the progress and decide.
func TestWaitWithATimeoutReturnsProgress(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	release := make(chan struct{})
	defer close(release)
	j := m.StartForSession("session", "task", "slow", func(jobCtx context.Context, _ io.Writer) (string, error) {
		select {
		case <-release:
		case <-jobCtx.Done():
		}
		return "", jobCtx.Err()
	})
	defer m.Kill(j.ID)

	start := time.Now()
	out, err := (waitJob{}).Execute(ctx, []byte(`{"job_ids":["`+j.ID+`"],"timeout_seconds":1}`))
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("a 1s wait took %s", elapsed)
	}
	if !strings.Contains(out, string(jobs.Running)) {
		t.Fatalf("wait output = %q, want the running status", out)
	}
	if !strings.Contains(out, "running ") {
		t.Fatalf("wait output = %q, want the progress readings", out)
	}
}
