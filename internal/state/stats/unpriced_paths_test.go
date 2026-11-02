package stats

// The same total is answered by two read paths: the exact JSONL query and the
// projection rollup. These guards pin that they count the same calls, that a
// fully priced range counts none, and what an old or reason-less row counts as.

import (
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

// Two read paths answer this total: the exact JSONL query and the projection
// rollup. One path losing the count is how a total would disagree with itself
// depending on which projection happened to be ready.
func TestBothReadPathsAgreeOnTheUnpricedCount(t *testing.T) {
	dir := testenv.TempDir(t)
	lines := []map[string]any{
		{"ts": "2026-09-01T01:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "1.50", "cost_currency": "CNY", "cost_complete": true},
		{"ts": "2026-09-01T02:00:00Z", "model": "relay/new-model", "source": "cli",
			"total": 40, "requests": 1, "cost_complete": false, "incomplete_reason": "missing_price_or_usage"},
		{"ts": "2026-09-02T01:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"total": 20, "requests": 1},
		{"ts": "2026-09-02T02:00:00Z", "model": "relay/new-model", "source": "cli",
			"total": 10, "requests": 1, "cost_complete": false, "incomplete_reason": "no_price"},
		// Request-only failure: no tokens, so no call to price was made.
		{"ts": "2026-09-02T03:00:00Z", "model": "relay/new-model", "source": "cli", "requests": 1},
	}
	filter := rangeFromDays(t, "2026-09-01", "2026-09-30")
	fromLive := queryLiveProjection(t, dir, filter, lines...)
	fromFiles, err := NewWriter(dir).Query(filter)
	if err != nil {
		t.Fatal(err)
	}
	fromProjection := queryProjection(t, dir, filter)

	wantReasons := []string{"missing_price_or_usage", "no_price"}
	paths := map[string]RangeStats{"JSONL": fromFiles, "live projection": fromLive, "rebuilt projection": fromProjection}
	for name, got := range paths {
		if got.UnpricedCalls != 3 {
			t.Errorf("%s: unpricedCalls = %d, want 3", name, got.UnpricedCalls)
		}
		if !reflect.DeepEqual(got.IncompleteReasons, wantReasons) {
			t.Errorf("%s: incompleteReasons = %v, want %v", name, got.IncompleteReasons, wantReasons)
		}
		if !got.CostIncomplete {
			t.Errorf("%s: costIncomplete = false, want true", name)
		}
		if !reflect.DeepEqual(got, fromFiles) {
			t.Errorf("%s disagrees with the exact read\nfiles: %+v\n%s: %+v", name, fromFiles, name, got)
		}
	}
}

// bothReadPaths reads one two-day fixture through the exact files and a
// rebuilt projection: a one-day range would never consult a rollup.
func bothReadPaths(t *testing.T, dir string) (RangeStats, RangeStats) {
	t.Helper()
	filter := rangeFromDays(t, "2026-08-09", "2026-08-11")
	fromFiles, err := NewWriter(dir).Query(filter)
	if err != nil {
		t.Fatal(err)
	}
	return fromFiles, queryProjection(t, dir, filter)
}

// A fully priced range reports no unpriced calls and names no reason: the
// count must not be invented to make the note appear.
func TestAFullyPricedRangeCountsNoUnpricedCalls(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "2", "cost_currency": "USD", "cost_complete": true},
		map[string]any{"ts": "2026-08-10T02:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 50, "requests": 1, "cost_amount": "0", "cost_currency": "USD", "cost_complete": true},
	)
	got := queryAll(t, dir)
	if got.UnpricedCalls != 0 || len(got.IncompleteReasons) != 0 || got.CostIncomplete {
		t.Fatalf("fully priced range = %d unpriced / reasons %v / costIncomplete %v, want zero and none",
			got.UnpricedCalls, got.IncompleteReasons, got.CostIncomplete)
	}
	fromFiles, fromProjection := bothReadPaths(t, dir)
	for name, got := range map[string]RangeStats{"JSONL": fromFiles, "projection": fromProjection} {
		if got.UnpricedCalls != 0 || len(got.IncompleteReasons) != 0 {
			t.Errorf("%s: fully priced range = %d unpriced / reasons %v, want zero and none",
				name, got.UnpricedCalls, got.IncompleteReasons)
		}
	}
}

// An old row with no cost fact is unpriced, and it is the case the quote path
// already names "no_price". It is counted, never silently skipped.
func TestAnOldRowCountsAsUnpricedWithTheExistingReason(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/x", "source": "cli", "total": 100, "requests": 1},
	)
	fromFiles, fromProjection := bothReadPaths(t, dir)
	for name, got := range map[string]RangeStats{"JSONL": fromFiles, "projection": fromProjection} {
		if got.UnpricedCalls != 1 {
			t.Errorf("%s: unpricedCalls = %d, want the old row counted", name, got.UnpricedCalls)
		}
		if !reflect.DeepEqual(got.IncompleteReasons, []string{"no_price"}) {
			t.Errorf("%s: incompleteReasons = %v, want the existing vocabulary [no_price]", name, got.IncompleteReasons)
		}
	}
}

// An estimate flag is not an unpriced call: an estimated rate with complete
// coverage priced the call, and the estimate caveat is a different note.
func TestAnEstimatedButCompleteQuoteIsNotUnpriced(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "2", "cost_currency": "USD",
			"cost_complete": true, "cost_estimated": true},
	)
	fromFiles, fromProjection := bothReadPaths(t, dir)
	for name, got := range map[string]RangeStats{"JSONL": fromFiles, "projection": fromProjection} {
		if !got.CostEstimated {
			t.Errorf("%s: costEstimated = false, want the estimate caveat kept", name)
		}
		if got.UnpricedCalls != 0 || len(got.IncompleteReasons) != 0 {
			t.Errorf("%s: an estimated but complete quote counts as %d unpriced / reasons %v",
				name, got.UnpricedCalls, got.IncompleteReasons)
		}
	}
}

