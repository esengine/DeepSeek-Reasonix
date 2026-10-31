package stats

import (
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestAgentAndUnpricedFactsSurviveEveryUsageReadPath(t *testing.T) {
	dir := testenv.TempDir(t)
	filter := rangeFromDays(t, "2026-09-01", "2026-09-30")
	live := queryLiveProjection(t, dir, filter,
		map[string]any{"ts": "2026-09-01T01:00:00Z", "source": "serve", "model": "relay/m", "agent": "a-1",
			"total": 40, "requests": 1, "cost_complete": false, "incomplete_reason": "missing_price_or_usage"},
		map[string]any{"ts": "2026-09-02T01:00:00Z", "source": "serve", "model": "relay/m", "agent": "a-2",
			"total": 60, "requests": 1, "cost_amount": "1.5", "cost_currency": "USD", "cost_complete": true},
	)
	files, err := NewWriter(dir).Query(filter)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]RangeStats{"live": live, "JSONL": files, "rebuilt": queryProjection(t, dir, filter)} {
		want := []AgentUsage{{Agent: "a-2", Tokens: 60, Requests: 1, Percent: 60}, {Agent: "a-1", Tokens: 40, Requests: 1, Percent: 40}}
		if !reflect.DeepEqual(got.Agents, want) || got.UnpricedCalls != 1 || !reflect.DeepEqual(got.IncompleteReasons, []string{"missing_price_or_usage"}) {
			t.Errorf("%s lost agent or unpriced facts: %+v", name, got)
		}
		if !reflect.DeepEqual(got, files) {
			t.Errorf("%s disagrees with JSONL: got %+v, want %+v", name, got, files)
		}
	}
}
