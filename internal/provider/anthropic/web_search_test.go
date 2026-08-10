package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// TestBuildRequestWebSearchServerTool covers the tools-array shape when the
// server-side web_search tool is enabled: it is prepended as a typed entry
// without an input_schema, and named tools keep their schema untouched.
func TestBuildRequestWebSearchServerTool(t *testing.T) {
	c := &client{name: "deepseek", model: "deepseek-v4-flash", webSearch: true}
	r := c.buildRequest(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools:    []provider.ToolSchema{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if len(r.Tools) != 2 {
		t.Fatalf("want 2 tools (web_search + read_file), got %d: %+v", len(r.Tools), r.Tools)
	}
	if r.Tools[0].Type != "web_search_20250305" || r.Tools[0].Name != "web_search" {
		t.Fatalf("tools[0] = %+v, want typed web_search server tool", r.Tools[0])
	}
	if len(r.Tools[0].InputSchema) != 0 {
		t.Fatalf("server tool must not carry input_schema, got %s", r.Tools[0].InputSchema)
	}
	if r.Tools[1].Type != "" || r.Tools[1].Name != "read_file" || len(r.Tools[1].InputSchema) == 0 {
		t.Fatalf("tools[1] = %+v, want named tool with schema and no type", r.Tools[1])
	}

	// Disabled (default) ⇒ no server tool is injected.
	off := &client{name: "deepseek", model: "deepseek-v4-flash"}
	r = off.buildRequest(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		Tools:    []provider.ToolSchema{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}},
	})
	if len(r.Tools) != 1 || r.Tools[0].Name != "read_file" {
		t.Fatalf("webSearch off: tools = %+v, want only read_file", r.Tools)
	}
}

// TestAnthToolWireShape pins the JSON encoding both tool kinds put on the wire:
// the typed server tool omits input_schema entirely, and the omitempty on
// input_schema must not leak into named tools (every named tool keeps a schema
// because buildRequest substitutes a default for empty parameters).
func TestAnthToolWireShape(t *testing.T) {
	server, err := json.Marshal(anthTool{Type: "web_search_20250305", Name: "web_search"})
	if err != nil {
		t.Fatalf("marshal server tool: %v", err)
	}
	if got := string(server); got != `{"type":"web_search_20250305","name":"web_search"}` {
		t.Fatalf("server tool wire = %s", got)
	}

	named, err := json.Marshal(anthTool{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatalf("marshal named tool: %v", err)
	}
	if got := string(named); got != `{"name":"read_file","input_schema":{"type":"object"}}` {
		t.Fatalf("named tool wire = %s", got)
	}
}

func TestFormatWebSearchResults(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty payload", "", ""},
		{"malformed json", `{"not":"an array"`, ""},
		{"non-array json", `{"title":"x"}`, ""},
		{"empty array", `[]`, ""},
		{"all entries blank", `[{"text":"body only"},{}]`, ""},
		{
			// DeepSeek returns encrypted_content alongside title/url; unknown
			// fields must be ignored rather than failing the whole block.
			"titles and urls",
			`[{"type":"web_search_result","title":"Change Log","url":"https://api-docs.deepseek.com/updates/","encrypted_content":"xxx"},{"title":"No URL"}]`,
			"\n\n- **Change Log**\n  <https://api-docs.deepseek.com/updates/>\n- **No URL**\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatWebSearchResults(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("formatWebSearchResults(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestStreamSurfacesWebSearchResults drives a full SSE round-trip: a
// web_search_tool_result block must surface as readable text chunks, and the
// server_tool_use block (the model initiating the search) must not be mistaken
// for a client tool call.
func TestStreamSurfacesWebSearchResults(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		``,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"s1","name":"web_search"}}`,
		``,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"s1","content":[{"title":"Change Log","url":"https://api-docs.deepseek.com/updates/"}]}}`,
		``,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"text"}}`,
		``,
		`data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"answer"}}`,
		``,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	p, err := New(provider.Config{Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-v4-flash", APIKey: "k", Extra: map[string]any{"web_search": true}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ch, err := p.Stream(context.Background(), provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "search something"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkToolCallStart, provider.ChunkToolCall:
			t.Fatalf("server-side search must not surface as a client tool call, got %+v", chunk)
		case provider.ChunkError:
			t.Fatalf("stream error: %v", chunk.Err)
		}
	}
	got := text.String()
	if !strings.Contains(got, "**Change Log**") || !strings.Contains(got, "<https://api-docs.deepseek.com/updates/>") {
		t.Fatalf("stream text missing formatted search results: %q", got)
	}
	if !strings.Contains(got, "answer") {
		t.Fatalf("stream text missing model answer: %q", got)
	}
}
