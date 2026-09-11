package plugin

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

type protocolListStats struct {
	durationMs  atomic.Int64
	toolCount   atomic.Int64
	schemaBytes atomic.Int64
}

type protocolMetrics struct {
	toolsList            atomic.Int64
	toolsCall            atomic.Int64
	remote               atomic.Int64
	outputSchemaMismatch atomic.Int64
	lists                protocolListStats
}

// ToolsListStats is a snapshot of MCP tools/list observations.
type ToolsListStats struct {
	Count, Remote, DurationMs, ToolCount, SchemaBytes int64
}

var hostProtocol protocolMetrics

func (c *Client) observeProtocol(method string, res json.RawMessage, d time.Duration, err error) {
	if c == nil {
		return
	}
	switch method {
	case "tools/list":
		hostProtocol.toolsList.Add(1)
		if err == nil {
			hostProtocol.remote.Add(1)
			n, schemaBytes := countListedTools(res)
			hostProtocol.lists.durationMs.Add(d.Milliseconds())
			hostProtocol.lists.toolCount.Add(int64(n))
			hostProtocol.lists.schemaBytes.Add(int64(schemaBytes))
		}
	case "tools/call":
		if err == nil {
			hostProtocol.toolsCall.Add(1)
		}
	}
}

func countListedTools(res json.RawMessage) (n, schemaBytes int) {
	var out struct {
		Tools []struct {
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if json.Unmarshal(res, &out) != nil {
		return 0, len(res)
	}
	for _, item := range out.Tools {
		schemaBytes += len(item.InputSchema)
	}
	return len(out.Tools), schemaBytes
}

func ToolsListCount() int64            { return hostProtocol.toolsList.Load() }
func ToolsCallCount() int64            { return hostProtocol.toolsCall.Load() }
func OutputSchemaMismatchCount() int64 { return hostProtocol.outputSchemaMismatch.Load() }

// SnapshotToolsListStats returns process-local tools/list counters.
func SnapshotToolsListStats() ToolsListStats {
	return ToolsListStats{
		Count:       hostProtocol.toolsList.Load(),
		Remote:      hostProtocol.remote.Load(),
		DurationMs:  hostProtocol.lists.durationMs.Load(),
		ToolCount:   hostProtocol.lists.toolCount.Load(),
		SchemaBytes: hostProtocol.lists.schemaBytes.Load(),
	}
}

func ResetProtocolMetricsForTest() {
	hostProtocol.toolsList.Store(0)
	hostProtocol.toolsCall.Store(0)
	hostProtocol.remote.Store(0)
	hostProtocol.outputSchemaMismatch.Store(0)
	hostProtocol.lists.durationMs.Store(0)
	hostProtocol.lists.toolCount.Store(0)
	hostProtocol.lists.schemaBytes.Store(0)
}
