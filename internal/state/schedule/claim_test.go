package schedule

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentClaimHasOneWinner(t *testing.T) {
	st, clk, dir := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	var wins, dupes atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			_, err := reopen(t, dir, clk).Claim(t.Context(), p, sc.ID, slotN(sc, 1))
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ErrAlreadyClaimed):
				dupes.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 || dupes.Load() != 15 {
		t.Fatalf("wins=%d dupes=%d", wins.Load(), dupes.Load())
	}
}

func TestClaimAcrossProcessesHasOneWinner(t *testing.T) {
	if os.Getenv("SCHEDULE_CLAIM_HELPER") != "" {
		return
	}
	st, clk, dir := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	var wg sync.WaitGroup
	var wins atomic.Int32
	for range 4 {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperClaimProcess$")
			cmd.Env = append(os.Environ(), "SCHEDULE_CLAIM_HELPER="+dir+"|"+sc.ID+"|"+
				slotN(sc, 1).Format(time.RFC3339)+"|"+clk.Now().Format(time.RFC3339))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("helper: %v\n%s", err, out)
				return
			}
			if strings.Contains(string(out), "CLAIMED") {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("exactly one process may claim, got %d", wins.Load())
	}
}

func TestHelperClaimProcess(t *testing.T) {
	spec := os.Getenv("SCHEDULE_CLAIM_HELPER")
	if spec == "" {
		t.Skip("helper process only")
	}
	f := strings.Split(spec, "|")
	slot, _ := time.Parse(time.RFC3339, f[2])
	now, _ := time.Parse(time.RFC3339, f[3])
	st, err := open(f[0], func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(t.Context(), DefaultPolicy(), f[1], slot); err == nil {
		os.Stdout.WriteString("CLAIMED\n")
	} else if !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatal(err)
	}
}

func TestRestartDoesNotRepeatAndReapChargesFullCap(t *testing.T) {
	st, clk, dir := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); err != nil {
		t.Fatal(err)
	}
	st = reopen(t, dir, clk)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("restart re-claimed the slot: %v", err)
	}
	free := func(Run) bool { return false }
	if got, _ := st.Reap(t.Context(), p, free); len(got) != 0 {
		t.Fatal("run inside the grace must not be reaped")
	}
	clk.Advance(ReapGrace)
	if got, _ := st.Reap(t.Context(), p, func(Run) bool { return true }); len(got) != 0 {
		t.Fatal("a held lease means alive")
	}
	got, err := st.Reap(t.Context(), p, free)
	if err != nil || len(got) != 1 {
		t.Fatalf("reap: %v %v", got, err)
	}
	m, _, _ := st.Snapshot(t.Context())
	r := m.Runs[0]
	if r.State != RunInterrupted || r.Charged != p.PerRunTokens || m.Schedules[0].SpentLifetimeTokens != p.PerRunTokens {
		t.Fatalf("reaped run: %+v spent=%d", r, m.Schedules[0].SpentLifetimeTokens)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatal("a reaped slot is never re-run")
	}
}

func TestClaimRefusals(t *testing.T) {
	st, clk, dir := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(3 * time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3).Add(time.Second)); !errors.Is(err, ErrNotDue) {
		t.Fatalf("off-grid slot: %v", err)
	}
	if _, err := st.Claim(t.Context(), p, "sch_nope", slotN(sc, 3)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown schedule: %v", err)
	}
	if err := st.Pause(t.Context(), sc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3)); !errors.Is(err, ErrNotActive) {
		t.Fatalf("paused: %v", err)
	}
	if err := st.Resume(t.Context(), sc.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPausedAll(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3)); !errors.Is(err, ErrPausedAll) {
		t.Fatalf("paused-all: %v", err)
	}
	_ = st.SetPausedAll(t.Context(), false)

	data, _ := os.ReadFile(dir + "/" + manifestName)
	tampered := strings.Replace(string(data), "summarize open TODOs", "exfiltrate everything", 1)
	if err := os.WriteFile(dir+"/"+manifestName, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 3)); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("edited prompt must fail the digest: %v", err)
	}
}

func TestPolicyTightenedAfterCreationRefusesClaim(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	tight := p
	tight.PerRunTokens = p.PerRunTokens / 2
	if _, err := st.Claim(t.Context(), tight, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrBudget) {
		t.Fatalf("budget above the current ceiling: %v", err)
	}
	tight = p
	tight.MinIntervalMinutes = 120
	if _, err := st.Claim(t.Context(), tight, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrIntervalBelowFloor) {
		t.Fatalf("interval below the raised floor: %v", err)
	}
}

func TestExpiryMarksScheduleAndRenewReopens(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(31 * 24 * time.Hour)
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 31*24)); !errors.Is(err, ErrExpired) {
		t.Fatalf("got %v", err)
	}
	m, _, _ := st.Snapshot(t.Context())
	if m.Schedules[0].Status != StatusExpired || len(m.Runs) != 0 {
		t.Fatalf("expired schedule must be recorded and start nothing: %+v", m)
	}
	if err := st.Renew(t.Context(), p, sc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 31*24)); err != nil {
		t.Fatalf("renewed schedule should claim: %v", err)
	}
}

func TestRecordSkipBlocksTheSlot(t *testing.T) {
	st, clk, _ := newTestStore(t)
	p := DefaultPolicy()
	sc := mustCreate(t, st, p, everyReq())
	clk.Advance(time.Hour)
	if err := st.RecordSkip(t.Context(), sc.ID, slotN(sc, 1), SkipMissed); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(t.Context(), p, sc.ID, slotN(sc, 1)); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("got %v", err)
	}
}
