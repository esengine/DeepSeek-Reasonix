package responses

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/provider"
)

func TestStreamEmitsTypedServerSearchFromWebSearchCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeEvents(w,
			`{"type":"response.output_item.done","item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"latest release","sources":[{"url":"https://api-docs.deepseek.com/updates/"}]}}}`,
			`{"type":"response.output_text.delta","item_id":"msg_1","content_index":0,"delta":"found it"}`,
			`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}`,
		)
	}))
	defer server.Close()

	chunks := collect(t, New(Config{Name: "compatible", APIKey: "key", BaseURL: server.URL, Model: "deepseek-v4-flash", Mode: "stateless", WebSearch: true}), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "search"}},
	})
	var searches []provider.ServerSearchCall
	for _, chunk := range chunks {
		if chunk.Type == provider.ChunkServerSearch && chunk.ServerSearch != nil {
			searches = provider.MergeServerSearch(searches, *chunk.ServerSearch)
		}
	}
	if len(searches) != 1 || searches[0].ID != "ws_1" || searches[0].Query != "latest release" {
		t.Fatalf("typed search chunks = %#v", searches)
	}
}
