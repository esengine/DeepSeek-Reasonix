package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRunPersistsServerSearchAndEmitsCardEvents(t *testing.T) {
	search := provider.ServerSearchCall{
		ID:      "s1",
		Query:   "latest",
		Results: []provider.ServerSearchHit{{Title: "Change Log", URL: "https://api-docs.deepseek.com/updates/"}},
		Raw:     json.RawMessage(`[{"title":"Change Log","encrypted_content":"xxx"}]`),
	}
	sink := &recordSink{}
	prov := testutil.NewMock("deepseek", testutil.Turn{Chunks: []provider.Chunk{
		{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: search.ID}},
		{Type: provider.ChunkServerSearch, ServerSearch: &provider.ServerSearchCall{ID: search.ID, Query: search.Query}},
		{Type: provider.ChunkServerSearch, ServerSearch: &search},
		{Type: provider.ChunkText, Text: "answer only"},
		{Type: provider.ChunkDone},
	}})
	session := NewSession("system")
	agent := New(prov, tool.NewRegistry(), session, Options{}, sink)
	if err := agent.Run(context.Background(), "search"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	assistant := session.Snapshot()[len(session.Snapshot())-1]
	if assistant.Content != "answer only" || len(assistant.ServerSearch) != 1 || assistant.ServerSearch[0].ID != "s1" || assistant.ServerSearch[0].Query != "latest" {
		t.Fatalf("persisted assistant = %#v", assistant)
	}
	if string(assistant.ServerSearch[0].Raw) != string(search.Raw) {
		t.Fatalf("persisted raw = %s", assistant.ServerSearch[0].Raw)
	}

	dispatches := sink.kinds(event.ToolDispatch)
	results := sink.kinds(event.ToolResult)
	if len(dispatches) == 0 || dispatches[0].Tool.Name != "web_search" || dispatches[0].Tool.ID != "s1" {
		t.Fatalf("dispatches = %#v", dispatches)
	}
	if len(results) != 1 || results[0].Tool.Name != "web_search" || !strings.Contains(results[0].Tool.Output, "Change Log") {
		t.Fatalf("results = %#v", results)
	}

	path := filepath.Join(t.TempDir(), "server-search.jsonl")
	if err := session.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	loadedAssistant := loaded.Messages[len(loaded.Messages)-1]
	if len(loadedAssistant.ServerSearch) != 1 || loadedAssistant.ServerSearch[0].Query != "latest" {
		t.Fatalf("reloaded ServerSearch = %#v", loadedAssistant.ServerSearch)
	}
}
