package stats

// A usage total says its money is a floor and how many calls are under it; it
// cannot say what the figure is over or whether a price names a card. These
// guards pin those counts and that both read paths compute them alike.

import (
	"reflect"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// provenanceFixture is one range with every provenance class in it: a pricier
// call named by its card, one priced from an estimated card, two unpriced calls
// with a quote (one of which names no card), and two rows written before the
// card field existed. It also carries a request-only failure and a turn marker,
// neither of which is a call.
func provenanceFixture(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	lines := provenanceLines(t)
	writeDay(t, dir, "2026-08-10", lines[:4]...)
	writeDay(t, dir, "2026-08-11", lines[4:]...)
	return dir
}

func provenanceFilter() SourceFilter {
	from, _ := time.Parse(dayLayout, "2026-08-09")
	to, _ := time.Parse(dayLayout, "2026-08-11")
	return SourceFilter{From: from, To: to}
}

// The gap: before these counts existed this range emitted exactly
// `{"active_days":1,"cache_hit":0,"cache_miss":0,"cost":[{"amount":"1.5","currency":"USD"}],"costIncomplete":true,"from":"2026-08-09","incompleteReasons":["missing_price_or_usage"],"requests":2,"to":"2026-08-11","tokens":150,"top_model":"deepseek/x","top_provider":"deepseek","turns":0,"unpricedCalls":1}`
// — the floor and one unpriced count, with no denominator and no card fact.
// The same read now carries them.
func TestATotalSaysWhatItsMoneyIsOver(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "1.5", "cost_currency": "USD",
			"cost_complete": true, "pricing_fingerprint": "card-aaaa"},
		map[string]any{"ts": "2026-08-10T02:00:00Z", "model": "relay/new", "source": "cli",
			"total": 50, "requests": 1, "cost_amount": "0", "cost_currency": "USD",
			"cost_complete": false, "incomplete_reason": "missing_price_or_usage",
			"pricing_fingerprint": "card-bbbb"},
	)
	from, _ := time.Parse(dayLayout, "2026-08-09")
	to, _ := time.Parse(dayLayout, "2026-08-11")
	got, err := NewWriter(dir).Query(SourceFilter{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	payload := totalPayload(t, got)
	t.Logf("total payload = %s", mustJSON(t, payload))
	want := map[string]any{
		"from": "2026-08-09", "to": "2026-08-11",
		"tokens": float64(150), "requests": float64(2), "turns": float64(0),
		"cache_hit": float64(0), "cache_miss": float64(0),
		"cost":           []any{map[string]any{"amount": "1.5", "currency": "USD"}},
		"costIncomplete": true,
		"unpricedCalls":  float64(1),
		"incompleteReasons": []any{
			"missing_price_or_usage",
		},
		// Two counted calls: the denominator the unpriced call is read against.
		"countedCalls": float64(2),
		// Neither price came from a fallback card, and both rows name theirs.
		"estimatedCalls":        float64(0),
		"unnamedCardCalls":      float64(0),
		"unclassifiedCardCalls": float64(0),
		"active_days":           float64(1),
		"top_model":             "deepseek/x",
		"top_provider":          "deepseek",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("total payload = %s\nwant %s", mustJSON(t, payload), mustJSON(t, want))
	}
}

// TestBothReadPathsAgreeOnEveryProvenanceCount is the guard that keeps the
// JSONL read and the projection honest: the same rows must yield the same
// denominator, unpriced count, estimate count, and card classification.
func TestBothReadPathsAgreeOnEveryProvenanceCount(t *testing.T) {
	dir := provenanceFixture(t)
	filter := provenanceFilter()
	fromFiles, err := NewWriter(dir).Query(filter)
	if err != nil {
		t.Fatal(err)
	}
	want := struct {
		counted, unpriced, estimated, unnamed, unclassified int
		reasons                                             []string
	}{6, 4, 1, 1, 2, []string{"missing_price_or_usage", "no_price"}}
	if got := fromFiles; got.CountedCalls != want.counted || got.UnpricedCalls != want.unpriced ||
		got.EstimatedCalls != want.estimated || got.UnnamedCardCalls != want.unnamed ||
		got.UnclassifiedCardCalls != want.unclassified || !reflect.DeepEqual(got.IncompleteReasons, want.reasons) {
		t.Fatalf("JSONL read = counted %d / unpriced %d %v / estimated %d / unnamed %d / unclassified %d, want %+v",
			got.CountedCalls, got.UnpricedCalls, got.IncompleteReasons, got.EstimatedCalls, got.UnnamedCardCalls,
			got.UnclassifiedCardCalls, want)
	}
	// The live path writes the rows it is given, so it gets a directory of its
	// own; rebuilding there reindexes exactly those rows once.
	liveDir := testenv.TempDir(t)
	fromLive := queryLiveProjection(t, liveDir, filter, provenanceLines(t)...)
	fromRebuilt := queryProjection(t, liveDir, filter)
	for name, got := range map[string]RangeStats{
		"live projection":    fromLive,
		"rebuilt projection": fromRebuilt,
	} {
		if !reflect.DeepEqual(got, fromFiles) {
			t.Errorf("%s disagrees with the exact read\nfiles:   %+v\n%s: %+v", name, fromFiles, name, got)
		}
	}
}

// provenanceLines is provenanceFixture's row set, so the append path builds
// the same projection the reindex builds from the files.
func provenanceLines(t *testing.T) []map[string]any {
	t.Helper()
	return []map[string]any{
		{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "1.50", "cost_currency": "USD",
			"cost_complete": true, "pricing_fingerprint": "card-aaaa"},
		{"ts": "2026-08-10T02:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "2", "cost_currency": "USD",
			"cost_complete": true, "cost_estimated": true, "pricing_fingerprint": "card-bbbb"},
		{"ts": "2026-08-10T03:00:00Z", "model": "relay/new", "source": "cli",
			"total": 50, "requests": 1, "cost_amount": "0", "cost_currency": "USD",
			"cost_complete": false, "incomplete_reason": "missing_price_or_usage",
			"pricing_fingerprint": "card-cccc"},
		{"ts": "2026-08-10T04:00:00Z", "model": "relay/new", "source": "cli",
			"total": 25, "requests": 1, "cost_amount": "0", "cost_currency": "USD",
			"cost_complete": false, "incomplete_reason": "no_price"},
		{"ts": "2026-08-11T01:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 20, "requests": 1},
		{"ts": "2026-08-11T02:00:00Z", "model": "deepseek/x", "source": "cli",
			"total": 10, "requests": 1},
		{"ts": "2026-08-11T03:00:00Z", "model": "relay/new", "source": "cli", "requests": 1},
		{"ts": "2026-08-11T04:00:00Z", "source": "cli", "turn": true},
	}
}
