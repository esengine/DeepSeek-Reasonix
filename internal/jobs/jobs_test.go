package jobs

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

type recordingSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *recordingSink) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingSink) texts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.events))
	for _, ev := range s.events {
		out = append(out, ev.Text)
	}
	return out
}

type blockingFinishedSink struct {
	mu       sync.Mutex
	events   []event.Event
	entered  chan struct{}
	released chan struct{}
	once     sync.Once
}

func (s *blockingFinishedSink) Emit(ev event.Event) {
	if strings.Contains(ev.Text, "background bash finished") {
		s.once.Do(func() { close(s.entered) })
		<-s.released
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestStartForSessionStampsJobContext(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	seen := make(chan string, 1)
	j := m.StartForSession("session-a", "task", "scoped", func(ctx context.Context, _ io.Writer) (string, error) {
		seen <- SessionFromContext(ctx)
		return "done", nil
	})
	if got := <-seen; got != "session-a" {
		t.Fatalf("job context session = %q, want session-a", got)
	}
	if res := m.WaitForSession(context.Background(), "session-a", []string{j.ID}, 5); len(res) != 1 || res[0].Status != Done {
		t.Fatalf("job result = %+v, want done", res)
	}
}

func TestJobStartObserverSeesLifetimeUntilTerminal(t *testing.T) {
	observed := make(chan (<-chan struct{}), 1)
	release := make(chan struct{})
	m := NewManager(event.Discard, WithJobStartObserver(func(done <-chan struct{}) {
		observed <- done
	}))
	t.Cleanup(m.Close)
	job := m.StartForSession("session-a", "bash", "lifetime", func(context.Context, io.Writer) (string, error) {
		<-release
		return "", nil
	})
	var lifetime <-chan struct{}
	select {
	case lifetime = <-observed:
	case <-time.After(time.Second):
		t.Fatal("job start observer was not called")
	}
	select {
	case <-lifetime:
		t.Fatal("job lifetime closed before run completed")
	default:
	}
	close(release)
	if results := m.WaitForSession(context.Background(), "session-a", []string{job.ID}, 2); len(results) != 1 || results[0].Status != Done {
		t.Fatalf("wait results = %+v", results)
	}
	select {
	case <-lifetime:
	case <-time.After(time.Second):
		t.Fatal("job lifetime did not close at terminal status")
	}
}

func TestReserveStartForSessionIsAtomic(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	const callers = 16
	start := make(chan struct{})
	releases := make(chan func(), callers)
	results := make(chan bool, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			release, _, ok := m.ReserveStartForSession("session-a", "task", 3)
			if ok {
				releases <- release
			}
			results <- ok
		}()
	}
	close(start)
	reserved := 0
	for i := 0; i < callers; i++ {
		if <-results {
			reserved++
		}
	}
	if reserved != 3 {
		t.Fatalf("concurrent reservations = %d, want exactly 3", reserved)
	}
	for i := 0; i < reserved; i++ {
		(<-releases)()
	}
	if release, running, ok := m.ReserveStartForSession("session-a", "task", 3); !ok || running != 0 {
		t.Fatalf("reservation after release = (running=%d, ok=%v), want (0, true)", running, ok)
	} else {
		release()
	}
}

func TestReserveStartForSessionCountsKilledJobUntilExit(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	exit := make(chan struct{})
	j := m.StartForSession("session-a", "task", "unwinding", func(context.Context, io.Writer) (string, error) {
		<-exit
		return "done", nil
	})
	if !m.KillForSession("session-a", j.ID) {
		t.Fatal("KillForSession did not find running job")
	}
	if release, running, ok := m.ReserveStartForSession("session-a", "task", 1); ok {
		release()
		t.Fatal("reserved a replacement while killed writer goroutine was still running")
	} else if running != 1 {
		t.Fatalf("unwinding writer count = %d, want 1", running)
	}
	close(exit)
	if res := m.WaitForSession(context.Background(), "session-a", []string{j.ID}, 5); len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("killed job result = %+v", res)
	}
	if release, running, ok := m.ReserveStartForSession("session-a", "task", 1); !ok || running != 0 {
		t.Fatalf("reservation after exit = (running=%d, ok=%v), want (0, true)", running, ok)
	} else {
		release()
	}
}

