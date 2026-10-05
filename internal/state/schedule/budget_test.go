package schedule

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestChargeForIsPessimistic(t *testing.T) {
	const cap = 250_000
	for _, tc := range []struct {
		name string
		o    Outcome
		want int64
	}{
		{"clean uses observed", Outcome{State: RunSucceeded, Observed: 10_000, Clean: true}, 10_000},
		{"killed charges cap", Outcome{State: RunBudgetStopped, Observed: 10_000}, cap},
		{"crash charges cap", Outcome{State: RunInterrupted}, cap},
		{"cut stream charges cap", Outcome{State: RunFailed, Observed: 1, Clean: false}, cap},
		{"observed above cap is not hidden", Outcome{State: RunBudgetStopped, Observed: cap + 5, Clean: true}, cap + 5},
		{"observed above cap when killed", Outcome{State: RunBudgetStopped, Observed: cap * 2}, cap * 2},
	} {
		if got := ChargeFor(cap, tc.o); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}

func claimAndFinish(t *testing.T, st *Store, p Policy, sc Schedule, n int64, o Outcome) error {
	t.Helper()
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, n)); err != nil {
		return err
	}
	return st.Finish(t.Context(), p, TriggerID(sc.ID, slotN(sc, n)), o)
}

func TestGlobalDayWindowReservesFullCap(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	p.GlobalTokensDay = 2*p.PerRunTokens + 1
	sc := mustCreate(t, st, p, everyReq())
	clean := Outcome{State: RunSucceeded, Observed: 1, Clean: true}
	for n := int64(1); n <= 2; n++ {
		clk.Advance(time.Hour)
		if err := claimAndFinish(t, st, p, sc, n, clean); err != nil {
			t.Fatal(err)
		}
	}
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3)); err != nil {
		t.Fatalf("2 tiny runs settled at 1 token leave room for a full reservation: %v", err)
	}
}

func TestKilledRunsExhaustTheDailyWindow(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	killed := Outcome{State: RunBudgetStopped, Observed: 5}
	var refused error
	for n := int64(1); n <= 6 && refused == nil; n++ {
		clk.Advance(time.Hour)
		refused = claimAndFinish(t, st, p, sc, n, killed)
	}
	if !errors.Is(refused, ErrBudget) && !errors.Is(refused, ErrNotActive) {
		t.Fatalf("killed runs charged at the full cap must stop the schedule, got %v", refused)
	}
}

func TestGlobalWindowsRollAndCapTokens(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	p.MaxConsecutiveFails = 100
	p.GlobalTokensDay = 2 * p.PerRunTokens
	p.MaxRunsPerScheduleDay = 100
	p.MaxRunsGlobalDay = 100
	b := p.DefaultBudget()
	b.MaxRunsPerDay = 100
	b.LifetimeTokens = p.LifetimeTokens
	req := everyReq()
	req.Budget = &b
	sc := mustCreate(t, st, p, req)
	full := Outcome{State: RunSucceeded, Observed: p.PerRunTokens, Clean: true}
	clk.Advance(time.Hour)
	if err := claimAndFinish(t, st, p, sc, 1, full); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	if err := claimAndFinish(t, st, p, sc, 2, full); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3)); !errors.Is(err, ErrBudget) {
		t.Fatalf("24h token window must refuse: %v", err)
	}
	clk.Advance(23 * time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 26)); err != nil {
		t.Fatalf("the window rolls: %v", err)
	}
}

func TestWeeklyWindow(t *testing.T) {
	m := Manifest{}
	p := DefaultPolicy()
	p.GlobalTokensWeek = 300_000
	sc := Schedule{ID: "sch_w", Status: StatusActive, Budget: p.DefaultBudget(), ExpiresAt: epoch.Add(Week * 10),
		Trigger: Trigger{Kind: TriggerEvery, EverySec: 3600}}
	m.Runs = []Run{{ScheduleID: "other", ClaimedAt: epoch.Add(-3 * Day), State: RunSucceeded, Charged: 100_000}}
	if err := Admit(p, m, sc, epoch); !errors.Is(err, ErrBudget) {
		t.Fatalf("100k+250k reserve > 300k weekly: %v", err)
	}
	if err := Admit(p, m, sc, epoch.Add(5*Day)); err != nil {
		t.Fatalf("run fell out of the 7d window: %v", err)
	}
}

