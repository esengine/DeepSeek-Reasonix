package schedrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/state/schedule"
)

var epoch = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func testPolicy() schedule.Policy {
	p, _, err := schedule.Resolve(schedule.Overrides{
		MinIntervalMinutes: new(int64(10)), PerRunTokens: new(int64(10_000)), PerRunWallSeconds: new(int64(1)),
	}, schedule.Overrides{})
	if err != nil {
		panic(err)
	}
	return p
}

type fixture struct {
	store *schedule.Store
	clk   *clock
	dir   string
	sc    schedule.Schedule
	run   schedule.Run
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "schedules")
	clk := &clock{t: epoch}
	base, err := schedule.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := base.WithClock(clk.Now)
	ws := t.TempDir()
	sc, err := store.Create(t.Context(), testPolicy(), schedule.CreateRequest{
		Trigger: schedule.Trigger{Kind: schedule.TriggerEvery, EverySec: 600}, Target: schedule.Target{Workspace: ws},
		Prompt: "look around", Model: schedule.Model{Provider: "p", Model: "m"}, ConfirmedBy: schedule.ConfirmedByHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(601 * time.Second)
	slot, _ := sc.Trigger.LatestSlot(clk.Now())
	run, err := store.Claim(t.Context(), testPolicy(), sc.ID, slot)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{store: store, clk: clk, dir: dir, sc: sc, run: run}
}

// ctx bounds a run under test: a supervisor that failed to end a child must fail
// the test, not hang it.
func (f *fixture) ctx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (f *fixture) supervisor(mode, mark string, extra ...string) *Supervisor {
	return &Supervisor{Store: f.store, Policy: testPolicy(), WallGrace: 300 * time.Millisecond,
		Command: childCommand(mode, f.dir, f.run.TriggerID, mark, extra...)}
}

func (f *fixture) settled(t *testing.T) schedule.Run {
	t.Helper()
	m, _, err := f.store.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range m.Runs {
		if r.TriggerID == f.run.TriggerID {
			return r
		}
	}
	t.Fatal("run vanished")
	return schedule.Run{}
}

func (f *fixture) waitFree(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for f.store.RunHeld(f.run) {
		if time.Now().After(deadline) {
			t.Fatal("the child still holds its lease: it was not ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWallClockKillsAChildThatIgnoresSignals(t *testing.T) {
	f := newFixture(t)
	start := time.Now()
	rep, err := f.supervisor("sleep", "").Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, ErrWallLimit) || rep.Code != CodeWallLimit || !rep.Killed {
		t.Fatalf("Run = %+v, %v; want a wall-limit kill", rep, err)
	}
	if took := time.Since(start); took > 15*time.Second {
		t.Fatalf("the kill took %s", took)
	}
	f.waitFree(t)
	got := f.settled(t)
	if got.State != schedule.RunBudgetStopped || got.Charged != got.PerRunCap {
		t.Fatalf("settled = %s charged %d, want budget_stopped at the whole cap %d", got.State, got.Charged, got.PerRunCap)
	}
}

func TestTokenCeilingKillsAChildThatKeepsSpending(t *testing.T) {
	f := newFixture(t)
	sup := f.supervisor("flood", "", "SCHEDRUN_CHUNK=4000")
	sup.WallGrace = 30 * time.Second
	rep, err := sup.Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, ErrTokenLimit) || rep.Code != CodeTokenLimit || !rep.Killed {
		t.Fatalf("Run = %+v, %v; want a token-limit kill", rep, err)
	}
	f.waitFree(t)
	got := f.settled(t)
	want := max(got.PerRunCap, got.Observed)
	if got.State != schedule.RunBudgetStopped || got.Charged != want {
		t.Fatalf("settled = %s charged %d observed %d, want the whole cap or what was seen if more (%d)", got.State, got.Charged, got.Observed, want)
	}
	if got.Observed <= got.PerRunCap+got.PerRunCap/4 || got.Observed > got.PerRunCap*2 {
		t.Fatalf("observed %d: the kill line is 1.25 times the cap %d", got.Observed, got.PerRunCap)
	}
}

func TestACleanRunChargesWhatItUsedAndStoresACleanResult(t *testing.T) {
	f := newFixture(t)
	rep, err := f.supervisor("ok", "").Run(f.ctx(t), f.run.TriggerID)
	if err != nil || rep.State != schedule.RunSucceeded || !rep.Clean || rep.Observed != 1000 {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	got := f.settled(t)
	if got.State != schedule.RunSucceeded || got.Charged != 1000 {
		t.Fatalf("settled = %s charged %d, want succeeded at what it used", got.State, got.Charged)
	}
	res, err := f.store.GetResult(t.Context(), f.run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(res.Report, "\x00‮") || !strings.Contains(res.Report, "found 3 TODOs") {
		t.Fatalf("report was not cleaned: %q", res.Report)
	}
	if len(res.Pending) != 1 || strings.ContainsRune(res.Pending[0].Detail, 0x1b) || !res.Pending[0].Untrusted {
		t.Fatalf("pending = %+v", res.Pending)
	}
	if res.Posture.RemoteContent {
		t.Fatal("a stored report must not claim remote content is rendered")
	}
	if !res.ReportUntrusted {
		t.Fatal("a stored report is model text and must be marked untrusted")
	}
}

func TestACrashedChildIsChargedTheWholeCap(t *testing.T) {
	f := newFixture(t)
	rep, err := f.supervisor("crash", "").Run(f.ctx(t), f.run.TriggerID)
	if err != nil || rep.State != schedule.RunFailed || rep.Code != CodeCrashed || rep.Clean {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	if got := f.settled(t); got.Charged != got.PerRunCap {
		t.Fatalf("charged %d, want the whole cap %d", got.Charged, got.PerRunCap)
	}
}

func TestAChildThatBreaksTheProtocolIsKilled(t *testing.T) {
	f := newFixture(t)
	sup := f.supervisor("huge-line", "")
	sup.WallGrace = 30 * time.Second
	rep, err := sup.Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, ErrProtocol) || rep.Code != CodeProtocol {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	f.waitFree(t)
	if got := f.settled(t); got.State != schedule.RunFailed || got.Charged != got.PerRunCap {
		t.Fatalf("settled = %s charged %d", got.State, got.Charged)
	}
}

func TestCancellingTheContextKillsTheRunAndInterruptsIt(t *testing.T) {
	f := newFixture(t)
	sup := f.supervisor("sleep", "")
	sup.WallGrace = time.Minute
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for {
			if m, _, err := f.store.Snapshot(t.Context()); err == nil && m.Runs[0].State == schedule.RunRunning {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	rep, err := sup.Run(ctx, f.run.TriggerID)
	if !errors.Is(err, ErrCancelled) || rep.State != schedule.RunInterrupted {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	f.waitFree(t)
	if got := f.settled(t); got.Charged != got.PerRunCap {
		t.Fatalf("charged %d, want the whole cap", got.Charged)
	}
}

func TestARunReapedBeforeTheSupervisorStartsIsNotRun(t *testing.T) {
	f := newFixture(t)
	f.clk.Advance(schedule.ReapGrace + time.Second)
	if reaped, err := f.store.ReapDead(t.Context(), testPolicy()); err != nil || len(reaped) != 1 {
		t.Fatalf("ReapDead = %v, %v", reaped, err)
	}
	mark := filepath.Join(t.TempDir(), "mark")
	_, err := f.supervisor("ok", mark).Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, schedule.ErrRunSettled) {
		t.Fatalf("Run = %v, want ErrRunSettled", err)
	}
	if _, statErr := os.Stat(mark); statErr == nil {
		t.Fatal("a settled run was executed")
	}
}

func TestARunSettledWhileTheChildStartsSpendsNothing(t *testing.T) {
	f := newFixture(t)
	mark := filepath.Join(t.TempDir(), "mark")
	sup := f.supervisor("ok", mark)
	inner := sup.Command
	sup.Command = func(id string) (*exec.Cmd, error) {
		if err := f.store.Finish(t.Context(), testPolicy(), id, schedule.Outcome{State: schedule.RunInterrupted}); err != nil {
			return nil, err
		}
		return inner(id)
	}
	rep, err := sup.Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, schedule.ErrRunSettled) || rep.Code != CodeRunSettled {
		t.Fatalf("Run = %+v, %v; want ErrRunSettled", rep, err)
	}
	f.waitFree(t)
	time.Sleep(200 * time.Millisecond)
	if _, statErr := os.Stat(mark); statErr == nil {
		t.Fatal("the child worked after its run was settled")
	}
	if got := f.settled(t); got.State != schedule.RunInterrupted {
		t.Fatalf("state = %s, want the reaper's verdict to stand", got.State)
	}
}

func TestTwoSupervisorsOnOneClaimRunItOnce(t *testing.T) {
	f := newFixture(t)
	mark := filepath.Join(t.TempDir(), "mark")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			sup := f.supervisor("slow-ok", mark)
			sup.WallGrace = 30 * time.Second
			_, errs[i] = sup.Run(f.ctx(t), f.run.TriggerID)
		})
	}
	wg.Wait()
	ok, settled := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, schedule.ErrRunSettled), errors.Is(err, schedule.ErrRunHeld):
			settled++
		default:
			t.Fatalf("unexpected error %v", err)
		}
	}
	if ok != 1 || settled != 1 {
		t.Fatalf("ok=%d settled=%d, want exactly one execution", ok, settled)
	}
	data, _ := os.ReadFile(mark)
	if n := strings.Count(string(data), "ran"); n != 1 {
		t.Fatalf("the run executed %d times:\n%s", n, data)
	}
}