func TestLeaseEvidenceWaitsForKilledJobExit(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	exit := make(chan struct{})
	j := m.StartForSession("session-a", "task", "partial writer", func(ctx context.Context, _ io.Writer) (string, error) {
		<-exit
		PublishEvidence(ctx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"partial.go"},
		}}})
		return "stopped", nil
	})
	if !m.KillForSession("session-a", j.ID) {
		t.Fatal("KillForSession did not find running job")
	}
	if early := m.LeaseEvidenceForSession("session-a", j.ID); len(early.Receipts) != 0 {
		t.Fatalf("collected evidence before killed job exited: %+v", early)
	}
	close(exit)
	if res := m.WaitForSession(context.Background(), "session-a", []string{j.ID}, 5); len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("killed job result = %+v", res)
	}
	if got := m.LeaseEvidenceForSession("session-a", j.ID); !got.HasMutation() {
		t.Fatalf("partial evidence lost after killed job exit: %+v", got)
	}
	// Lease does not consume: a second lease still returns the receipts, and a
	// commit is required to drain them.
	if again := m.LeaseEvidenceForSession("session-a", j.ID); !again.HasMutation() {
		t.Fatalf("lease consumed evidence without a commit: %+v", again)
	}
	m.CommitEvidenceForSession("session-a", j.ID)
	if after := m.LeaseEvidenceForSession("session-a", j.ID); len(after.Receipts) != 0 {
		t.Fatalf("committed evidence still leasable: %+v", after)
	}
}

func TestTryLeaseEvidenceForSessionReportsReadiness(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	exit := make(chan struct{})
	j := m.StartForSession("session-a", "task", "partial writer", func(ctx context.Context, _ io.Writer) (string, error) {
		<-exit
		PublishEvidence(ctx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"partial.go"},
		}}})
		return "stopped", nil
	})
	if _, ready := m.TryLeaseEvidenceForSession("session-a", "no-such-job"); ready {
		t.Fatal("unknown job reported ready")
	}
	if _, ready := m.TryLeaseEvidenceForSession("session-a", j.ID); ready {
		t.Fatal("running job reported ready before it reached a terminal state")
	}
	if !m.KillForSession("session-a", j.ID) {
		t.Fatal("KillForSession did not find running job")
	}
	// Killed flips the status synchronously but the goroutine has not exited yet
	// (still blocked on exit): must not report ready.
	if _, ready := m.TryLeaseEvidenceForSession("session-a", j.ID); ready {
		t.Fatal("killed-but-unwinding job reported ready before its evidence was flushed")
	}
	close(exit)
	if res := m.WaitForSession(context.Background(), "session-a", []string{j.ID}, 5); len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("killed job result = %+v", res)
	}
	summary, ready := m.TryLeaseEvidenceForSession("session-a", j.ID)
	if !ready || !summary.HasMutation() {
		t.Fatalf("ready/summary after exit = %v/%+v, want ready with the published mutation", ready, summary)
	}
	m.CommitEvidenceForSession("session-a", j.ID)
	if summary, ready := m.TryLeaseEvidenceForSession("session-a", j.ID); !ready || len(summary.Receipts) != 0 {
		t.Fatalf("post-commit ready/summary = %v/%+v, want ready with no receipts", ready, summary)
	}
}

