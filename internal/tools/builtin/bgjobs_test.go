package builtin

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/jobs"
)

// End-to-end through the actual tools: a background bash job runs under a manager
// injected on the context, the wait tool collects its output, and bash_output
// reads it — the same path the agent drives.
func TestBackgroundBashWaitAndOutput(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithManager(context.Background(), m)

	start, err := bash{}.Execute(ctx, []byte(`{"command":"echo hello; sleep 0.3","run_in_background":true}`))
	if err != nil {
		t.Fatalf("bash background: %v", err)
	}
	if !strings.Contains(start, "Started background job") {
		t.Fatalf("unexpected start message: %q", start)
	}

	// The job is registered and running synchronously before Execute returns.
	running := m.Running()
	if len(running) != 1 {
		t.Fatalf("want 1 running job, got %d", len(running))
	}
	id := running[0].ID

	// wait blocks until it finishes, then returns its output.
	wout, err := waitJob{}.Execute(ctx, []byte(`{"job_ids":["`+id+`"]}`))
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !strings.Contains(wout, "done") || !strings.Contains(wout, "hello") {
		t.Errorf("wait output = %q, want it to report done with hello", wout)
	}

	// bash_output reads the buffered output (wait doesn't consume the read cursor).
	bo, err := bashOutput{}.Execute(ctx, []byte(`{"job_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("bash_output: %v", err)
	}
	if !strings.Contains(bo, "hello") {
		t.Errorf("bash_output = %q, want hello", bo)
	}
}

// kill_shell terminates a long-running background job.
func TestBackgroundKill(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithManager(context.Background(), m)

	if _, err := (bash{}).Execute(ctx, []byte(`{"command":"sleep 120","run_in_background":true}`)); err != nil {
		t.Fatalf("bash background: %v", err)
	}
	id := m.Running()[0].ID

	kout, err := killShell{}.Execute(ctx, []byte(`{"job_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("kill_shell: %v", err)
	}
	if !strings.Contains(kout, "Killed") {
		t.Errorf("kill_shell = %q, want it to report Killed", kout)
	}
	// 120s natural duration keeps the job far from finishing on its own, so the
	// reap window is the only thing this measures: a loaded machine's slow
	// process-tree teardown (up to ~bashWaitDelay) still fits, while a genuinely
	// broken kill trips the 40s timeout. Pairing the sleep with the timeout (as
	// 10/10 did) raced natural completion against the reap.
	res, _ := m.Wait(ctx, []string{id}, jobs.WaitOptions{Timeout: 40 * time.Second})
	if len(res) != 1 || res[0].Status != jobs.Killed {
		t.Fatalf("want killed, got %+v", res)
	}
}

// Without a manager on the context the background tools degrade to a clear error
// rather than panicking.
func TestBackgroundToolsNoManager(t *testing.T) {
	ctx := context.Background()
	if _, err := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"bash-1"}`)); err == nil {
		t.Error("bash_output without a manager should error")
	}
	if _, err := (bash{}).Execute(ctx, []byte(`{"command":"echo hi","run_in_background":true}`)); err == nil {
		t.Error("background bash without a manager should error")
	}
}

// A background start is reported as its own state, with no exit status and no
// error: the command has not finished, so nothing about it has failed.
func TestBackgroundStartReportsStateWithoutErrorOrExitCode(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithManager(context.Background(), m)

	res, err := bash{}.ExecuteDetailed(ctx, []byte(`{"command":"sleep 0.2","run_in_background":true}`))
	if err != nil {
		t.Fatalf("background start returned an error: %v", err)
	}
	if res.Execution == nil || res.Execution.State != tool.ShellStateBackgroundStarted {
		t.Fatalf("execution = %+v, want state %q", res.Execution, tool.ShellStateBackgroundStarted)
	}
	if res.Execution.ExitCode != nil {
		t.Errorf("exit code = %d, want none before the job finishes", *res.Execution.ExitCode)
	}
	if running := m.Running(); len(running) != 1 || res.Execution.JobID != running[0].ID {
		t.Errorf("job id = %q, want the background job it started (%+v)", res.Execution.JobID, running)
	}
}

// A job that ended as failed makes the read that reports it a failed call, so
// the activity panel and the model see one outcome; the output still comes back
// with it. A job that finished, was killed or is still running is not a failure.
func TestReadingAFailedJobIsAFailedCall(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	failed := m.StartForSession("session", "bash", "make test", func(context.Context, io.Writer) (string, error) {
		return "2 tests failed", errors.New("exit status 2")
	})
	done := m.StartForSession("session", "bash", "echo ok", func(context.Context, io.Writer) (string, error) {
		return "all good", nil
	})

	out, err := waitJob{}.Execute(ctx, []byte(`{"job_ids":["`+failed.ID+`","`+done.ID+`"]}`))
	if !errors.Is(err, errJobFailed) || !strings.Contains(err.Error(), failed.ID) || strings.Contains(err.Error(), done.ID) {
		t.Fatalf("wait error = %v, want %v naming only %s", err, errJobFailed, failed.ID)
	}
	if !strings.Contains(out, "2 tests failed") || !strings.Contains(out, "all good") {
		t.Fatalf("wait output = %q, want both jobs' output", out)
	}

	out, err = bashOutput{}.Execute(ctx, []byte(`{"job_id":"`+failed.ID+`"}`))
	if !errors.Is(err, errJobFailed) || !strings.Contains(out, "failed") {
		t.Fatalf("bash_output = %q, %v; want the failed status with %v", out, err, errJobFailed)
	}
	if _, err := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"`+done.ID+`"}`)); err != nil {
		t.Fatalf("bash_output on a finished job: %v", err)
	}

	release := make(chan struct{})
	defer close(release)
	running := m.StartForSession("session", "bash", "sleep", func(jobCtx context.Context, _ io.Writer) (string, error) {
		select {
		case <-release:
		case <-jobCtx.Done():
		}
		return "", jobCtx.Err()
	})
	if _, err := (waitJob{}).Execute(ctx, []byte(`{"job_ids":["`+running.ID+`"],"timeout_seconds":1}`)); err != nil {
		t.Fatalf("wait on a running job: %v", err)
	}
	if _, err := (killShell{}).Execute(ctx, []byte(`{"job_id":"`+running.ID+`"}`)); err != nil {
		t.Fatalf("kill_shell: %v", err)
	}
	if _, err := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"`+running.ID+`"}`)); err != nil {
		t.Fatalf("bash_output on a killed job: %v", err)
	}
}

func TestBashOutputWithoutJobIDListsTheRunningJobs(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session-a")

	none, err := bashOutput{}.Execute(ctx, []byte(`{}`))
	if err != nil || !strings.Contains(none, "No background jobs are running") {
		t.Fatalf("empty listing = %q, %v", none, err)
	}

	for _, command := range []string{"sleep 5", "sleep 6"} {
		if _, err := (bash{}).Execute(ctx, []byte(`{"command":"`+command+`","run_in_background":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := bashOutput{}.Execute(ctx, []byte(`{}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, want := range []string{"Running background jobs (2)", "bash-1", "bash-2", "kind=bash", "sleep 5", "sleep 6", "elapsed="} {
		if !strings.Contains(got, want) {
			t.Errorf("listing lacks %q:\n%s", want, got)
		}
	}
}

func TestBashOutputListingIsFailSafeAndBounded(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	mine := jobs.WithSession(jobs.WithManager(context.Background(), m), "session-a")
	block := func(ctx context.Context, _ io.Writer) (string, error) { <-ctx.Done(); return "", ctx.Err() }
	for range maxListedJobs + 3 {
		m.StartForSession("session-a", "bash", "wait", block)
	}
	m.StartForSession("session-b", "bash", "other session", block)

	got, err := bashOutput{}.Execute(mine, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Running background jobs (53)") || !strings.Contains(got, "3 more running job(s) not listed") || strings.Contains(got, "other session") {
		t.Fatalf("listing:\n%s", got)
	}
	if n := strings.Count(got, "\n- "); n != maxListedJobs {
		t.Fatalf("listed %d rows, want %d", n, maxListedJobs)
	}

	noSession, err := bashOutput{}.Execute(jobs.WithManager(context.Background(), m), []byte(`{}`))
	if err != nil || !strings.Contains(noSession, "No background jobs are running") {
		t.Fatalf("without a session the listing must be empty, got %q, %v", noSession, err)
	}
}

// A wait shows the work it waits on: what the job writes reaches the call's
// live progress, the channel a running bash already streams through.
func TestWaitStreamsTheJobOutputAsProgress(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	release := make(chan struct{})
	j := m.Start("bash", "build", func(ctx context.Context, w io.Writer) (string, error) {
		<-release
		_, _ = io.WriteString(w, "step 1\nstep 2\n")
		return "", nil
	})
	var live strings.Builder
	ctx := tool.WithProgress(jobs.WithManager(context.Background(), m), func(chunk string) { live.WriteString(chunk) })
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	if _, err := (waitJob{}).Execute(ctx, []byte(`{"job_ids":["`+j.ID+`"]}`)); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if live.String() != "step 1\nstep 2\n" {
		t.Fatalf("progress = %q, want the job's lines", live.String())
	}
}