func TestChildLeavesWhenTheSupervisorCrashesAndTheRunIsReaped(t *testing.T) {
	f := newFixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(exe)
	mark := filepath.Join(t.TempDir(), "child-started")
	parent.Env = append(os.Environ(), fakeEnv+"=parent", "SCHEDRUN_STORE="+f.dir, "SCHEDRUN_ID="+f.run.TriggerID, "SCHEDRUN_CHILD_MODE=hold", "SCHEDRUN_MARK="+mark)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Process.Kill(); _ = parent.Wait() })
	deadline := time.Now().Add(20 * time.Second)
	for {
		if body, err := os.ReadFile(mark); err == nil && strings.HasPrefix(string(body), "ran ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the child never received its release line")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := f.settled(t); got.State != schedule.RunRunning || !f.store.RunHeld(got) {
		t.Fatalf("child started with state=%s held=%v", got.State, f.store.RunHeld(got))
	}
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()
	f.waitFree(t)

	f.clk.Advance(schedule.ReapGrace + time.Second)
	if reaped, err := f.store.ReapDead(t.Context(), testPolicy()); err != nil || len(reaped) != 1 {
		t.Fatalf("ReapDead = %v, %v", reaped, err)
	}
	got := f.settled(t)
	if got.State != schedule.RunInterrupted || got.Charged != got.PerRunCap {
		t.Fatalf("settled = %s charged %d, want interrupted at the whole cap %d", got.State, got.Charged, got.PerRunCap)
	}
}