func TestPendingEvidenceJobIDsForSession(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	running := m.StartForSession("session-a", "task", "still going", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	mutator := m.StartForSession("session-a", "task", "writer", func(ctx context.Context, _ io.Writer) (string, error) {
		PublishEvidence(ctx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"changed.go"},
		}}})
		return "done", nil
	})
	readOnly := m.StartForSession("session-a", "task", "reader", func(context.Context, io.Writer) (string, error) {
		return "no changes", nil
	})
	otherSession := m.StartForSession("session-b", "task", "writer", func(ctx context.Context, _ io.Writer) (string, error) {
		PublishEvidence(ctx, evidence.ChildEvidenceSummary{Receipts: []evidence.Receipt{{
			ToolName: "write_file", Success: true, Mutation: true, Paths: []string{"other.go"},
		}}})
		return "done", nil
	})
	if res := m.WaitForSession(context.Background(), "session-a", []string{mutator.ID, readOnly.ID}, 5); len(res) != 2 {
		t.Fatalf("wait = %+v, want mutator and read-only jobs done", res)
	}
	if res := m.WaitForSession(context.Background(), "session-b", []string{otherSession.ID}, 5); len(res) != 1 {
		t.Fatalf("wait = %+v, want other-session job done", res)
	}

	pending := m.PendingEvidenceJobIDsForSession("session-a")
	if len(pending) != 1 || pending[0] != mutator.ID {
		t.Fatalf("pending = %v, want only %q (running job excluded, read-only job has no receipts, other session excluded)", pending, mutator.ID)
	}

	m.CommitEvidenceForSession("session-a", mutator.ID)
	if pending := m.PendingEvidenceJobIDsForSession("session-a"); len(pending) != 0 {
		t.Fatalf("pending after commit = %v, want none", pending)
	}
	_ = running // still running when the test ends; Close() cancels and reaps it
}

func TestStalledWarningIgnoresReturnedJobBeforeTerminalStatusPublished(t *testing.T) {
	sink := &blockingFinishedSink{entered: make(chan struct{}), released: make(chan struct{})}
	m := NewManager(sink, WithStalledWarningAfter(20*time.Millisecond))
	defer func() {
		close(sink.released)
		m.Close()
	}()

	j := m.Start("bash", "", func(context.Context, io.Writer) (string, error) {
		return "", nil
	})
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("completion notice did not start")
	}

	time.Sleep(50 * time.Millisecond)
	note := m.DrainCompletedNote()
	if strings.Contains(note, "may be stalled") {
		t.Fatalf("got false stalled warning for already-returned job %s: %q", j.ID, note)
	}
	if !strings.Contains(note, j.ID) || !strings.Contains(note, string(Done)) {
		t.Fatalf("completion note = %q, want done update for %s", note, j.ID)
	}
}

