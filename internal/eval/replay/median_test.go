package replay

import (
	"path/filepath"
	"testing"
)

func TestMedianReportFivePairedRuns(t *testing.T) {
	pairs, err := LoadPairs(filepath.Join("testdata", "paired_runs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 5 {
		t.Fatalf("pairs = %d, want 5", len(pairs))
	}
	got := MedianReport(pairs)
	if got.Pairs != 5 {
		t.Fatalf("report pairs = %d", got.Pairs)
	}
	if got.MedianListDelta >= 0 {
		t.Fatalf("median list delta = %v, want proxy fewer tools/list than baseline", got.MedianListDelta)
	}
	if got.MedianLatencyDelta >= 0 {
		t.Fatalf("median latency delta = %v, want proxy faster than baseline", got.MedianLatencyDelta)
	}
}
