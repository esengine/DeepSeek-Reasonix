package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTrajectory(t *testing.T, name string, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestSummarizeOutcomeFromRecordedShadowSamples(t *testing.T) {
	path := writeTrajectory(t, "shadow.trajectory.jsonl", []string{
		`{"seq":1,"ts":500,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":1000,"outcome_progress":{"round":1,"exploration":1,"legacy_gain":1}}`,
		`{"seq":3,"ts":2000,"outcome_progress":{"round":2,"verification":1,"legacy_gain":2}}`,
		`{"seq":4,"ts":3000,"outcome_progress":{"round":3,"churn":1,"legacy_gain":3}}`,
		`{"seq":5,"ts":4000,"outcome_progress":{"round":4,"verification":1,"objective":1,"legacy_gain":2}}`,
		`{"seq":6,"ts":5000,"outcome_progress":{"round":5,"churn":1,"legacy_gain":3}}`,
		`{"seq":7,"ts":6000,"outcome_progress":{"round":6,"verification":1,"regression":1}}`,
		`{"seq":8,"ts":7000,"event":{"kind":"turn_done"}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	o := s.Outcome
	if o == nil || o.Backfilled {
		t.Fatalf("outcome = %+v, want recorded (not backfilled)", o)
	}
	if o.Rounds != 6 || o.ProgressRounds != 5 {
		t.Errorf("rounds=%d progress=%d, want 6/5", o.Rounds, o.ProgressRounds)
	}
	// Round 5 claimed legacy progress (a mutation) with no objective transition
	// inside the redemption window — the false-progress case.
	if o.FalseProgressRounds != 1 {
		t.Errorf("false progress = %d, want 1", o.FalseProgressRounds)
	}
	if o.SolutionStallMax != 2 {
		t.Errorf("solution stall max = %d, want 2", o.SolutionStallMax)
	}
	if o.Objective != 1 || o.Regression != 1 || o.BestScore != 1 || o.FinalScore != 0 {
		t.Errorf("objective=%d regression=%d best=%d final=%d, want 1/1/1/0",
			o.Objective, o.Regression, o.BestScore, o.FinalScore)
	}
	if !o.RegressedFromBest || o.SearchRegretMs != 3000 {
		t.Errorf("regressed=%v regret=%d, want true/3000 (best at ts 4000, end at 7000)",
			o.RegressedFromBest, o.SearchRegretMs)
	}
}

func TestSummarizeOutcomeBackfillsFromVerificationReceipts(t *testing.T) {
	args := `{\"command\":\"go test ./x\"}`
	path := writeTrajectory(t, "old.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"event":{"kind":"tool_result","tool":{"name":"bash","args":"` + args + `","err":"exit 1","execution":{"verification":"failed"}}}}`,
		`{"seq":3,"ts":3000,"event":{"kind":"tool_result","tool":{"name":"bash","args":"` + args + `","execution":{"verification":"passed"}}}}`,
		`{"seq":4,"ts":4000,"event":{"kind":"tool_result","tool":{"name":"bash","args":"` + args + `","err":"exit 1","execution":{"verification":"failed"}}}}`,
		`{"seq":5,"ts":5000,"event":{"kind":"turn_done"}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	o := s.Outcome
	if o == nil || !o.Backfilled {
		t.Fatalf("outcome = %+v, want a backfilled summary", o)
	}
	if o.Objective != 1 || o.Regression != 1 || o.BestScore != 1 || o.FinalScore != 0 {
		t.Errorf("objective=%d regression=%d best=%d final=%d, want 1/1/1/0",
			o.Objective, o.Regression, o.BestScore, o.FinalScore)
	}
	if !o.RegressedFromBest || o.SearchRegretMs != 2000 {
		t.Errorf("regressed=%v regret=%d, want true/2000", o.RegressedFromBest, o.SearchRegretMs)
	}
	if o.FalseProgressRounds != 0 || o.ProgressRounds != 0 {
		t.Errorf("backfill cannot price legacy claims, got progress=%d false=%d",
			o.ProgressRounds, o.FalseProgressRounds)
	}
	// A subagent's verification must not pollute the parent series.
	sub := writeTrajectory(t, "sub.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"event":{"kind":"tool_result","tool":{"name":"bash","args":"` + args + `","parentId":"task-1","execution":{"verification":"failed"}}}}`,
		`{"seq":3,"ts":3000,"event":{"kind":"turn_done"}}`,
	})
	if s, err = summarizeTrajectory(sub); err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.Outcome != nil {
		t.Errorf("subagent-only verification must yield no outcome summary, got %+v", s.Outcome)
	}
}

func TestSummarizeOutcomeTracksDebtAndTTFDC(t *testing.T) {
	path := writeTrajectory(t, "debt.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"outcome_progress":{"round":1,"churn":1,"legacy_gain":3,"debt_age":1}}`,
		`{"seq":3,"ts":3000,"outcome_progress":{"round":2,"exploration":1,"legacy_gain":1,"debt_age":2}}`,
		`{"seq":4,"ts":4000,"outcome_progress":{"round":3,"churn":1,"legacy_gain":3,"debt_age":3}}`,
		`{"seq":5,"ts":9000,"outcome_progress":{"round":4,"discriminating":1,"verification":1}}`,
		`{"seq":6,"ts":10000,"event":{"kind":"turn_done"}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	o := s.Outcome
	if o == nil || o.DebtAgeMax != 3 {
		t.Fatalf("outcome = %+v, want debt age max 3", o)
	}
	if o.TTFDCMs != 8000 {
		t.Fatalf("TTFDC = %d, want 8000 (run start 1000 → first discriminating 9000)", o.TTFDCMs)
	}
	got := renderOutcomeProgress([]result{{Trajectory: s}})
	for _, want := range []string{"**discriminating checks** in 1/1 runs (TTFDC p50 8.0s)", "**verification debt max** 3 rounds"} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in:\n%s", want, got)
		}
	}
}

func TestSummarizeRunwayShadowDistinguishesOldDataFromZeroBalance(t *testing.T) {
	path := writeTrajectory(t, "runway.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"outcome_progress":{"round":1,"runway":8,"runway_dry":4,"runway_idle":4}}`,
		`{"seq":3,"ts":3000,"outcome_progress":{"round":2,"runway":4,"runway_dry":5,"runway_idle":5}}`,
		`{"seq":4,"ts":4000,"outcome_progress":{"round":3,"runway":0,"runway_dry":6,"runway_idle":6,"runway_spent":true}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	o := s.Outcome
	if o == nil || o.RunwaySamples != 3 || o.RunwayMin != 0 || o.RunwayFinal != 0 || o.RunwayFirstSpentRound != 3 {
		t.Fatalf("runway summary = %+v, want 3 samples and spent at round 3", o)
	}
	if o.RunwayDryMax != 6 || o.RunwayIdleMax != 6 {
		t.Fatalf("runway maxima = dry %d idle %d, want 6/6", o.RunwayDryMax, o.RunwayIdleMax)
	}
	got := renderOutcomeProgress([]result{{Trajectory: s}})
	for _, want := range []string{"**runway shadow** would spend in 1/1 runs (100%)", "median round 3", "max dry/idle 6/6"} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in:\n%s", want, got)
		}
	}

	old := writeTrajectory(t, "pre-runway.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"outcome_progress":{"round":1,"exploration":1}}`,
	})
	legacy, err := summarizeTrajectory(old)
	if err != nil {
		t.Fatalf("summarize old trajectory: %v", err)
	}
	if legacy.Outcome == nil || legacy.Outcome.RunwaySamples != 0 {
		t.Fatalf("old outcome = %+v, want no runway observations", legacy.Outcome)
	}
	if strings.Contains(renderOutcomeProgress([]result{{Trajectory: legacy}}), "runway shadow") {
		t.Fatal("old trajectory was misclassified as a spent runway")
	}
}

func TestSummarizeOutcomeDerivesEBMChain(t *testing.T) {
	path := writeTrajectory(t, "ebm.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"outcome_progress":{"round":1,"churn":2,"debt_age":1,"blind_mutations":2}}`,
		`{"seq":3,"ts":3000,"outcome_progress":{"round":2,"churn":1,"debt_age":2,"blind_mutations":3,"ebm_eligible":true,"ebm_fired":true}}`,
		`{"seq":4,"ts":4000,"outcome_progress":{"round":3,"exploration":1,"debt_age":3,"blind_mutations":3}}`,
		`{"seq":5,"ts":9000,"outcome_progress":{"round":4,"discriminating":1,"verification":1}}`,
		`{"seq":6,"ts":10000,"event":{"kind":"turn_done"}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	o := s.Outcome
	if o == nil || o.EBMFiredRound != 2 || o.EBMEligibleRound != 2 {
		t.Fatalf("outcome = %+v, want EBM fired/eligible at round 2", o)
	}
	if o.EBMBlindAtFire != 3 || o.EBMDebtAgeAtFire != 2 {
		t.Errorf("at-fire = blind %d debt %d, want 3/2", o.EBMBlindAtFire, o.EBMDebtAgeAtFire)
	}
	if o.EBMRoundsToCheck != 2 || o.EBMMsToCheck != 6000 || o.EBMCheckWithin1 || !o.EBMCheckWithin2 {
		t.Errorf("chain = rounds %d ms %d w1 %v w2 %v, want 2/6000/false/true",
			o.EBMRoundsToCheck, o.EBMMsToCheck, o.EBMCheckWithin1, o.EBMCheckWithin2)
	}
	if o.DebtArea != 6 || o.BlindPeak != 3 {
		t.Errorf("debt area %d blind peak %d, want 6/3", o.DebtArea, o.BlindPeak)
	}
	if o.EBMPostSequence != "RV" || o.EBMExtraBlind != 0 {
		t.Errorf("post sequence %q extra %d, want RV/0", o.EBMPostSequence, o.EBMExtraBlind)
	}
	got := renderOutcomeProgress([]result{{Trajectory: s}})
	if !strings.Contains(got, "**EBM** eligible 1 · fired 1 (compliance ≤1 extra mutation 100%, median rounds-to-check 2)") {
		t.Errorf("render missing EBM segment:\n%s", got)
	}
}

func TestRenderOutcomeProgressAggregatesRuns(t *testing.T) {
	results := []result{
		{Trajectory: &trajectorySummary{Outcome: &outcomeSummary{
			Rounds: 8, ProgressRounds: 5, FalseProgressRounds: 2,
			Objective: 2, BestScore: 2, FinalScore: 2,
		}}},
		{Trajectory: &trajectorySummary{Outcome: &outcomeSummary{
			Objective: 1, Regression: 1, BestScore: 1, FinalScore: 0,
			RegressedFromBest: true, SearchRegretMs: 4000, Backfilled: true,
		}}},
		{Trajectory: &trajectorySummary{}}, // no outcome data: excluded
	}
	got := renderOutcomeProgress(results)
	for _, want := range []string{
		"**Outcome shadow** (2 runs)",
		"**objective transitions** 3",
		"**regressed from best** 1 (50%)",
		"**false progress** 2/5 (40%)",
		"**avg search regret** 4.0s",
		"backfilled 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in:\n%s", want, got)
		}
	}
	if renderOutcomeProgress(nil) != "" {
		t.Error("no runs must render nothing")
	}
}