func TestKillingTheRunEndsItsWholeProcessTree(t *testing.T) {
	f := newFixture(t)
	lock := filepath.Join(t.TempDir(), "grand.lock")
	sawHeld := make(chan bool, 1)
	go func() {
		for end := time.Now().Add(1200 * time.Millisecond); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if release, err := filelock.TryAcquire(lock); err != nil {
				sawHeld <- true
				return
			} else {
				release()
			}
		}
		sawHeld <- false
	}()
	rep, err := f.supervisor("tree", "", "SCHEDRUN_GRAND_LOCK="+lock).Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, ErrWallLimit) {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	if !<-sawHeld {
		t.Fatal("the grandchild never took its lock, so the test proves nothing")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		release, err := filelock.TryAcquire(lock)
		if err == nil {
			release()
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("a grandchild survived the kill of the run")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAStreamThatDisagreesWithTheChildIsChargedTheWholeCap(t *testing.T) {
	f := newFixture(t)
	rep, err := f.supervisor("lie", "").Run(f.ctx(t), f.run.TriggerID)
	if err != nil || rep.Clean {
		t.Fatalf("Run = %+v, %v; a usage count that does not match must not read as clean", rep, err)
	}
	if got := f.settled(t); got.Charged != got.PerRunCap {
		t.Fatalf("charged %d, want the whole cap %d", got.Charged, got.PerRunCap)
	}
}

func TestARunWithUnreportedUsageIsChargedTheWholeCap(t *testing.T) {
	f := newFixture(t)
	rep, err := f.supervisor("unmetered", "").Run(f.ctx(t), f.run.TriggerID)
	if err != nil || rep.Clean || rep.Code != CodeUnmetered {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	if got := f.settled(t); got.Charged != got.PerRunCap {
		t.Fatalf("charged %d, want the whole cap %d", got.Charged, got.PerRunCap)
	}
}

func TestAChildRefusedTheLeaseLeavesTheRunUnsettled(t *testing.T) {
	f := newFixture(t)
	foreign, err := f.store.HoldRun(f.run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign()
	mark := filepath.Join(t.TempDir(), "mark")
	_, err = f.supervisor("ok", mark).Run(f.ctx(t), f.run.TriggerID)
	if !errors.Is(err, ErrExecutorRefused) {
		t.Fatalf("Run = %v, want ErrExecutorRefused", err)
	}
	if got := f.settled(t); got.State != schedule.RunClaimed {
		t.Fatalf("state = %s: a supervisor whose child never held the run must not release or settle it", got.State)
	}
	if _, statErr := os.Stat(mark); statErr == nil {
		t.Fatal("work ran without the lease")
	}
}

func TestASupervisedRunIsNeverReapedBeforeItIsSettled(t *testing.T) {
	f := newFixture(t)
	f.clk.Advance(schedule.ReapGrace + time.Minute)
	sup := f.supervisor("ok", "")
	inner := sup.Command
	sup.Command = func(id string) (*exec.Cmd, error) {
		if reaped, err := f.store.ReapDead(t.Context(), testPolicy()); err != nil || len(reaped) != 0 {
			t.Errorf("a run under supervision was reaped: %v %v", reaped, err)
		}
		return inner(id)
	}
	if _, err := sup.Run(f.ctx(t), f.run.TriggerID); err != nil {
		t.Fatal(err)
	}
	if got := f.settled(t); got.State != schedule.RunSucceeded {
		t.Fatalf("state = %s", got.State)
	}
}

func TestTheStderrTailIsSanitized(t *testing.T) {
	f := newFixture(t)
	rep, _ := f.supervisor("noisy", "").Run(f.ctx(t), f.run.TriggerID)
	if !strings.Contains(rep.StderrTail, "boom") || strings.ContainsAny(rep.StderrTail, "\x1b\u202e\x00") {
		t.Fatalf("stderr tail = %q", rep.StderrTail)
	}
}
