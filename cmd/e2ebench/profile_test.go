package main

import (
	"reflect"
	"testing"
)

func TestAppendBenchmarkProfileArgsBaselineIsByteIdentical(t *testing.T) {
	args := []string{"run", "fix the bug"}
	if got := appendBenchmarkProfileArgs(args, benchmarkProfileBaseline); !reflect.DeepEqual(got, args) {
		t.Fatalf("baseline args changed: %v", got)
	}
}

func TestAppendBenchmarkProfileArgsDeliveryUsesRealRuntimeProfile(t *testing.T) {
	args := []string{"run"}
	got := appendBenchmarkProfileArgs(args, benchmarkProfileDelivery)
	want := []string{"run", "--profile", "delivery"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery args = %v, want %v", got, want)
	}
}

func TestBuildRunTaskArgsEnablesUnattendedWorkspaceWrites(t *testing.T) {
	got := buildRunTaskArgs("metrics.json", "e2e", benchmarkProfileDelivery, 12, "fix it")
	want := []string{
		"run", "--auto", "--metrics", "metrics.json",
		"--model", "e2e", "--max-steps", "12",
		"--profile", "delivery", "fix it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("run task args = %v, want %v", got, want)
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

func TestNormalizeBenchmarkProfile(t *testing.T) {
	for _, input := range []string{"", "baseline", " BASELINE "} {
		if got, err := normalizeBenchmarkProfile(input); err != nil || got != benchmarkProfileBaseline {
			t.Fatalf("normalize(%q) = %q, %v", input, got, err)
		}
	}
	if got, err := normalizeBenchmarkProfile("delivery"); err != nil || got != benchmarkProfileDelivery {
		t.Fatalf("normalize(delivery) = %q, %v", got, err)
	}
	if _, err := normalizeBenchmarkProfile("fast"); err == nil {
		t.Fatal("unknown profile should fail")
	}
}
