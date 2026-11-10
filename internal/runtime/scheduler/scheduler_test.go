package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/runtime/schedrun"
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

// settler is a Runner that settles what it is given, as a supervisor would.
type settler struct {
	st    *schedule.Store
	p     schedule.Policy
	mu    sync.Mutex
	ran   []string
	state schedule.RunState
}

func (s *settler) Run(ctx context.Context, id string) (schedrun.Report, error) {
	s.mu.Lock()
	s.ran = append(s.ran, id)
	state := s.state
	s.mu.Unlock()
	if state == "" {
		state = schedule.RunSucceeded
	}
	return schedrun.Report{TriggerID: id, State: state}, s.st.Finish(ctx, s.p, id, schedule.Outcome{State: state, Observed: 1000, Clean: true})
}

func (s *settler) runs() []string { s.mu.Lock(); defer s.mu.Unlock(); return slices.Clone(s.ran) }

type rig struct {
	t   *testing.T
	st  *schedule.Store
	clk *clock
	dir string
	p   schedule.Policy
	run *settler
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := filepath.Join(testenv.TempDir(t), "schedules")
	clk := &clock{t: epoch}
	st, err := schedule.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st = st.WithClock(clk.Now)
	p := schedule.DefaultPolicy()
	return &rig{t: t, st: st, clk: clk, dir: dir, p: p, run: &settler{st: st, p: p}}
}

