package main

import "testing"

func TestUnionIntervalsDoesNotDoubleCountOverlappingOSLocks(t *testing.T) {
	spans := unionIntervals([]interval{{10, 30}, {20, 40}, {50, 60}, {1, 3}, {3, 10}})
	if len(spans) != 2 || spans[0] != (interval{1, 40}) || spans[1] != (interval{50, 60}) || sumIntervals(spans) != 49 {
		t.Fatalf("invalid physical hold union: %+v", spans)
	}
}
