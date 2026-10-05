package main

import (
	"strings"
	"testing"
)

func TestMarkDominatedFindsTheAlarm(t *testing.T) {
	points := []paretoPoint{
		{label: "fast-accurate", acc: 90, ttcsMs: 30_000, solved: 9, ran: 10},
		{label: "reasonix", acc: 70, ttcsMs: 100_000, solved: 7, ran: 10},
		{label: "slow-accurate", acc: 95, ttcsMs: 150_000, solved: 19, ran: 20},
	}
	markDominated(points)
	if points[1].dominatedBy != "fast-accurate" {
		t.Errorf("reasonix dominatedBy = %q, want fast-accurate (more accurate and faster)", points[1].dominatedBy)
	}
	if points[0].dominatedBy != "" || points[2].dominatedBy != "" {
		t.Errorf("frontier points must not be dominated: %q / %q", points[0].dominatedBy, points[2].dominatedBy)
	}
}

func TestMarkDominatedIgnoresUnsolvedArms(t *testing.T) {
	points := []paretoPoint{
		{label: "a", acc: 50, ttcsMs: 60_000, solved: 1, ran: 2},
		{label: "none", acc: 0, ttcsMs: 0, solved: 0, ran: 2},
	}
	markDominated(points)
	if points[0].dominatedBy != "" {
		t.Errorf("a zero-solve arm must not dominate (ttcs 0 is absence, not speed): %q", points[0].dominatedBy)
	}
}

func TestPerClassWinners(t *testing.T) {
	economy := armStats{ByClass: map[string]classStats{
		"atomic-bugfix":    {Ran: 2, Solved: 2, TTCS: []int64{15_000, 17_000}},
		"repo-exploration": {Ran: 2, Solved: 1, TTCS: []int64{20_000}},
	}}
	balanced := armStats{ByClass: map[string]classStats{
		"atomic-bugfix":    {Ran: 2, Solved: 2, TTCS: []int64{22_000, 24_000}},
		"repo-exploration": {Ran: 2, Solved: 2, TTCS: []int64{30_000, 31_000}},
		"unclassified":     {Ran: 1, Solved: 1, TTCS: []int64{5_000}},
	}}
	got := perClassWinners([]string{"economy.json", "balanced.json"}, []armStats{economy, balanced})
	for _, want := range []string{
		"### Per-class winners",
		"| atomic-bugfix | 100% · 17.0s | 100% · 24.0s | economy |",
		"| repo-exploration | 50% · 20.0s | 100% · 31.0s | balanced |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("per-class table missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "unclassified") {
		t.Fatalf("unclassified rows must not render:\n%s", got)
	}
	if perClassWinners([]string{"a.json"}, []armStats{economy}) != "" {
		t.Fatal("single arm has no winners to declare")
	}
}

func TestParetoSectionRendersChartAndVerdicts(t *testing.T) {
	got := paretoSection([]paretoPoint{
		{label: "a", acc: 90, ttcsMs: 30_000, solved: 9, ran: 10},
		{label: "b", acc: 70, ttcsMs: 100_000, solved: 7, ran: 10},
		{label: "none", acc: 0, ttcsMs: 0, solved: 0, ran: 2},
	})
	for _, want := range []string{
		"### Pareto: accuracy vs TTCS",
		"Accuracy",
		"→ TTCS",
		"A=a", "✗",
		"✅ `a` is on the Pareto frontier",
		"⚠️ `b` is **dominated** by `a`",
		"`none`: no solves",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pareto section missing %q:\n%s", want, got)
		}
	}
	if paretoSection([]paretoPoint{{label: "solo", acc: 50, ttcsMs: 1000, solved: 1, ran: 2}}) != "" {
		t.Fatal("a single arm has no comparison to draw")
	}
}
