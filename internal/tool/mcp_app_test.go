package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPAppSanitizedDropsOversizedNestedImageAndPreservesResultFields(t *testing.T) {
	raw := json.RawMessage(`{
		"content":[
			{"type":"image","data":"` + strings.Repeat("A", 5000) + `","mimeType":"image/png"},
			{"type":"resource","resource":{"blob":"` + strings.Repeat("B", 5000) + `"}},
			{"type":"text","text":"kept"}
		],
		"structuredContent":{"answer":42},
		"isError":false,
		"_meta":{"trace":"local"}
	}`)
	got := (&MCPAppResult{Server: "srv", Tool: "show", RawResult: raw}).Sanitized()
	if got == nil {
		t.Fatal("sanitized result unexpectedly nil")
	}
	text := string(got.RawResult)
	for _, unwanted := range []string{strings.Repeat("A", 100), strings.Repeat("B", 100)} {
		if strings.Contains(text, unwanted) {
			t.Fatal("oversized inline data survived sanitization")
		}
	}
	for _, wanted := range []string{`"text":"kept"`, `"structuredContent":{"answer":42}`, `"isError":false`, `"trace":"local"`} {
		if !strings.Contains(text, wanted) {
			t.Fatalf("result field %s was lost: %s", wanted, text)
		}
	}
}

func TestMCPAppSanitizedEnforcesOneAggregateBudget(t *testing.T) {
	rawPayload, err := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": strings.Repeat("r", 400<<10)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	structured, err := json.Marshal(map[string]any{"value": strings.Repeat("s", 400<<10)})
	if err != nil {
		t.Fatal(err)
	}
	got := (&MCPAppResult{
		Server: "srv", Tool: "show", ResourceURI: "ui://app/main.html",
		RawResult: rawPayload, Structured: structured,
		CSP: map[string][]string{"connect-src": {"https://api.example.test"}},
	}).Sanitized()
	if got == nil {
		t.Fatal("bounded result unexpectedly nil")
	}
	if size := mcpAppPersistedSize(got); size > maxMCPAppBytes {
		t.Fatalf("persisted presentation = %d bytes, limit = %d", size, maxMCPAppBytes)
	}
	if len(got.RawResult) == 0 || len(got.Structured) != 0 {
		t.Fatalf("expected raw result to win over duplicate structured copy: raw=%d structured=%d", len(got.RawResult), len(got.Structured))
	}
}

func TestMCPAppSanitizedKeepsStructuredFallbackWhenRawIsTooLarge(t *testing.T) {
	rawPayload, err := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": strings.Repeat("r", 600<<10)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	structured := json.RawMessage(`{"answer":42}`)
	got := (&MCPAppResult{Server: "srv", Tool: "show", RawResult: rawPayload, Structured: structured}).Sanitized()
	if got == nil || len(got.RawResult) != 0 || string(got.Structured) != string(structured) {
		t.Fatalf("structured fallback not retained: %+v", got)
	}
}
