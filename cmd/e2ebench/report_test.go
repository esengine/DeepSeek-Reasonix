package main

import (
	"strings"
	"testing"
)

func TestResultClassSeparatesGuardStopsFromWrongPatches(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    result
		want string
	}{
		{"solved", result{Passed: true, runMetrics: runMetrics{Outcome: "success"}}, "solved"},
		{"grader failed after a clean run", result{runMetrics: runMetrics{Outcome: "success"}}, "wrong_patch"},
		{"guard stop", result{runMetrics: runMetrics{Outcome: "max_steps"}}, "max_steps"},
		{"killed before writing metrics", result{}, "no_metrics"},
		{"skipped", result{Skipped: true}, "skipped"},
	} {
		if got := tc.r.class(); got != tc.want {
			t.Errorf("%s: class = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCostPerSolvedIsNotAnAverageOverAttempts(t *testing.T) {
	if got := costPerSolved(0.4, 2, "CNY"); got != "CNY 0.2000" {
		t.Fatalf("cost per solved = %q, want CNY 0.2000", got)
	}
	if got := costPerSolved(0.4, 0, "CNY"); got != "n/a" {
		t.Fatalf("zero solved = %q, want n/a", got)
	}
	if got := tokensPerSolved(1000, 0); got != "n/a" {
		t.Fatalf("zero solved tokens = %q, want n/a", got)
	}
}

func TestRenderLeadsWithCostPerSolvedAndFailureClasses(t *testing.T) {
	out := render([]result{
		{task: task{ID: "solved-one"}, Passed: true, WallMs: 4000,
			runMetrics: runMetrics{Outcome: "success", Cost: 0.02, Currency: "CNY", PromptTokens: 1000, CacheHitTokens: 900, CacheMissTokens: 100, ToolCalls: 6}},
		{task: task{ID: "stopped"}, WallMs: 9000,
			runMetrics: runMetrics{Outcome: "max_steps", Cost: 0.02, Currency: "CNY", PromptTokens: 1000, ToolCalls: 40, ToolFailures: 3}},
	})

	for _, want := range []string{
		"**Cost per solved:** CNY 0.0400",
		"**Failures by class:** max_steps ×1",
		"| Task | Result | Class |",
		"| `stopped` | ❌ fail | max_steps |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

// A killed agent writes no metrics. Counting that run as a zero-cost solve
// would make every published per-task figure cheaper than the truth.
func TestUnaccountedSolvesDoNotDeflateCostPerSolved(t *testing.T) {
	results := []result{
		{task: task{ID: "accounted"}, Passed: true, WallMs: 1000,
			runMetrics: runMetrics{Outcome: "success", Cost: 0.02, Currency: "$", PromptTokens: 1000}},
		{task: task{ID: "killed"}, Passed: true, WallMs: 1800000, Unaccounted: true,
			runMetrics: runMetrics{Outcome: "timeout"}},
	}
	out := renderBody(results)

	if !strings.Contains(out, "**Solved:** 2/2") {
		t.Error("both solves must still count toward the solve rate")
	}
	if !strings.Contains(out, "**Cost per solved:** $ 0.0200") {
		t.Errorf("cost per solved must divide by accounted solves only:\n%s", out)
	}
	if !strings.Contains(out, "Accounting incomplete for 1 of 2 instances") {
		t.Errorf("the gap must be disclosed, not hidden:\n%s", out)
	}
}

func TestRenderShowsCacheResetsByCause(t *testing.T) {
	out := render([]result{
		{task: task{ID: "a"}, Passed: true, WallMs: 1000,
			runMetrics: runMetrics{Outcome: "success", PrefixChangeReasonCounts: map[string]int{"compact_auto": 1, "tools": 2}}},
		{task: task{ID: "b"}, Passed: true, WallMs: 1000,
			runMetrics: runMetrics{Outcome: "success", PrefixChangeReasonCounts: map[string]int{"snip": 3}}},
	})
	if want := "**Cache resets by cause:** compact_auto ×1 · snip ×3 · tools ×2"; !strings.Contains(out, want) {
		t.Errorf("report missing %q:\n%s", want, out)
	}
}

func TestRenderOmitsCacheResetsLineWhenNoReasonsReported(t *testing.T) {
	out := render([]result{
		{task: task{ID: "a"}, Passed: true, WallMs: 1000, runMetrics: runMetrics{Outcome: "success"}},
	})
	if strings.Contains(out, "Cache resets by cause") {
		t.Errorf("report should omit the cache-resets line when no run reported any reasons:\n%s", out)
	}
}

func TestDurFormatsSubMinuteAndMinuteScale(t *testing.T) {
	if got := dur(4500); got != "4.5s" {
		t.Errorf("dur(4500) = %q, want 4.5s", got)
	}
	if got := dur(125000); got != "2m05s" {
		t.Errorf("dur(125000) = %q, want 2m05s", got)
	}
	if got := dur(0); got != "—" {
		t.Errorf("dur(0) = %q, want an em dash", got)
	}
}

func TestMedianPicksTheMiddleWallTime(t *testing.T) {
	if got := median([]int64{9000, 1000, 4000}); got != 4000 {
		t.Fatalf("median = %d, want 4000", got)
	}
	if got := median(nil); got != 0 {
		t.Fatalf("empty median = %d, want 0", got)
	}
}
