package schedule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/observe"
)

func claimedRun(t *testing.T) (*Store, *fakeClock, string, Run) {
	t.Helper()
	st, clk, dir := newTestStore(t)
	sc := mustCreate(t, st, DefaultPolicy(), everyReq())
	clk.Advance(time.Duration(sc.Trigger.EverySec)*time.Second + time.Second)
	run, err := st.Claim(t.Context(), DefaultPolicy(), sc.ID, slotN(sc, 1))
	if err != nil {
		t.Fatal(err)
	}
	return st, clk, dir, run
}

func TestPutResultCleansAndBoundsWhatItStores(t *testing.T) {
	st, _, _, run := claimedRun(t)
	pending := make([]observe.Pending, MaxResultPending+5)
	for i := range pending {
		pending[i] = observe.Pending{ID: fmt.Sprintf("p%d", i), Kind: observe.KindAsk, Source: "ask", Summary: "s‮", Detail: "d\x00" + strings.Repeat("y", 10<<10)}
	}
	in := Result{Report: "a\x00b‮c" + strings.Repeat("é", MaxReportBytes), Pending: pending, SessionPath: "p\x1b/",
		Posture: observe.Posture{Name: "observe", RemoteContent: true}}
	if err := st.PutResult(t.Context(), run.TriggerID, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetResult(t.Context(), run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(got.Report, "\x00‮") || len(got.Report) > MaxReportBytes || !got.ReportTruncated {
		t.Fatalf("report: %d bytes, truncated=%v, %q...", len(got.Report), got.ReportTruncated, got.Report[:8])
	}
	if strings.Contains(got.Report, "�") {
		t.Fatal("the report was cut in the middle of a character")
	}
	if len(got.Pending) != MaxResultPending || got.PendingDropped != 5 {
		t.Fatalf("pending = %d dropped %d", len(got.Pending), got.PendingDropped)
	}
	if p := got.Pending[0]; strings.ContainsAny(p.Summary+p.Detail, "\x00‮") || len(p.Detail) > maxPendingDetail || !p.Untrusted {
		t.Fatalf("pending not cleaned: %+v", p)
	}
	if got.Posture.RemoteContent || got.SessionPath != "" {
		t.Fatalf("posture/session = %+v %q", got.Posture, got.SessionPath)
	}
}

func TestPutResultRefusesASettledOrUnknownRun(t *testing.T) {
	st, _, _, run := claimedRun(t)
	if err := st.PutResult(t.Context(), "sch_x/1", Result{}); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	if err := st.Finish(t.Context(), DefaultPolicy(), run.TriggerID, Outcome{State: RunSucceeded, Clean: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutResult(t.Context(), run.TriggerID, Result{Report: "late"}); !errors.Is(err, ErrRunSettled) {
		t.Fatalf("settled run: %v", err)
	}
	if _, err := st.GetResult(t.Context(), run.TriggerID); !errors.Is(err, ErrResultNotFound) {
		t.Fatalf("a refused result must not be stored: %v", err)
	}
}

func TestResultsAreARing(t *testing.T) {
	st, _, dir, run := claimedRun(t)
	rdir := filepath.Join(dir, resultsDirName)
	if err := os.MkdirAll(rdir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	for i := range MaxResults + 20 {
		p := filepath.Join(rdir, fmt.Sprintf("%032d.json", i))
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(p, old.Add(time.Duration(i)*time.Second), old.Add(time.Duration(i)*time.Second))
	}
	if err := st.PutResult(t.Context(), run.TriggerID, Result{Report: "new"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(rdir)
	if len(entries) != MaxResults {
		t.Fatalf("%d result files kept, want %d", len(entries), MaxResults)
	}
	if _, err := st.GetResult(t.Context(), run.TriggerID); err != nil {
		t.Fatalf("the newest result was pruned: %v", err)
	}
}

func TestRunLeaseIsExclusiveAndDiesWithItsHolder(t *testing.T) {
	st, _, _, run := claimedRun(t)
	if st.RunHeld(run) {
		t.Fatal("a fresh run reads as held")
	}
	release, err := st.HoldRun(run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.HoldRun(run.TriggerID); !errors.Is(err, ErrRunHeld) {
		t.Fatalf("second holder: %v", err)
	}
	if !st.RunHeld(run) {
		t.Fatal("a held lease reads as free")
	}
	release()
	if st.RunHeld(run) {
		t.Fatal("a released lease reads as held")
	}
}

func TestReapDeadSettlesOnlyRunsWhoseLeaseIsFree(t *testing.T) {
	st, clk, _, run := claimedRun(t)
	release, err := st.HoldRun(run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(ReapGrace + time.Minute)
	if reaped, err := st.ReapDead(t.Context(), DefaultPolicy()); err != nil || len(reaped) != 0 {
		t.Fatalf("a run with a live lease was reaped: %v %v", reaped, err)
	}
	release()
	reaped, err := st.ReapDead(t.Context(), DefaultPolicy())
	if err != nil || len(reaped) != 1 {
		t.Fatalf("ReapDead = %v, %v", reaped, err)
	}
	m, _, _ := st.Snapshot(t.Context())
	if r := m.run(run.TriggerID); r.State != RunInterrupted || r.Charged != r.PerRunCap {
		t.Fatalf("reaped run = %s charged %d", r.State, r.Charged)
	}
}

func TestLoadOverridesReadsOnlyTheScheduleTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if o, err := LoadOverrides(path); err != nil || o != (Overrides{}) {
		t.Fatalf("missing file: %+v %v", o, err)
	}
	if err := os.WriteFile(path, []byte("default_model = \"x\"\n\n[schedule]\nmax_repeat_parks = 5\nper_run_tokens = 1000\n\n[other]\nmax_repeat_parks = 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	o, err := LoadOverrides(path)
	if err != nil || o.MaxRepeatParks == nil || *o.MaxRepeatParks != 5 || *o.PerRunTokens != 1000 {
		t.Fatalf("overrides = %+v, %v", o, err)
	}
	p, _, err := Resolve(o, Overrides{})
	if err != nil || p.MaxRepeatParks != 5 {
		t.Fatalf("policy = %+v, %v", p, err)
	}
	if err := os.WriteFile(path, []byte("[schedule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOverrides(path); !errors.Is(err, ErrPolicyInvalid) {
		t.Fatalf("a file that does not parse must refuse, not fall back: %v", err)
	}
}

func TestProjectCannotRaiseTheRepeatParkLimit(t *testing.T) {
	p, ign, err := Resolve(Overrides{}, Overrides{MaxRepeatParks: new(int64(50))})
	if err != nil || p.MaxRepeatParks != 3 || len(ign) != 1 {
		t.Fatalf("policy %+v ignored %v err %v", p, ign, err)
	}
	p, _, _ = Resolve(Overrides{}, Overrides{MaxRepeatParks: new(int64(2))})
	if p.MaxRepeatParks != 2 {
		t.Fatalf("a project may tighten it: %+v", p)
	}
}

func TestStartAdmitsOnlyTheTokenHolderOnce(t *testing.T) {
	st, _, dir, run := claimedRun(t)
	if err := st.Start(t.Context(), run.TriggerID, "x"); !errors.Is(err, ErrRunSettled) {
		t.Fatalf("a claimed run cannot start: %v", err)
	}
	token, err := st.MarkRunning(t.Context(), run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, manifestName))
	if strings.Contains(string(raw), token) {
		t.Fatal("the token itself is in the manifest")
	}
	if err := st.Start(t.Context(), run.TriggerID, "guess"); !errors.Is(err, ErrRunToken) {
		t.Fatalf("wrong token: %v", err)
	}
	if err := st.Start(t.Context(), run.TriggerID, token); err != nil {
		t.Fatal(err)
	}
	if err := st.Start(t.Context(), run.TriggerID, token); !errors.Is(err, ErrRunStarted) {
		t.Fatalf("second start: %v", err)
	}
}

func TestSupervisionKeepsARunAliveAcrossTheExecutorLeaving(t *testing.T) {
	st, clk, _, run := claimedRun(t)
	release, err := st.HoldSupervision(run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.HoldSupervision(run.TriggerID); !errors.Is(err, ErrRunHeld) {
		t.Fatalf("second supervisor: %v", err)
	}
	clk.Advance(ReapGrace + time.Minute)
	if reaped, _ := st.ReapDead(t.Context(), DefaultPolicy()); len(reaped) != 0 {
		t.Fatal("a supervised run was reaped")
	}
	release()
	if reaped, _ := st.ReapDead(t.Context(), DefaultPolicy()); len(reaped) != 1 {
		t.Fatal("a run nobody holds was not reaped")
	}
}

func TestSessionPathMustLieUnderTheStateRoot(t *testing.T) {
	st, _, dir, run := claimedRun(t)
	inside := filepath.Join(filepath.Dir(dir), "projects", "p", "sessions")
	if err := os.MkdirAll(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(inside, "a.jsonl"):                 filepath.Join(inside, "a.jsonl"),
		filepath.Join(t.TempDir(), "a.jsonl"):            "",
		filepath.Join(filepath.Dir(dir), "..", "x.json"): "",
		"relative.jsonl":                                 "",
	} {
		if got := st.sessionPathOrEmpty(path); got != want {
			t.Errorf("sessionPathOrEmpty(%q) = %q, want %q", path, got, want)
		}
	}
	if err := st.PutResult(t.Context(), run.TriggerID, Result{Report: "x"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetResult(t.Context(), run.TriggerID); !got.ReportUntrusted {
		t.Fatal("a report must be marked untrusted")
	}
}