// A job runs to completion: Wait reports Done with its output, and the completion
// note drains exactly once.
func TestStartWaitDoneAndDrain(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.Start("bash", "echo", func(_ context.Context, out io.Writer) (string, error) {
		io.WriteString(out, "hello\n")
		return "", nil
	})
	res := m.Wait(context.Background(), []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Done {
		t.Fatalf("want one Done result, got %+v", res)
	}
	if !strings.Contains(res[0].Output, "hello") {
		t.Errorf("output = %q, want it to contain hello", res[0].Output)
	}
	note := m.DrainCompletedNote()
	if !strings.Contains(note, j.ID) {
		t.Errorf("note = %q, want it to mention %s", note, j.ID)
	}
	if again := m.DrainCompletedNote(); again != "" {
		t.Errorf("second drain = %q, want empty", again)
	}
}

// Output returns only the bytes produced since the previous read.
func TestOutputStreamsIncrementally(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	j := m.Start("bash", "", func(_ context.Context, out io.Writer) (string, error) {
		io.WriteString(out, "first\n")
		<-release
		io.WriteString(out, "second\n")
		return "", nil
	})

	waitFor(t, func() bool {
		txt, _, _ := m.Output(j.ID)
		return strings.Contains(txt, "first")
	})
	close(release)
	m.Wait(context.Background(), []string{j.ID}, 5)

	txt, st, ok := m.Output(j.ID)
	if !ok || st != Done {
		t.Fatalf("Output after done: ok=%v status=%s", ok, st)
	}
	if !strings.Contains(txt, "second") || strings.Contains(txt, "first") {
		t.Errorf("incremental output = %q, want only the new 'second' chunk", txt)
	}
}

// Kill cancels a running job; a second Kill is a no-op once it has finished.
func TestKill(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.Start("bash", "", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !m.Kill(j.ID) {
		t.Fatal("Kill on a running job returned false")
	}
	res := m.Wait(context.Background(), []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("want Killed, got %+v", res)
	}
	if m.Kill(j.ID) {
		t.Error("Kill on a finished job should return false")
	}
}

func TestJobPanicRecoveredAsFailed(t *testing.T) {
	sink := &recordingSink{}
	m := NewManager(sink)
	defer m.Close()

	j := m.Start("task", "panic", func(context.Context, io.Writer) (string, error) {
		panic("boom")
	})
	res := m.Wait(context.Background(), []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Failed {
		t.Fatalf("want Failed result after panic, got %+v", res)
	}
	if !strings.Contains(res[0].Output, "internal error: panic: boom") {
		t.Fatalf("panic output = %q, want internal panic message", res[0].Output)
	}
	waitFor(t, func() bool {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		for _, ev := range sink.events {
			if strings.Contains(ev.Text, "background task failed") && strings.Contains(ev.Detail, j.ID) && strings.Contains(ev.Detail, "panic: boom") {
				return true
			}
		}
		return false
	})
}

func TestStalledWarningEmitsNoticeAndDrainNote(t *testing.T) {
	sink := &recordingSink{}
	m := NewManager(sink, WithStalledWarningAfter(20*time.Millisecond))
	defer m.Close()

	j := m.Start("bash", "quiet", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	defer m.Kill(j.ID)

	waitFor(t, func() bool {
		for _, text := range sink.texts() {
			if strings.Contains(text, "may be stalled") && strings.Contains(text, j.ID) {
				return true
			}
		}
		return false
	})
	if _, st, ok := m.Output(j.ID); !ok || st != Running {
		t.Fatalf("stalled job output status = %q ok=%v, want running", st, ok)
	}
	note := m.DrainCompletedNote()
	if !strings.Contains(note, "may be stalled") || !strings.Contains(note, j.ID) {
		t.Fatalf("stalled drain note = %q, want stalled update for %s", note, j.ID)
	}
	// The warning is once per job.
	time.Sleep(30 * time.Millisecond)
	if again := m.DrainCompletedNote(); again != "" {
		t.Fatalf("second stalled drain note = %q, want empty", again)
	}
}

// Killed status is observable as soon as Kill returns, before the run goroutine
// unwinds — otherwise a slow cancelled process tree (Windows taskkill + WaitDelay
// drain) leaves Wait reporting Running until the goroutine finally returns, which
// is the TestBackgroundKill flake. The job here stays blocked past ctx.Done.
func TestKillStatusObservableBeforeGoroutineReturns(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	j := m.Start("bash", "", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		<-release // simulate a teardown that hasn't returned yet
		return "", ctx.Err()
	})
	if !m.Kill(j.ID) {
		t.Fatal("Kill on a running job returned false")
	}

	// Short timeout: the goroutine is still blocked, so Wait can only know the
	// status if Kill set it synchronously.
	res := m.Wait(context.Background(), []string{j.ID}, 1)
	if len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("want Killed before the goroutine returns, got %+v", res)
	}
	if n := len(m.Running()); n != 0 {
		t.Fatalf("a killed job should not still be Running(), got %d", n)
	}

	close(release)
	m.Wait(context.Background(), []string{j.ID}, 5)
}

// Close cancels every still-running job.
func TestCloseCancels(t *testing.T) {
	m := NewManager(event.Discard)

	started := make(chan struct{})
	j := m.Start("task", "", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	<-started
	m.Close()

	res := m.Wait(context.Background(), []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("want Killed after Close, got %+v", res)
	}
}

// Running reflects only in-flight jobs.
func TestRunning(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	j := m.Start("task", "label", func(ctx context.Context, _ io.Writer) (string, error) {
		<-release
		return "answer", nil
	})
	waitFor(t, func() bool { return len(m.Running()) == 1 })
	if r := m.Running()[0]; r.ID != j.ID || r.Label != "label" {
		t.Errorf("running view = %+v, want id=%s label=label", r, j.ID)
	}
	close(release)
	m.Wait(context.Background(), []string{j.ID}, 5)
	waitFor(t, func() bool { return len(m.Running()) == 0 })
}

func TestSessionScopedOperations(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	a := m.StartForSession("session-a", "bash", "a", func(_ context.Context, out io.Writer) (string, error) {
		io.WriteString(out, "from-a\n")
		<-releaseA
		return "", nil
	})
	b := m.StartForSession("session-b", "bash", "b", func(_ context.Context, out io.Writer) (string, error) {
		io.WriteString(out, "from-b\n")
		<-releaseB
		return "", nil
	})

	waitFor(t, func() bool {
		txt, _, ok := m.OutputForSession("session-a", a.ID)
		return ok && strings.Contains(txt, "from-a")
	})
	if _, _, ok := m.OutputForSession("session-a", b.ID); ok {
		t.Fatal("session-a should not read session-b output")
	}
	if got := m.RunningForSession("session-a"); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("session-a running = %+v, want only %s", got, a.ID)
	}
	if got := m.WaitForSession(context.Background(), "session-a", []string{b.ID}, 1); len(got) != 0 {
		t.Fatalf("session-a wait on session-b job = %+v, want none", got)
	}
	if m.KillForSession("session-a", b.ID) {
		t.Fatal("session-a should not kill session-b job")
	}

	close(releaseA)
	res := m.WaitForSession(context.Background(), "session-a", []string{a.ID}, 5)
	if len(res) != 1 || res[0].ID != a.ID || res[0].Status != Done {
		t.Fatalf("session-a wait all = %+v, want only done %s", res, a.ID)
	}
	if note := m.DrainCompletedNoteForSession("session-b"); note != "" {
		t.Fatalf("session-b drain before completion = %q, want empty", note)
	}
	if note := m.DrainCompletedNoteForSession("session-a"); !strings.Contains(note, a.ID) {
		t.Fatalf("session-a drain = %q, want %s", note, a.ID)
	}

	close(releaseB)
	m.WaitForSession(context.Background(), "session-b", []string{b.ID}, 5)
	if note := m.DrainCompletedNoteForSession("session-b"); !strings.Contains(note, b.ID) {
		t.Fatalf("session-b drain = %q, want %s", note, b.ID)
	}
}

func TestSessionScopedNoticesUseActiveSession(t *testing.T) {
	sink := &recordingSink{}
	m := NewManager(sink)
	defer m.Close()
	m.SetActiveSession("session-a")

	releaseA := make(chan struct{})
	releaseB := make(chan struct{})
	a := m.StartForSession("session-a", "bash", "a", func(_ context.Context, _ io.Writer) (string, error) {
		<-releaseA
		return "", nil
	})
	b := m.StartForSession("session-b", "bash", "b", func(_ context.Context, _ io.Writer) (string, error) {
		<-releaseB
		return "", nil
	})
	close(releaseB)
	m.Wait(context.Background(), []string{b.ID}, 5)
	for _, text := range sink.texts() {
		if strings.Contains(text, b.ID) {
			t.Fatalf("inactive session job notice leaked: %q", text)
		}
	}

	close(releaseA)
	m.Wait(context.Background(), []string{a.ID}, 5)
	waitFor(t, func() bool {
		for _, text := range sink.texts() {
			if strings.Contains(text, a.ID) {
				return true
			}
		}
		return false
	})
}

func TestDestroySessionCancelsOwnedJobsAndSuppressesCompletion(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	started := make(chan struct{})
	j := m.StartForSession("session-a", "task", "cleanup", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	<-started

	done := m.DestroySession("session-a")
	if len(done) != 1 {
		t.Fatalf("DestroySession returned %d done channels, want 1", len(done))
	}
	if !m.IsDestroying("session-a") {
		t.Fatal("session-a should be marked destroying")
	}
	<-done[0]
	res := m.WaitForSession(context.Background(), "session-a", []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("destroyed job result = %+v, want killed", res)
	}
	if note := m.DrainCompletedNoteForSession("session-a"); note != "" {
		t.Fatalf("destroyed session should not queue completion note, got %q", note)
	}
	m.FinishDestroySession("session-a")
	if m.IsDestroying("session-a") {
		t.Fatal("session-a should no longer be marked destroying")
	}
}

func TestDestroySessionWaitsForAlreadyKilledJobs(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	j := m.StartForSession("session-a", "task", "cleanup", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		<-release
		return "", ctx.Err()
	})
	<-started

	if !m.KillForSession("session-a", j.ID) {
		t.Fatal("KillForSession on a running job returned false")
	}
	waitFor(t, func() bool {
		_, status, ok := m.OutputForSession("session-a", j.ID)
		return ok && status == Killed
	})

	done := m.DestroySession("session-a")
	if len(done) != 1 {
		t.Fatalf("DestroySession returned %d done channels, want 1", len(done))
	}
	if !m.IsDestroying("session-a") {
		t.Fatal("session-a should be marked destroying")
	}
	select {
	case <-done[0]:
		t.Fatal("done channel closed before killed job finished unwinding")
	default:
	}

	close(release)
	select {
	case <-done[0]:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for killed job to unwind")
	}
	res := m.WaitForSession(context.Background(), "session-a", []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("destroyed job result = %+v, want killed", res)
	}
	if note := m.DrainCompletedNoteForSession("session-a"); note != "" {
		t.Fatalf("destroyed session should not queue completion note, got %q", note)
	}
	m.FinishDestroySession("session-a")
	if m.IsDestroying("session-a") {
		t.Fatal("session-a should no longer be marked destroying")
	}
}

func TestWaitTeardownTimesOutForNonCooperativeJob(t *testing.T) {
	m := NewManager(event.Discard)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		releaseJob()
		m.Close()
	}()

	started := make(chan struct{})
	j := m.StartForSession("session-a", "task", "cleanup", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		<-release
		return "", ctx.Err()
	})
	<-started

	handle := m.BeginDestroySession("session-a")
	start := time.Now()
	result := m.WaitTeardown(context.Background(), handle, 25*time.Millisecond)
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("WaitTeardown took %s, want bounded wait", elapsed)
	}
	if len(result.TimedOut) != 1 {
		t.Fatalf("timed out jobs = %+v, want one", result.TimedOut)
	}
	got := result.TimedOut[0]
	if got.ID != j.ID || got.Kind != "task" || got.Label != "cleanup" || got.Waited <= 0 {
		t.Fatalf("timed out job = %+v, want id=%s kind=task label=cleanup waited>0", got, j.ID)
	}
	if note := m.DrainCompletedNoteForSession("session-a"); note != "" {
		t.Fatalf("destroyed session should not queue completion note, got %q", note)
	}
	if !m.IsDestroying("session-a") {
		t.Fatal("session-a should stay destroying until delayed cleanup finishes")
	}

	releaseJob()
	for _, ch := range handle.DoneChannels() {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for delayed job unwind")
		}
	}
	m.FinishDestroySession("session-a")
	if m.IsDestroying("session-a") {
		t.Fatal("session-a should no longer be destroying after Finish")
	}
}

func TestCloseWithGraceTimesOutForNonCooperativeJob(t *testing.T) {
	m := NewManager(event.Discard)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		releaseJob()
		m.Close()
	}()

	started := make(chan struct{})
	j := m.Start("task", "cleanup", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		<-release
		return "", ctx.Err()
	})
	<-started

	start := time.Now()
	result := m.CloseWithGrace(25 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Fatalf("CloseWithGrace took %s, want bounded wait", elapsed)
	}
	if len(result.TimedOut) != 1 {
		t.Fatalf("timed out jobs = %+v, want one", result.TimedOut)
	}
	if got := result.TimedOut[0]; got.ID != j.ID || got.Kind != "task" || got.Label != "cleanup" || got.Waited <= 0 {
		t.Fatalf("timed out job = %+v, want id=%s kind=task label=cleanup waited>0", got, j.ID)
	}
	if running := m.Running(); len(running) != 0 {
		t.Fatalf("cancelled close jobs should not remain Running, got %+v", running)
	}

	releaseJob()
	res := m.Wait(context.Background(), []string{j.ID}, 5)
	if len(res) != 1 || res[0].Status != Killed {
		t.Fatalf("want killed after delayed close cleanup, got %+v", res)
	}
}