func TestLifetimeCircuitBreaker(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	p.GlobalTokensDay = 100_000_000
	p.GlobalTokensWeek = 100_000_000
	p.MaxRunsGlobalDay = 100
	p.MaxRunsPerScheduleDay = 100
	b := p.DefaultBudget()
	b.MaxRunsPerDay = 100
	b.LifetimeTokens = 2*p.PerRunTokens + 10
	req := everyReq()
	req.Budget = &b
	sc := mustCreate(t, st, p, req)
	full := Outcome{State: RunSucceeded, Observed: p.PerRunTokens, Clean: true}
	for n := int64(1); n <= 2; n++ {
		clk.Advance(time.Hour)
		if err := claimAndFinish(t, st, p, sc, n, full); err != nil {
			t.Fatal(err)
		}
	}
	m, _, _ := st.Snapshot(t.Context())
	got := m.Schedules[0]
	if got.Status != StatusCircuitOpen || got.PausedReason != PauseLifetime {
		t.Fatalf("breaker did not open: %+v", got)
	}
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3)); !errors.Is(err, ErrNotActive) {
		t.Fatalf("open circuit must refuse: %v", err)
	}
	if err := st.Resume(t.Context(), sc.ID); !errors.Is(err, ErrLifetimeSpent) {
		t.Fatalf("resume must not reopen a spent schedule: %v", err)
	}
	m, _, _ = st.Snapshot(t.Context())
	if m.Schedules[0].Status != StatusCircuitOpen {
		t.Fatalf("status must stay circuit-open: %s", m.Schedules[0].Status)
	}
}

func TestConsecutiveFailureBreaker(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	p.GlobalTokensDay = 100_000_000
	p.GlobalTokensWeek = 100_000_000
	p.MaxRunsGlobalDay = 100
	p.MaxRunsPerScheduleDay = 100
	b := p.DefaultBudget()
	b.MaxRunsPerDay = 100
	b.LifetimeTokens = p.LifetimeTokens
	req := everyReq()
	req.Budget = &b
	sc := mustCreate(t, st, p, req)
	fail := Outcome{State: RunFailed, Observed: 10, Clean: true}
	for n := int64(1); n <= 3; n++ {
		clk.Advance(time.Hour)
		if err := claimAndFinish(t, st, p, sc, n, fail); err != nil {
			t.Fatal(err)
		}
	}
	m, _, _ := st.Snapshot(t.Context())
	if m.Schedules[0].Status != StatusCircuitOpen || m.Schedules[0].PausedReason != PauseFailures {
		t.Fatalf("got %+v", m.Schedules[0])
	}
}

func TestConcurrencyAndSettleErrors(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 2)); !errors.Is(err, ErrConcurrency) {
		t.Fatalf("one in-flight run machine-wide: %v", err)
	}
	id := TriggerID(sc.ID, slotN(sc, 1))
	if _, err := st.MarkRunning(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	ok := Outcome{State: RunSucceeded, Observed: 1, Clean: true}
	if err := st.Finish(t.Context(), p, id, ok); err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), p, id, ok); !errors.Is(err, ErrRunSettled) {
		t.Fatalf("double settle: %v", err)
	}
	if err := st.Finish(t.Context(), p, "sch_x/1", ok); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	if err := st.Finish(t.Context(), p, id, Outcome{State: RunClaimed}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-settling state: %v", err)
	}
}

func TestPruneNeverDropsChargedRecordInWindow(t *testing.T) {
	m := &Manifest{}
	m.Runs = append(m.Runs, Run{TriggerID: "s/charged", ScheduleID: "s", ClaimedAt: epoch.Add(-3 * Day), State: RunSucceeded, Charged: 250_000})
	for i := range 600 {
		m.Runs = append(m.Runs, Run{TriggerID: fmt.Sprintf("s/skip%d", i), ScheduleID: "s", ClaimedAt: epoch.Add(-time.Duration(i) * time.Minute), State: RunSkipped})
	}
	prune(m, epoch)
	skips := 0
	for _, r := range m.Runs {
		if r.State == RunSkipped {
			skips++
		}
	}
	if skips > MaxSkips {
		t.Fatalf("skips are limited on their own: %d", skips)
	}
	if m.run("s/charged") == nil {
		t.Fatal("skips crowded a charged record out of the weekly window")
	}
	if got := windowUsage(m.Runs, epoch, Week, "").tokens; got != 250_000 {
		t.Fatalf("weekly usage lost the record: %d", got)
	}
}

