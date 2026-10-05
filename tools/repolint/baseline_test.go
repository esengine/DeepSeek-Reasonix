package main

import "testing"

func budget(files map[string]map[string]int, limits map[string]int) *Baseline {
	return &Baseline{Limits: limits, Files: files}
}

func TestBaselineAllowsWhatItRecorded(t *testing.T) {
	b := budget(map[string]map[string]int{"a.go": {ruleEssay: 4}}, map[string]int{ruleEssay: 4})
	_, msgs := b.exceeded([]Finding{{"a.go", 3, ruleEssay, "", 4}})
	if len(msgs) != 0 {
		t.Fatalf("recorded debt reported as new: %v", msgs)
	}
}

func TestSwappingAShortEssayForALongOneTripsTheRatchet(t *testing.T) {
	b := budget(map[string]map[string]int{"a.go": {ruleEssay: 1}}, map[string]int{ruleEssay: 1})
	_, msgs := b.exceeded([]Finding{{"a.go", 3, ruleEssay, "", 27}})
	if len(msgs) == 0 {
		t.Fatal("a 27-line-over block replacing a 1-line-over block must fail")
	}
}

func TestUnbaselinedFileMustBeClean(t *testing.T) {
	b := budget(map[string]map[string]int{"a.go": {ruleEssay: 9}}, map[string]int{ruleEssay: 9})
	over, msgs := b.exceeded([]Finding{{"new.go", 1, ruleEssay, "", 1}})
	if len(msgs) != 1 || len(over) != 1 || over[0].File != "new.go" {
		t.Fatalf("new file not gated: over=%v msgs=%v", over, msgs)
	}
}

func TestRepoCeilingCatchesDebtMovedBetweenFiles(t *testing.T) {
	b := budget(map[string]map[string]int{"a.go": {ruleBanner: 2}, "b.go": {ruleBanner: 2}}, map[string]int{ruleBanner: 2})
	_, msgs := b.exceeded([]Finding{
		{"a.go", 1, ruleBanner, "", 1}, {"a.go", 2, ruleBanner, "", 1},
		{"b.go", 1, ruleBanner, "", 1}, {"b.go", 2, ruleBanner, "", 1},
	})
	if len(msgs) != 1 {
		t.Fatalf("per-file budgets pass but the repo ceiling of 2 must fail: %v", msgs)
	}
}

func TestBaselineRoundTripsWeights(t *testing.T) {
	b := baselineFrom([]Finding{
		{"a.go", 1, ruleEssay, "", 3},
		{"a.go", 9, ruleEssay, "", 2},
		{"a.go", 4, ruleBanner, "", 1},
	})
	if got := b.Files["a.go"][ruleEssay]; got != 5 {
		t.Fatalf("essay budget = %d, want 5", got)
	}
	if got := b.Limits[ruleEssay]; got != 5 {
		t.Fatalf("essay ceiling = %d, want 5", got)
	}
	if got := b.Limits[ruleBanner]; got != 1 {
		t.Fatalf("banner ceiling = %d, want 1", got)
	}
}
