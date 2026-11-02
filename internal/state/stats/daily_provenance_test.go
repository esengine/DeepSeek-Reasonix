package stats

// A chart draws a day whose money is a floor and a day whose money is measured
// with the same shape. These guards pin the per-day accounting at the wire and
// on both read paths, so the series can say which days can be trusted.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// trustFixture is the two-day case the series must tell apart: 2026-09-01 is
// fully priced and named its card; 2026-09-02 counted an unpriced call.
func trustFixture(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-09-01",
		map[string]any{"ts": "2026-09-01T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "1.50", "cost_currency": "USD",
			"cost_complete": true, "pricing_fingerprint": "card-a"},
	)
	writeDay(t, dir, "2026-09-02",
		map[string]any{"ts": "2026-09-02T01:00:00Z", "model": "relay/new", "source": "cli",
			"total": 40, "requests": 1, "cost_complete": false,
			"incomplete_reason": "missing_price_or_usage"},
	)
	return dir
}

func dailyPayload(t *testing.T, r RangeStats) map[string]map[string]any {
	t.Helper()
	b, err := json.Marshal(r.Daily)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]any{}
	for _, row := range rows {
		day, _ := row["day"].(string)
		out[day] = row
	}
	return out
}

// The gap: the same two-day read used to emit, per day, only
// {"day":"2026-09-02","total":40,"byModel":{"relay/new":40},"byProvider":{"relay":40},
// "requests":1,"turns":0,"cacheHit":0,"cacheMiss":0} — nothing that said the
// second day's money was a floor. The same read now carries it.
func TestADaySaysWhetherItsMoneyIsAFloor(t *testing.T) {
	got, err := NewWriter(trustFixture(t)).Query(rangeFromDays(t, "2026-09-01", "2026-09-02"))
	if err != nil {
		t.Fatal(err)
	}
	rows := dailyPayload(t, got)
	if len(rows) != 2 {
		t.Fatalf("series has %d rows, want 2: %+v", len(rows), rows)
	}

	// The good day: one counted call, every unknown measured at zero.
	good := rows["2026-09-01"]
	wantGood := map[string]any{
		"countedCalls": float64(1), "unpricedCalls": float64(0),
		"estimatedCalls": float64(0), "unnamedCardCalls": float64(0),
		"unclassifiedCardCalls": float64(0),
	}
	for key, want := range wantGood {
		if good[key] != want {
			t.Errorf("good day %s = %v, want %v (the day is priced, so zero is a measurement)", key, good[key], want)
		}
	}
	if _, ok := good["incompleteReasons"]; ok {
		t.Errorf("good day carries an incomplete reason: %v", good["incompleteReasons"])
	}

	// The bad day: one counted call, one unpriced, with the reason the row
	// recorded. Its card is unclassified, not measured zero.
	bad := rows["2026-09-02"]
	wantBad := map[string]any{
		"countedCalls": float64(1), "unpricedCalls": float64(1),
		"estimatedCalls": float64(0), "unnamedCardCalls": float64(0),
		"unclassifiedCardCalls": float64(1),
	}
	for key, want := range wantBad {
		if bad[key] != want {
			t.Errorf("floor day %s = %v, want %v", key, bad[key], want)
		}
	}
	if got, _ := bad["incompleteReasons"].([]any); !reflect.DeepEqual(got, []any{"missing_price_or_usage"}) {
		t.Errorf("floor day reasons = %v, want the reason the row recorded", bad["incompleteReasons"])
	}
}

// dayCounts is one day's accounting reduced to the facts the series exists to
// carry, so a parity failure names the count instead of dumping both structs.
type dayCounts struct {
	Counted, Unpriced, Estimated, UnnamedCard, Unclassified int
	Reasons                                                 []string
}

func countsByDay(days []DailyTokens) map[string]dayCounts {
	out := map[string]dayCounts{}
	for _, day := range days {
		out[day.Day] = dayCounts{
			Counted: day.CountedCalls, Unpriced: day.UnpricedCalls, Estimated: day.EstimatedCalls,
			UnnamedCard: day.UnnamedCardCalls, Unclassified: day.UnclassifiedCardCalls,
			Reasons: day.IncompleteReasons,
		}
	}
	return out
}

// TestBothReadPathsAgreeOnEverySeriesCount keeps the JSONL series and the
// projection series honest: the same rows must yield the same denominator and
// the same three unknowns on every day, live and rebuilt.
func TestBothReadPathsAgreeOnEverySeriesCount(t *testing.T) {
	dir := usageProvenanceFixture(t)
	filter := provenanceFilter()
	fromFiles, err := NewWriter(dir).Query(filter)
	if err != nil {
		t.Fatal(err)
	}
	liveDir := testenv.TempDir(t)
	fromLive := queryLiveProjection(t, liveDir, filter, provenanceLines(t)...)
	fromRebuilt := queryProjection(t, liveDir, filter)

	want := countsByDay(fromFiles.Daily)
	for name, got := range map[string]RangeStats{
		"live projection":    fromLive,
		"rebuilt projection": fromRebuilt,
	} {
		if have := countsByDay(got.Daily); !reflect.DeepEqual(have, want) {
			t.Errorf("%s disagrees with the exact read on a per-day count\nfiles:   %+v\n%s: %+v", name, want, name, have)
		}
		// The whole series, not only the counts: money and reasons are read
		// beside the counts and must come from the same rows.
		if !reflect.DeepEqual(got.Daily, fromFiles.Daily) {
			t.Errorf("%s disagrees with the exact read on a day row\nfiles:   %+v\n%s: %+v", name, fromFiles.Daily, name, got.Daily)
		}
	}
}