func TestPruneOverCapDropsOnlySettledOutsideWindow(t *testing.T) {
	m := &Manifest{}
	m.Runs = append(m.Runs, Run{TriggerID: "s/live", State: RunRunning, ClaimedAt: epoch.Add(-10 * Day)})
	for i := range MaxRuns + 5 {
		age := 10 * Day
		if i >= MaxRuns {
			age = time.Hour
		}
		m.Runs = append(m.Runs, Run{TriggerID: fmt.Sprintf("s/%d", i), ClaimedAt: epoch.Add(-age), State: RunSucceeded, Charged: 1})
	}
	prune(m, epoch)
	if m.run("s/live") == nil {
		t.Fatal("in-flight record dropped")
	}
	for i := MaxRuns; i < MaxRuns+5; i++ {
		if m.run(fmt.Sprintf("s/%d", i)) == nil {
			t.Fatal("in-window record dropped")
		}
	}
	if len(m.Runs) != MaxRuns {
		t.Fatalf("len=%d", len(m.Runs))
	}
	m.Runs[1].ClaimedAt = epoch.Add(-RunRetention - time.Hour)
	before := len(m.Runs)
	prune(m, epoch)
	if len(m.Runs) != before-1 {
		t.Fatal("retention should drop the aged record")
	}
}

func TestWeekWindowBoundary(t *testing.T) {
	p := DefaultPolicy()
	p.GlobalTokensWeek = 300_000
	sc := Schedule{ID: "sch_w", Status: StatusActive, Budget: p.DefaultBudget(), ExpiresAt: epoch.Add(Week * 10),
		Trigger: Trigger{Kind: TriggerEvery, EverySec: 3600}}
	at := func(claimed time.Time) error {
		m := Manifest{Runs: []Run{{ScheduleID: "other", ClaimedAt: claimed, State: RunSucceeded, Charged: 100_000}}}
		return Admit(p, m, sc, epoch)
	}
	if err := at(epoch.Add(-Week)); !errors.Is(err, ErrBudget) {
		t.Fatalf("a run exactly 7d old still counts: %v", err)
	}
	if err := at(epoch.Add(-Week - time.Second)); err != nil {
		t.Fatalf("7d+1s old is outside the window: %v", err)
	}
}

func TestShouldReapWithBackwardClock(t *testing.T) {
	r := Run{State: RunClaimed, ClaimedAt: epoch}
	back := epoch.Add(-time.Hour)
	if !ShouldReap(r, false, back) {
		t.Fatal("negative age with a free lease must be reaped, or the global concurrency cap wedges")
	}
	if ShouldReap(r, true, back) {
		t.Fatal("a held lease is alive")
	}
	if ShouldReap(r, false, epoch.Add(time.Minute)) {
		t.Fatal("inside the grace")
	}
}

func TestReapCallbackMayTakeTheStoreLock(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); err != nil {
		t.Fatal(err)
	}
	clk.Advance(ReapGrace)
	done := make(chan error, 1)
	go func() {
		_, err := st.Reap(t.Context(), p, func(Run) bool {
			_, _, err := st.Snapshot(t.Context())
			return err != nil
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Reap called back under the transaction lock")
	}
}

func TestPolicyTighteningIsIdentifiable(t *testing.T) {
	sc := Schedule{ID: "s", Status: StatusActive, Budget: DefaultPolicy().DefaultBudget(), ExpiresAt: epoch.Add(Day),
		Trigger: Trigger{Kind: TriggerEvery, EverySec: 3600}}
	p := DefaultPolicy()
	p.PerRunTokens /= 2
	if err := Admit(p, Manifest{}, sc, epoch); !errors.Is(err, ErrPolicyTightened) || !errors.Is(err, ErrBudget) {
		t.Fatalf("got %v", err)
	}
}

func TestClaimOnlyTheLatestSlot(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(30 * time.Minute)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrNotDue) {
		t.Fatalf("future slot: %v", err)
	}
	clk.Advance(5 * time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 2)); !errors.Is(err, ErrSlotStale) {
		t.Fatalf("a long-missed slot must not be made up: %v", err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 5)); err != nil {
		t.Fatalf("latest slot: %v", err)
	}
}

func TestReapSkipsRunThatStartedDuringCallback(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	run, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1))
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(ReapGrace)
	got, err := st.Reap(t.Context(), p, func(r Run) bool {
		if _, err := st.MarkRunning(t.Context(), r.TriggerID); err != nil {
			t.Error(err)
		}
		return false
	})
	if err != nil || len(got) != 0 {
		t.Fatalf("a run that took its lease during the callback was reaped: %v %v", got, err)
	}
	m, _, _ := st.Snapshot(t.Context())
	if r := m.run(run.TriggerID); r.State != RunRunning || r.Charged != run.PerRunCap {
		t.Fatalf("live run disturbed: %+v", r)
	}
}
