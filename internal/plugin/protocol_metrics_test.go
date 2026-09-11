package plugin

import (
	"encoding/json"
	"testing"
	"time"
)

func TestObserveProtocolRecordsToolsListShape(t *testing.T) {
	ResetProtocolMetricsForTest()
	c := &Client{name: "m"}
	res, _ := json.Marshal(map[string]any{"tools": []map[string]any{
		{"name": "a", "inputSchema": map[string]any{"type": "object"}},
		{"name": "b", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}},
	}})
	c.observeProtocol("tools/list", res, 12*time.Millisecond, nil)
	got := SnapshotToolsListStats()
	if got.Count != 1 || got.Remote != 1 || got.ToolCount != 2 || got.DurationMs < 12 || got.SchemaBytes <= 0 {
		t.Fatalf("stats = %+v", got)
	}
}
