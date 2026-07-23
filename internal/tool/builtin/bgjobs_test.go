package builtin

import (
	"context"
	"io"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/planmode"
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

func TestWaitMergesBackgroundEvidenceExactlyOnce(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ledger := evidence.NewLedger()
	ctx := jobs.WithManager(context.Background(), m)
	ctx = jobs.WithSession(ctx, "session")
	ctx = evidence.WithLedger(ctx, ledger)

	j := m.StartForSession("session", "task", "writer", func(jobCtx context.Context, _ io.Writer) (string, error) {
		jobs.PublishEvidence(jobCtx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file",
			Success:  true,
			Mutation: true,
			Write:    true,
			Paths:    []string{"changed.go"},
		}}})
		return "done", nil
	})

	args := []byte(`{"job_ids":["` + j.ID + `"]}`)
	if _, err := (waitJob{}).Execute(ctx, args); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if !ledger.Summary().HasMutation() {
		t.Fatal("wait did not merge the task job's mutation evidence")
	}
	firstLen := ledger.Len()
	if _, err := (waitJob{}).Execute(ctx, args); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	if got := ledger.Len(); got != firstLen {
		t.Fatalf("second wait duplicated evidence: len %d -> %d", firstLen, got)
	}
}

func TestWaitWithoutLedgerDoesNotConsumeBackgroundEvidence(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	baseCtx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	j := m.StartForSession("session", "task", "writer", func(jobCtx context.Context, _ io.Writer) (string, error) {
		jobs.PublishEvidence(jobCtx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Write: true, Paths: []string{"changed.go"},
		}}})
		return "done", nil
	})
	args := []byte(`{"job_ids":["` + j.ID + `"]}`)
	if _, err := (waitJob{}).Execute(baseCtx, args); err != nil {
		t.Fatalf("wait without ledger: %v", err)
	}

	ledger := evidence.NewLedger()
	if _, err := (waitJob{}).Execute(evidence.WithLedger(baseCtx, ledger), args); err != nil {
		t.Fatalf("wait with ledger: %v", err)
	}
	if !ledger.Summary().HasMutation() {
		t.Fatal("wait without a ledger consumed evidence before a collecting turn could merge it")
	}
}

func TestWaitInPlanModeDefersBackgroundEvidence(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	baseCtx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	j := m.StartForSession("session", "task", "writer", func(jobCtx context.Context, _ io.Writer) (string, error) {
		jobs.PublishEvidence(jobCtx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Write: true, Paths: []string{"changed.go"},
		}}})
		return "done", nil
	})
	args := []byte(`{"job_ids":["` + j.ID + `"]}`)

	// A planning turn may wait on jobs, but merging mutation receipts there
	// would arm delivery sign-off demands the read-only turn cannot satisfy.
	planLedger := evidence.NewLedger()
	planCtx := planmode.WithActive(evidence.WithLedger(baseCtx, planLedger), true)
	if _, err := (waitJob{}).Execute(planCtx, args); err != nil {
		t.Fatalf("wait in plan mode: %v", err)
	}
	if planLedger.Summary().HasMutation() {
		t.Fatal("plan-mode wait merged mutation evidence into the planning turn")
	}

	// The evidence stays on the job for the first normal turn to collect.
	ledger := evidence.NewLedger()
	normalCtx := planmode.WithActive(evidence.WithLedger(baseCtx, ledger), false)
	if _, err := (waitJob{}).Execute(normalCtx, args); err != nil {
		t.Fatalf("wait after plan mode: %v", err)
	}
	if !ledger.Summary().HasMutation() {
		t.Fatal("plan-mode wait consumed the background evidence instead of deferring it")
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

// kill_shell flips a job's status to Killed synchronously, well before its
// cancelled run goroutine actually unwinds, flushes PublishEvidence, and closes
// done. A bash_output poll that lands in that window must not note an empty
// lease: the ledger's lease is idempotent per turn, so noting it early would
// dedupe away every later retry in this same turn while the job's real
// mutation evidence is still forthcoming — and a turn that goes on to deliver
// would then commit (permanently drain) evidence nobody ever merged or
// reviewed. This deterministically drives that exact window with channels
// instead of a timing race.
func TestKilledJobBashOutputDoesNotNoteLeaseBeforeEvidenceIsReady(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	ledger := evidence.NewLedger()
	ctx := jobs.WithManager(context.Background(), m)
	ctx = jobs.WithSession(ctx, "session")
	ctx = evidence.WithLedger(ctx, ledger)

	cancelSeen := make(chan struct{})
	release := make(chan struct{})
	j := m.StartForSession("session", "task", "writer", func(jobCtx context.Context, _ io.Writer) (string, error) {
		<-jobCtx.Done()
		close(cancelSeen)
		// Simulate a job that keeps unwinding (e.g. a subprocess still tearing
		// down) after cancellation is requested but before it actually returns.
		<-release
		jobs.PublishEvidence(jobCtx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Write: true, Paths: []string{"changed.go"},
		}}})
		return "", context.Canceled
	})

	if _, err := (killShell{}).Execute(ctx, []byte(`{"job_id":"`+j.ID+`"}`)); err != nil {
		t.Fatalf("kill_shell: %v", err)
	}
	<-cancelSeen // the goroutine observed the cancellation but has not returned

	// bash_output lands in the unwinding window: status already reports Killed,
	// but the job's done channel is not closed yet and no evidence exists.
	bo, err := bashOutput{}.Execute(ctx, []byte(`{"job_id":"`+j.ID+`"}`))
	if err != nil {
		t.Fatalf("bash_output during unwind: %v", err)
	}
	if !strings.Contains(bo, "killed") {
		t.Fatalf("bash_output during unwind = %q, want killed status", bo)
	}
	if ledger.Summary().HasMutation() {
		t.Fatal("bash_output merged mutation evidence before the job was ready")
	}
	if leases := ledger.BackgroundLeases(); len(leases) != 0 {
		t.Fatalf("bash_output noted a lease before the job was ready: %+v", leases)
	}

	close(release)
	if res := m.WaitForSession(context.Background(), "session", []string{j.ID}, 5); len(res) != 1 || res[0].Status != jobs.Killed {
		t.Fatalf("post-unwind wait = %+v, want one killed result", res)
	}

	// A later retry — the model calling bash_output again, or the next turn's
	// automatic re-lease — must still find the evidence, not a dead dedupe entry.
	if _, err := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"`+j.ID+`"}`)); err != nil {
		t.Fatalf("bash_output after unwind: %v", err)
	}
	if !ledger.Summary().HasMutation() {
		t.Fatal("bash_output did not collect the killed job's evidence once it became ready")
	}
	if leases := ledger.BackgroundLeases(); len(leases) != 1 {
		t.Fatalf("leases = %+v, want exactly one lease recorded", leases)
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
