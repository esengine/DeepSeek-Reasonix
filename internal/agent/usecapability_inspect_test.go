package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarshalBoundedInspectHardLimit(t *testing.T) {
	payload := map[string]any{
		"id":           "mcp-tool:svc/read",
		"description":  strings.Repeat("oversized third-party description ", 4000),
		"input_schema": json.RawMessage(`{"type":"object"}`),
		"note":         strings.Repeat("untrusted metadata ", 4000),
	}
	got := marshalBoundedInspect(payload)
	if len(got) > maxInspectBytes {
		t.Fatalf("inspect bytes = %d, want <= %d", len(got), maxInspectBytes)
	}
	if !json.Valid([]byte(got)) {
		t.Fatalf("bounded inspect is invalid JSON: %q", got)
	}
	if !strings.Contains(got, `"truncated": true`) {
		t.Fatalf("bounded inspect did not disclose truncation: %s", got)
	}
}
