package builtin

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
)

// End-to-end through the actual tools: a background bash job runs under a manager
// injected on the context, the wait tool collects its output, and bash_output
// reads it — the same path the agent drives.
func TestBackgroundBashWaitAndOutput(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ctx := jobs.WithManager(context.Background(), m)

	start, err := bash{}.Execute(ctx, []byte(`{"command":"printf hello; sleep 0.3","run_in_background":true}`))
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
	res := m.Wait(ctx, []string{id}, 40)
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
