package stats

// A range total said its money might be incomplete (costIncomplete), but never
// how many calls it could not price nor why. These guards pin the count, the
// vocabulary of the reasons, and that both read paths compute them alike.

import (
	"encoding/json"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

// totalPayload is the aggregate's own facts as the panel reads them, without
// the series and breakdowns other guards pin.
func totalPayload(t *testing.T, r RangeStats) map[string]any {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	delete(payload, "daily")
	delete(payload, "models")
	delete(payload, "providers")
	return payload
}

// The gap: a range that priced some calls and could not price others carried
// only costIncomplete. The exact old payload said "the floor exists" and
// nothing said how many calls were under it or why.
func TestARangeTotalSaysHowManyCallsItCouldNotPrice(t *testing.T) {
	dir := testenv.TempDir(t)
	writeDay(t, dir, "2026-08-10",
		map[string]any{"ts": "2026-08-10T01:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"total": 100, "requests": 1, "cost_amount": "2", "cost_currency": "USD", "cost_complete": true},
		// A quote that said it could not price the call, and named why.
		map[string]any{"ts": "2026-08-10T02:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"total": 50, "requests": 1, "cost_complete": false, "incomplete_reason": "missing_price_or_usage"},
		map[string]any{"ts": "2026-08-10T03:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"total": 30, "requests": 1, "cost_complete": false, "incomplete_reason": "no_price"},
		// Written before the cost fields existed: no cost fact at all.
		map[string]any{"ts": "2026-08-10T04:00:00Z", "model": "deepseek/deepseek-chat", "source": "cli",
			"total": 20, "requests": 1},
		map[string]any{"ts": "2026-08-10T05:00:00Z", "source": "cli", "turn": true},
	)
	got := queryAll(t, dir)
	payload := totalPayload(t, got)
	t.Logf("total payload = %s", mustJSON(t, payload))

	want := map[string]any{
		"from": "2026-08-01", "to": "2026-08-31",
		"tokens": float64(200), "requests": float64(4), "turns": float64(1),
		"cache_hit": float64(0), "cache_miss": float64(0),
		"cost":           []any{map[string]any{"amount": "2", "currency": "USD"}},
		"costIncomplete": true,
		// Three calls had no money fact: two quotes that said so, one row that
		// predates the cost fields. Two distinct reasons stand behind them.
		"unpricedCalls":     float64(3),
		"incompleteReasons": []any{"missing_price_or_usage", "no_price"},
		"active_days":       float64(1),
		"top_model":         "deepseek/deepseek-chat",
		"top_provider":      "deepseek",
		"agents":            []any{},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("total payload = %s\nwant %s", mustJSON(t, payload), mustJSON(t, want))
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
