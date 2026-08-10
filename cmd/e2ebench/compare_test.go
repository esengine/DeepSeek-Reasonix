package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeArm(t *testing.T, dir, name string, results []result) string {
	t.Helper()
	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestCompareReportsRendersPerSolvedDeltas(t *testing.T) {
	dir := t.TempDir()
	control := result{task: task{ID: "a"}, Passed: true, WallMs: 60_000}
	control.Steps = 7
	control.ToolCalls = 10
	control.PromptTokens = 900
	control.CompletionTokens = 100
	control.UsageBySource = map[string]sourceUsage{"executor": {Calls: 6}, "planner": {Calls: 1}}
	control.Trajectory = &trajectorySummary{ModelRounds: 5}

	ablated := result{task: task{ID: "a"}, Passed: true, WallMs: 45_000}
	ablated.Steps = 5
	ablated.ToolCalls = 10
	ablated.PromptTokens = 700
	ablated.CompletionTokens = 100
	ablated.UsageBySource = map[string]sourceUsage{"executor": {Calls: 5}}
	ablated.Trajectory = &trajectorySummary{ModelRounds: 5}

	pathA := writeArm(t, dir, "control.json", []result{control})
	pathB := writeArm(t, dir, "ablated.json", []result{ablated})

	got, err := compareReports(pathA, pathB)
	if err != nil {
		t.Fatalf("compareReports: %v", err)
	}
	for _, want := range []string{
		"| Solved | 1/1 (100%) | 1/1 (100%) |",
		"| Model requests / solved | 7.0 | 5.0 |",
		"| Planner requests / solved | 1.0 | 0.0 |",
		"| Wall seconds / solved | 60.0 | 45.0 |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("compare table missing %q:\n%s", want, got)
		}
	}
}

func TestCompareReportsMarginalUtilityByClass(t *testing.T) {
	dir := t.TempDir()
	mk := func(id, class string, passed bool, wallMs int64) result {
		r := result{task: task{ID: id, Class: class}, Passed: passed, WallMs: wallMs}
		r.Steps = 1
		return r
	}
	withPlanner := []result{
		mk("bug1", "bugfix", true, 30_000), mk("bug2", "bugfix", true, 30_000),
		mk("ref1", "refactor", true, 112_000),
	}
	without := []result{
		mk("bug1", "bugfix", true, 18_000), mk("bug2", "bugfix", true, 18_000),
		mk("ref1", "refactor", false, 74_000),
	}
	pathA := writeArm(t, dir, "with.json", withPlanner)
	pathB := writeArm(t, dir, "without.json", without)

	got, err := compareReports(pathA, pathB)
	if err != nil {
		t.Fatalf("compareReports: %v", err)
	}
	for _, want := range []string{
		"**Marginal utility (A − B):** accuracy +33.3pp · wall/task +20.7s",
		"| bugfix | 2/2 | 2/2 | +0.0pp | 30.0s | 18.0s | +12.0s |",
		"| refactor | 1/1 | 0/1 | +100.0pp | 112.0s | 74.0s | +38.0s |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("marginal utility missing %q:\n%s", want, got)
		}
	}
}

func TestRequestsBySourceLineOrdersByCalls(t *testing.T) {
	line := requestsBySourceLine(map[string]sourceUsage{
		"planner":  {Calls: 3, PromptTokens: 300},
		"executor": {Calls: 9, PromptTokens: 900, CompletionTokens: 100},
		"title":    {Calls: 0},
	})
	if !strings.Contains(line, "executor 9 (1,000 tok) · planner 3 (300 tok)") {
		t.Fatalf("source line = %q", line)
	}
	if strings.Contains(line, "title") {
		t.Fatalf("zero-call sources must be omitted: %q", line)
	}
	if requestsBySourceLine(nil) != "" {
		t.Fatal("no sources must render nothing")
	}
}
