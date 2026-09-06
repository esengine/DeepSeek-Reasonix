package responses

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reasonix/internal/provider"
	"reflect"
	"strings"
	"testing"
)

func TestTerminalReasoningSnapshotReplacesEarlierOpaqueItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.done","item":{"id":"rs_1","type":"reasoning","status":"completed","summary":[]}}`,
			`{"type":"response.completed","response":{"id":"resp_1","output":[{"id":"rs_1","type":"reasoning","status":"completed","summary":[],"encrypted_content":"opaque-final"}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
		)
	}))
	defer server.Close()
	chunks := collect(t, New(Config{Name: "compatible", APIKey: "key", BaseURL: server.URL, Model: "reasoner", Mode: "stateless"}), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}})
	var items []json.RawMessage
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkResponsesItem {
			items = append(items, chunk.ResponsesItem)
		}
	}
	if len(items) != 1 || !strings.Contains(string(items[0]), "opaque-final") {
		t.Fatalf("terminal items=%s", items)
	}
	input := messagesToInput([]provider.Message{{Role: provider.RoleAssistant, ReasoningContent: "legacy summary", ResponsesItems: items}}, false, false, true)
	raw, _ := json.Marshal(input)
	if strings.Count(string(raw), `"type":"reasoning"`) != 1 || !strings.Contains(string(raw), "opaque-final") || strings.Contains(string(raw), "legacy summary") {
		t.Fatalf("replay=%s", raw)
	}
}

func TestDeepSeekPlainReasoningItemRemainsUnmodified(t *testing.T) {
	raw := json.RawMessage(`{"id":"rs_1","type":"reasoning","status":"completed","content":[{"type":"reasoning_text","text":"original"}]}`)
	input := messagesToInput([]provider.Message{{Role: provider.RoleAssistant, ResponsesItems: []json.RawMessage{raw}}}, false, false, false)
	encoded, _ := json.Marshal(input[0])
	var got, want any
	_ = json.Unmarshal(encoded, &got)
	_ = json.Unmarshal(raw, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%s want=%s", encoded, raw)
	}
}
