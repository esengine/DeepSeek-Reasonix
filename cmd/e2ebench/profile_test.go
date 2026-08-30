package main

import (
	"reflect"
	"testing"

	"reasonix/internal/ablation"
)

func TestBuildRunTaskArgsEnablesUnattendedWorkspaceWrites(t *testing.T) {
	cfg := suiteConfig{model: "e2e"}
	got := buildRunTaskArgs(cfg, "metrics.json", "run.trajectory.jsonl", 12, "fix it")
	want := []string{
		"run", "--auto", "--metrics", "metrics.json",
		"--trajectory", "run.trajectory.jsonl",
		"--model", "e2e", "--max-steps", "12", "fix it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("run task args = %v, want %v", got, want)
	}
}

func TestBuildRunTaskArgsPassesTheAblationArmThrough(t *testing.T) {
	cfg := suiteConfig{arm: ablation.New(ablation.Evidence, ablation.Planner)}
	got := buildRunTaskArgs(cfg, "m.json", "", 0, "fix it")
	want := []string{"run", "--auto", "--metrics", "m.json", "--ablate", "evidence,planner", "fix it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ablated args = %v, want %v", got, want)
	}
}

func TestBuildRunTaskArgsPassesEffortThroughWithoutLegacyMode(t *testing.T) {
	cfg := suiteConfig{effort: "low"}
	got := buildRunTaskArgs(cfg, "m.json", "", 0, "fix it")
	want := []string{"run", "--auto", "--metrics", "m.json", "--effort", "low", "fix it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effort args = %v, want %v", got, want)
	}
}

func TestResolveExperimentAxesHasNoExecutionModeArm(t *testing.T) {
	got, err := resolveExperimentAxes("evidence", "warm", "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if got.arm.String() != "evidence" || got.cache != "warm" || got.anchor != "wrong" {
		t.Fatalf("axes = %+v", got)
	}
}

func TestDefaultSuiteBudgetCoversCurrentFiveTaskBaseline(t *testing.T) {
	// The real-provider baseline exceeded 400k after only three successful
	// tasks. Keep enough headroom to grade all five instead of silently skipping
	// the final scenarios as normal model and cache usage varies.
	if defaultSuiteTokenBudget < 800_000 {
		t.Fatalf("default suite token budget = %d, want at least 800000", defaultSuiteTokenBudget)
	}
}

func TestNormalizeCacheArm(t *testing.T) {
	for input, want := range map[string]string{"": "cold", "cold": "cold", " WARM ": "warm"} {
		if got, err := normalizeCacheArm(input); err != nil || got != want {
			t.Fatalf("normalizeCacheArm(%q) = %q, %v", input, got, err)
		}
	}
	if _, err := normalizeCacheArm("hot"); err == nil {
		t.Fatal("unknown cache arm should fail")
	}
}
