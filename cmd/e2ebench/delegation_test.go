package main

import (
	"strings"
	"testing"
)

// A single-agent arm must say so rather than render nothing: an empty section
// reads as "not measured" when it actually means "no delegation happened".
func TestRenderDelegationNamesTheSingleAgentArm(t *testing.T) {
	got := renderDelegation([]result{
		{Passed: true, runMetrics: runMetrics{ToolCalls: 9}},
		{Passed: false, runMetrics: runMetrics{ToolCalls: 4}},
	})
	if !strings.Contains(got, "none") || !strings.Contains(got, "1/2") {
		t.Fatalf("single-agent arm rendered as %q", got)
	}
	if renderDelegation(nil) != "" {
		t.Fatal("no runs at all must render nothing")
	}
}

// The delegated arm has to expose the cost side, not just the outcome.
func TestRenderDelegationExposesWhatDelegationCost(t *testing.T) {
	got := renderDelegation([]result{{
		Passed: true,
		runMetrics: runMetrics{
			ToolCalls: 30, SubagentToolCalls: 24,
			SubagentRuns: 3, SubagentNestedRuns: 1, SubagentMutations: 5,
			DuplicateWorkPaths: 2,
			CompletionReports:  2, CompletionsProsedOnly: 1,
			FalseCompletions: 1, CriterionDowngrades: 3,
			WriteScopeViolations: 1,
		},
	}})
	for _, want := range []string{
		"3** child runs (1 nested)",
		"parent **6** tool calls",
		"children **24**",
		"duplicate work**: 2",
		"checkable claim: **2/3**",
		"false completions**: 1 run(s), 3 criterion",
		"write-scope violations**: 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("delegation section missing %q:\n%s", want, got)
		}
	}
}

// A clean delegated arm must not print alarm lines it has no evidence for.
func TestRenderDelegationStaysQuietWhenNothingWentWrong(t *testing.T) {
	got := renderDelegation([]result{{
		Passed:     true,
		runMetrics: runMetrics{ToolCalls: 12, SubagentToolCalls: 8, SubagentRuns: 2, CompletionReports: 2},
	}})
	for _, unwanted := range []string{"duplicate work", "false completions", "write-scope violations"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("clean run reported %q:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "checkable claim: **2/2**") {
		t.Errorf("clean run lost its completion coverage:\n%s", got)
	}
}
