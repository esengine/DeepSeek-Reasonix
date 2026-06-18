package jobs

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
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
		for _, text := range sink.texts() {
			if strings.Contains(text, "background task failed") && strings.Contains(text, j.ID) && strings.Contains(text, "panic: boom") {
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
