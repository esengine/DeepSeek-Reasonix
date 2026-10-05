package main

import (
	"strings"
	"testing"
)

func fanoutResult(f *fanoutMetrics) result {
	return result{runMetrics: runMetrics{Fanout: f}}
}

// The section exists to price a shape, so the two figures that price it have to
// survive aggregation: work against wall, and the floor the dependencies impose.
func TestFanoutSectionPricesWorkAgainstWall(t *testing.T) {
	got := renderFanout([]result{
		fanoutResult(&fanoutMetrics{Groups: 1, Workers: 2, WallMs: 1000, WorkMs: 1800, CriticalPathMs: 900, SlotWaitMs: 200}),
		fanoutResult(&fanoutMetrics{Groups: 1, Workers: 3, Adopted: 1, WallMs: 1000, WorkMs: 2200, CriticalPathMs: 700}),
		{Skipped: true, runMetrics: runMetrics{Fanout: &fanoutMetrics{Groups: 9}}},
	})
	for _, want := range []string{"**2** group(s)", "**5** member(s)", "**1** reused", "**2.00×**", "0.4s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("fan-out section missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "**11** group(s)") {
		t.Fatalf("a skipped run was priced:\n%s", got)
	}
}

// An arm that never delegated has nothing to price. Rendering zeros would put a
// measured-looking row under a suite that never ran the thing being measured.
func TestFanoutSectionIsAbsentWithoutAFanOut(t *testing.T) {
	if got := renderFanout([]result{{}, {}}); got != "" {
		t.Fatalf("priced a suite that never fanned out:\n%s", got)
	}
	if got := fanoutComparison([]string{"a.json", "b.json"}, []armStats{{}, {}}); got != "" {
		t.Fatalf("compared arms that never fanned out:\n%s", got)
	}
}