// Neither a completed turn nor a request-only failure is a priced call: the
// count follows the same rule as costIncomplete.
func TestATurnMarkerAndARequestOnlyRowAreNotUnpricedCalls(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "source": "cli", "turn": true},
		map[string]any{"ts": "2026-08-10T02:00:00Z", "model": "relay/x", "source": "cli", "requests": 1},
		map[string]any{"ts": "2026-08-10T03:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 10, "requests": 1, "cost_amount": "0.5", "cost_currency": "USD", "cost_complete": true},
	)
	fromFiles, fromProjection := bothReadPaths(t, dir)
	for name, got := range map[string]RangeStats{"JSONL": fromFiles, "projection": fromProjection} {
		if got.UnpricedCalls != 0 || len(got.IncompleteReasons) != 0 || got.CostIncomplete {
			t.Errorf("%s: turn/request-only rows read as %d unpriced / reasons %v / incomplete %v",
				name, got.UnpricedCalls, got.IncompleteReasons, got.CostIncomplete)
		}
	}
}

// An unpriced row that recorded no reason is still counted. The count must not
// depend on the reason being known, and an unknown reason is not an empty
// reason in the list, which would read as a named one.
func TestAnUnpricedRowWithoutAReasonIsStillCounted(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "0", "cost_currency": "USD", "cost_complete": false},
	)
	fromFiles, fromProjection := bothReadPaths(t, dir)
	for name, got := range map[string]RangeStats{"JSONL": fromFiles, "projection": fromProjection} {
		if got.UnpricedCalls != 1 {
			t.Errorf("%s: unpricedCalls = %d, want the row counted even without a reason", name, got.UnpricedCalls)
		}
		if len(got.IncompleteReasons) != 0 {
			t.Errorf("%s: incompleteReasons = %v, want no reason invented for an unknown one", name, got.IncompleteReasons)
		}
	}
}