// TestAFullyPricedRangeHasEveryDayMeasured is the control: when every call is
// priced, every day answers measured zeros rather than silence.
func TestAFullyPricedRangeHasEveryDayMeasured(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-09-05",
		map[string]any{"ts": "2026-09-05T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 10, "requests": 1, "cost_amount": "0.10", "cost_currency": "USD",
			"cost_complete": true, "pricing_fingerprint": "card-a"},
	)
	writeDay(t, dir, "2026-09-06",
		map[string]any{"ts": "2026-09-06T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 20, "requests": 1, "cost_amount": "0.20", "cost_currency": "USD",
			"cost_complete": true, "pricing_fingerprint": "card-a"},
	)
	got, err := NewWriter(dir).Query(rangeFromDays(t, "2026-09-05", "2026-09-06"))
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range got.Daily {
		if day.CountedCalls != 1 {
			t.Errorf("%s counted %d calls, want 1", day.Day, day.CountedCalls)
		}
		if day.UnpricedCalls != 0 || day.EstimatedCalls != 0 || day.UnnamedCardCalls != 0 || day.UnclassifiedCardCalls != 0 {
			t.Errorf("%s reads unmeasured: %+v", day.Day, day)
		}
		if len(day.IncompleteReasons) != 0 {
			t.Errorf("%s carries an incomplete reason though every call is priced: %v", day.Day, day.IncompleteReasons)
		}
	}
}

// TestAnOldRowMarksItsDayUnknownRatherThanZero covers the legacy row: tokens
// with no price fact at all. The day is a floor, and its card classification is
// unknown (UnclassifiedCardCalls), never a measured zero of UnnamedCardCalls.
func TestAnOldRowMarksItsDayUnknownRatherThanZero(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-09-07",
		map[string]any{"ts": "2026-09-07T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 10, "requests": 1},
	)
	day := func(r RangeStats) DailyTokens {
		t.Helper()
		for _, row := range r.Daily {
			if row.Day == "2026-09-07" {
				return row
			}
		}
		t.Fatalf("no 2026-09-07 row in %+v", r.Daily)
		return DailyTokens{}
	}
	exact, err := NewWriter(dir).Query(rangeFromDays(t, "2026-09-07", "2026-09-08"))
	if err != nil {
		t.Fatal(err)
	}
	projection := queryProjection(t, dir, rangeFromDays(t, "2026-09-07", "2026-09-08"))
	for name, got := range map[string]RangeStats{"JSONL": exact, "projection": projection} {
		row := day(got)
		if row.CountedCalls != 1 || row.UnpricedCalls != 1 {
			t.Errorf("%s: counted %d / unpriced %d, want 1 / 1", name, row.CountedCalls, row.UnpricedCalls)
		}
		if row.UnclassifiedCardCalls != 1 {
			t.Errorf("%s: unclassified card calls = %d, want 1 (the card was never recorded, which is unknown)", name, row.UnclassifiedCardCalls)
		}
		if row.UnnamedCardCalls != 0 {
			t.Errorf("%s: unnamed card calls = %d, want 0; an old row is unclassified, not a measured unnamed card", name, row.UnnamedCardCalls)
		}
		if !reflect.DeepEqual(row.IncompleteReasons, []string{"no_price"}) {
			t.Errorf("%s: reasons = %v, want [no_price]", name, row.IncompleteReasons)
		}
	}
}

// TestASeriesCountWithoutAReasonStillCounts pins that the denominator covers a
// call whose unpriced quote recorded no reason: the count is not derived from
// the reason vocabulary.
func TestASeriesCountWithoutAReasonStillCounts(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-09-09",
		map[string]any{"ts": "2026-09-09T01:00:00Z", "model": "relay/x", "source": "cli",
			"total": 5, "requests": 1, "cost_amount": "1", "cost_currency": "USD",
			"cost_complete": false},
	)
	got, err := NewWriter(dir).Query(rangeFromDays(t, "2026-09-09", "2026-09-10"))
	if err != nil {
		t.Fatal(err)
	}
	var row DailyTokens
	for _, d := range got.Daily {
		if d.Day == "2026-09-09" {
			row = d
		}
	}
	if row.CountedCalls != 1 || row.UnpricedCalls != 1 {
		t.Fatalf("counted %d / unpriced %d, want 1 / 1", row.CountedCalls, row.UnpricedCalls)
	}
	if len(row.IncompleteReasons) != 0 {
		t.Fatalf("an unnamed reason entered the vocabulary: %v", row.IncompleteReasons)
	}
	if !strings.Contains(mustJSON(t, dailyPayload(t, got)["2026-09-09"]), `"unpricedCalls":1`) {
		t.Fatal("the wire row does not carry its unpriced count")
	}
}
