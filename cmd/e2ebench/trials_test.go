package main

import (
	"strings"
	"testing"
)

func trialResult(id string, trial int, passed bool) result {
	return result{task: task{ID: id}, Trial: trial, Passed: passed}
}

// pass^k is the share of tasks that passed every trial, pass@k the share that
// passed any, and mean pass@1 the share of all trials that passed. A task that
// passed some trials is named, since it is the one worth reading.
func TestTrialsLineReportsReliabilityAcrossTrials(t *testing.T) {
	results := []result{
		trialResult("steady", 1, true), trialResult("steady", 2, true), trialResult("steady", 3, true),
		trialResult("flaky", 1, true), trialResult("flaky", 2, false), trialResult("flaky", 3, true),
		trialResult("broken", 1, false), trialResult("broken", 2, false), trialResult("broken", 3, false),
	}
	line := trialsLine(results)
	for _, want := range []string{
		"3 trials, 3/3 tasks complete",
		"**pass^3** 33% (1/3)",
		"**pass@3** 67% (2/3)",
		"**mean pass@1** 56% (5/9)",
		"`flaky` 2/3",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in:\n%s", want, line)
		}
	}
	if strings.Contains(line, "`steady`") || strings.Contains(line, "`broken`") {
		t.Fatalf("only a task with mixed outcomes is flaky:\n%s", line)
	}
}

// A trial the budget skipped is not a failure; the task is judged on the
// trials it ran, and the line says it was not complete.
func TestTrialsLineJudgesABudgetCutTaskOnWhatRan(t *testing.T) {
	results := []result{
		trialResult("a", 1, true), trialResult("a", 2, true),
		trialResult("b", 1, true), {task: task{ID: "b"}, Trial: 2, Skipped: true},
	}
	line := trialsLine(results)
	if !strings.Contains(line, "1/2 tasks complete") || !strings.Contains(line, "**pass^2** 100% (2/2)") {
		t.Fatalf("line = %s", line)
	}
}

// Without trials the report reads exactly as it did.
func TestTrialsLineIsSilentWithoutTrials(t *testing.T) {
	if got := trialsLine([]result{{task: task{ID: "a"}, Attempt: 1, Passed: true}}); got != "" {
		t.Fatalf("line = %q, want nothing for a run without trials", got)
	}
}
