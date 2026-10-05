package main

import "testing"

func base(limits map[string]int, files map[string]map[string]int) *Baseline {
	if limits == nil {
		limits = map[string]int{}
	}
	if files == nil {
		files = map[string]map[string]int{}
	}
	return &Baseline{Limits: limits, Files: files}
}

// Lowering a budget is the ratchet doing its job and needs no ceremony.
func TestWideningsIgnoresATighteningUpdate(t *testing.T) {
	old := base(map[string]int{"essay": 100}, map[string]map[string]int{"a.go": {"essay": 10}})
	next := base(map[string]int{"essay": 90}, map[string]map[string]int{"a.go": {"essay": 5}})
	if got := widenings(old, next); len(got) != 0 {
		t.Fatalf("a tightening update was reported as widening: %v", got)
	}
}

// Raising one carries debt forward, which REASONIX.md refuses. Naming the file
// and the numbers is the point: "the baseline grew" is not reviewable.
func TestWideningsNamesEveryRaisedBudget(t *testing.T) {
	old := base(map[string]int{"essay": 100}, map[string]map[string]int{"a.go": {"essay": 10}})
	next := base(map[string]int{"essay": 101}, map[string]map[string]int{
		"a.go": {"essay": 10}, "b.go": {"essay": 3},
	})
	got := widenings(old, next)
	if len(got) != 2 {
		t.Fatalf("widenings = %v, want the ceiling and the new file", got)
	}
	if got[0] != "b.go: essay 0 -> 3" || got[1] != "ceiling essay: 100 -> 101" {
		t.Fatalf("widenings = %v", got)
	}
}

// A first run has nothing to compare against, so nothing is a widening.
func TestWideningsIsQuietWithoutAPreviousBaseline(t *testing.T) {
	if got := widenings(nil, base(map[string]int{"essay": 5}, nil)); len(got) != 0 {
		t.Fatalf("a first baseline was reported as widening: %v", got)
	}
}

// Budget nothing uses is budget a file can grow back into, and it only comes
// down when someone runs -update — so a clean run has to say it is there.
func TestReclaimableReportsBudgetTheTreeNoLongerNeeds(t *testing.T) {
	old := base(map[string]int{"essay": 10, "banner": 4}, nil)
	findings := []Finding{{Rule: "essay", Weight: 3}, {Rule: "banner", Weight: 4}}
	got := reclaimable(old, findings)
	if got["essay"] != 7 {
		t.Fatalf("essay slack = %d, want 7", got["essay"])
	}
	if _, ok := got["banner"]; ok {
		t.Fatalf("an exactly-used budget was reported as slack: %v", got)
	}
}
