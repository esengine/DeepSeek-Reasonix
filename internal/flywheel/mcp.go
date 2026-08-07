package flywheel

import (
	"encoding/json"
	"time"
)

// MCPCallLine is one mcp.call.v1 record (§2.2 of DATA_FLYWHEEL.md), aligned
// with the OTel gen-ai/mcp semantic conventions.
type MCPCallLine struct {
	Schema      string    `json:"schema"`
	Ts          time.Time `json:"ts"`
	Server      string    `json:"server"`
	Tool        string    `json:"tool"`
	ArgsDigest  string    `json:"args_digest,omitempty"`
	ResultDigest string   `json:"result_digest,omitempty"`
	DurationMs  int64     `json:"duration_ms,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
}

// MCPRecorder records MCP server tool calls into the flywheel mcp JSONL.
// It is a plain recorder (not an event.Sink) because MCP calls may originate
// outside the agent event stream (external clients, subagents).
type MCPRecorder struct {
	writer *Writer
}

// NewMCPRecorder builds an MCP recorder writing under dir (e.g. flywheel/mcp).
func NewMCPRecorder(dir string) *MCPRecorder {
	return &MCPRecorder{writer: NewWriter(dir)}
}

// Record snapshots one MCP tool call.
func (r *MCPRecorder) Record(server, tool, args, result string, duration time.Duration, errCode string) {
	if r == nil || r.writer == nil {
		return
	}
	line := MCPCallLine{
		Schema:       "mcp.call.v1",
		Ts:           time.Now().UTC(),
		Server:       server,
		Tool:         tool,
		ArgsDigest:   Digest(args),
		ResultDigest: Digest(result),
		DurationMs:   duration.Milliseconds(),
		ErrorCode:    errCode,
	}
	buf, err := json.Marshal(line)
	if err != nil {
		return
	}
	r.writer.Append(buf)
}

// Close flushes and closes the underlying writer.
func (r *MCPRecorder) Close() {
	if r != nil && r.writer != nil {
		r.writer.Close()
	}
}