func (r *rig) add(every int64) schedule.Schedule {
	r.t.Helper()
	sc, err := r.st.Create(r.t.Context(), r.p, schedule.CreateRequest{
		Trigger: schedule.Trigger{Kind: schedule.TriggerEvery, EverySec: every},
		Target:  schedule.Target{Workspace: filepath.Join(os.TempDir(), "w")}, Prompt: "p",
		Model: schedule.Model{Provider: "deepseek", Model: "m"}, Grant: schedule.Grant{Tools: []string{"read_file"}},
		ConfirmedBy: schedule.ConfirmedByHuman, ToolCeiling: []string{"read_file"},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return sc
}

func (r *rig) tick() Result {
	r.t.Helper()
	res, err := (&Scheduler{Store: r.st, Policy: r.p, Runner: r.run, Now: r.clk.Now}).Tick(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	return res
}

func TestNothingDueStartsNothing(t *testing.T) {
	r := newRig(t)
	r.add(3600)
	r.clk.Advance(30 * time.Minute)
	if res := r.tick(); len(res.Reports) != 0 || len(r.run.runs()) != 0 {
		t.Fatalf("started early: %+v", res)
	}
}

func TestADueSlotWithinGraceRunsOnceAndNeverAgain(t *testing.T) {
	r := newRig(t)
	sc := r.add(3600)
	r.clk.Advance(time.Hour + time.Minute)
	if res := r.tick(); len(res.Reports) != 1 {
		t.Fatalf("result = %+v", res)
	}
	r.clk.Advance(2 * time.Minute)
	r.tick()
	want := schedule.TriggerID(sc.ID, time.Unix(sc.Trigger.Anchor.Unix()+3600, 0).UTC())
	if got := r.run.runs(); len(got) != 1 || got[0] != want {
		t.Fatalf("ran %v, want only %s", got, want)
	}
}

func TestASlotPastItsGraceIsSkippedNotMadeUp(t *testing.T) {
	r := newRig(t)
	sc := r.add(3600)
	r.clk.Advance(7*24*time.Hour + 40*time.Minute)
	res := r.tick()
	if len(r.run.runs()) != 0 || len(res.Skipped) != 1 || res.Skipped[0].Reason != schedule.SkipMissed || res.Skipped[0].ScheduleID != sc.ID {
		t.Fatalf("result = %+v ran = %v", res, r.run.runs())
	}
	if again := r.tick(); len(again.Skipped) != 0 || len(again.Reports) != 0 {
		t.Fatalf("the same slot was judged twice: %+v", again)
	}
}

func TestTheGraceIsAtMostAQuarterOfTheInterval(t *testing.T) {
	r := newRig(t)
	r.add(3600)
	r.clk.Advance(time.Hour + 16*time.Minute)
	if res := r.tick(); len(res.Reports) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("a slot 16 minutes late under a 15 minute grace ran: %+v", res)
	}
	if g := graceFor(schedule.Schedule{Trigger: schedule.Trigger{Kind: schedule.TriggerEvery, EverySec: 600}}); g != 150*time.Second {
		t.Fatalf("grace for a 10 minute interval = %s", g)
	}
}

func TestTheGlobalPauseClaimsNothing(t *testing.T) {
	r := newRig(t)
	r.add(3600)
	if err := r.st.SetPausedAll(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(time.Hour + time.Minute)
	if res := r.tick(); !res.Paused || len(r.run.runs()) != 0 {
		t.Fatalf("result = %+v", res)
	}
	m, _, _ := r.st.Snapshot(t.Context())
	if len(m.Runs) != 0 {
		t.Fatalf("a paused tick left %d run record(s)", len(m.Runs))
	}
}

func TestOneRunAtATimeAndTheOtherWaitsForALaterTick(t *testing.T) {
	r := newRig(t)
	a, b := r.add(3600), r.add(3600)
	r.clk.Advance(time.Hour + time.Minute)
	r.tick()
	if got := r.run.runs(); len(got) != 1 {
		t.Fatalf("two schedules due, ran %v", got)
	}
	r.clk.Advance(time.Minute)
	r.tick()
	got := r.run.runs()
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("the second schedule did not run on the next tick: %v (a=%s b=%s)", got, a.ID, b.ID)
	}
}

func TestAnInFlightRunBlocksNewClaimsWithoutSkippingTheWaitingSlot(t *testing.T) {
	r := newRig(t)
	a := r.add(3600)
	b := r.add(3600)
	r.clk.Advance(time.Hour + time.Minute)
	if _, err := r.st.Claim(t.Context(), r.p, a.ID, time.Unix(a.Trigger.Anchor.Unix()+3600, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	res := r.tick()
	if len(r.run.runs()) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("result = %+v ran = %v", res, r.run.runs())
	}
	m, _, _ := r.st.Snapshot(t.Context())
	for _, run := range m.Runs {
		if run.ScheduleID == b.ID {
			t.Fatalf("the waiting schedule was recorded early: %+v", run)
		}
	}
}

func TestABudgetThatDoesNotFitSkipsTheSlotWithItsReason(t *testing.T) {
	r := newRig(t)
	sc, err := r.st.Create(t.Context(), r.p, schedule.CreateRequest{
		Trigger: schedule.Trigger{Kind: schedule.TriggerEvery, EverySec: 3600},
		Target:  schedule.Target{Workspace: filepath.Join(os.TempDir(), "w")}, Prompt: "p",
		Model: schedule.Model{Provider: "deepseek", Model: "m"}, Grant: schedule.Grant{Tools: []string{"read_file"}},
		Budget:      &schedule.Budget{PerRunTokens: 100_000, PerRunWallSec: 600, PerRunSteps: 10, MaxRunsPerDay: 1, LifetimeTokens: 1_000_000},
		ConfirmedBy: schedule.ConfirmedByHuman, ToolCeiling: []string{"read_file"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(time.Hour + time.Minute)
	r.tick()
	r.clk.Advance(time.Hour)
	res := r.tick()
	if len(r.run.runs()) != 1 || len(res.Skipped) != 1 || res.Skipped[0].Reason != schedule.SkipBudgetWindow || res.Skipped[0].ScheduleID != sc.ID {
		t.Fatalf("result = %+v ran = %v", res, r.run.runs())
	}
}

func TestPausedAndExpiredSchedulesAreLeftAlone(t *testing.T) {
	r := newRig(t)
	paused := r.add(3600)
	if err := r.st.Pause(t.Context(), paused.ID); err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(time.Hour + time.Minute)
	if r.tick(); len(r.run.runs()) != 0 {
		t.Fatal("a paused schedule ran")
	}
	r.clk.Advance(31 * 24 * time.Hour)
	r.tick()
	if len(r.run.runs()) != 0 {
		t.Fatal("an expired schedule ran")
	}
}

func TestADeadRunIsReapedBeforeAnythingElse(t *testing.T) {
	r := newRig(t)
	sc := r.add(3600)
	r.clk.Advance(time.Hour + time.Minute)
	slot := time.Unix(sc.Trigger.Anchor.Unix()+3600, 0).UTC()
	if _, err := r.st.Claim(t.Context(), r.p, sc.ID, slot); err != nil {
		t.Fatal(err)
	}
	r.clk.Advance(schedule.ReapGrace + time.Minute)
	res := r.tick()
	if len(res.Reaped) != 1 || res.Reaped[0] != schedule.TriggerID(sc.ID, slot) {
		t.Fatalf("result = %+v", res)
	}
	m, _, _ := r.st.Snapshot(t.Context())
	if m.Runs[0].State != schedule.RunInterrupted {
		t.Fatalf("state = %s", m.Runs[0].State)
	}
}

func TestTwoTickersOnOneStoreStartOneRun(t *testing.T) {
	r := newRig(t)
	r.add(3600)
	r.clk.Advance(time.Hour + time.Minute)
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			st, _ := schedule.Open(r.dir)
			st = st.WithClock(r.clk.Now)
			run := &settler{st: st, p: r.p}
			_, _ = (&Scheduler{Store: st, Policy: r.p, Runner: run, Now: r.clk.Now}).Tick(t.Context())
			r.run.mu.Lock()
			r.run.ran = append(r.run.ran, run.runs()...)
			r.run.mu.Unlock()
		})
	}
	wg.Wait()
	if got := r.run.runs(); len(got) != 1 {
		t.Fatalf("six tickers started %d runs: %v", len(got), got)
	}
}
