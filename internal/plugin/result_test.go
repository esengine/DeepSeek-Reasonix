package plugin

import (
	"strings"
	"testing"
)

func TestParseToolResultStructuredContentVariants(t *testing.T) {
	text, _, err := parseToolResult([]byte(`{"content":[],"structuredContent":{"ok":true}}`))
	if err != nil || text != `{"ok":true}` {
		t.Fatalf("empty text used structuredContent: %q %v", text, err)
	}
	text, _, err = parseToolResult([]byte(`{"content":[{"type":"text","text":"{\"ok\":true}"}],"structuredContent":{"ok":true}}`))
	if err != nil || text != `{"ok":true}` {
		t.Fatalf("equal JSON must collapse: %q %v", text, err)
	}
	text, _, err = parseToolResult([]byte(`{"content":[{"type":"text","text":"hello"}],"structuredContent":{"ok":true}}`))
	if err != nil || !strings.Contains(text, "hello") || !strings.Contains(text, "structured content") {
		t.Fatalf("unequal content must keep both: %q %v", text, err)
	}
	text, _, err = parseToolResult([]byte(`{"content":[{"type":"text","text":"boom"}],"isError":true}`))
	if err == nil || text != "boom" {
		t.Fatalf("error result: %q %v", text, err)
	}
	classified, ok := err.(interface{ RetryableToolError() bool })
	if !ok || classified.RetryableToolError() {
		t.Fatalf("MCP isError must be a typed non-retryable application error: %T", err)
	}
}

func TestOutputSchemaMismatchIsTelemetryOnly(t *testing.T) {
	ResetProtocolMetricsForTest()
	text, _, err := parseToolResultWithSchema(
		[]byte(`{"content":[],"structuredContent":{"ok":"not-a-boolean"}}`),
		[]byte(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"]}`),
	)
	if err != nil || text == "" {
		t.Fatalf("third-party output mismatch must not fail: text=%q err=%v", text, err)
	}
	if got := OutputSchemaMismatchCount(); got != 1 {
		t.Fatalf("mismatch count = %d, want 1", got)
	}
}
