package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	_ "reasonix/internal/provider/responses"
	"reasonix/internal/tool"
)

func TestIndependentSearchWireAndMainReplay(t *testing.T) {
	for _, kind := range []string{"anthropic", "responses"} {
		t.Run(kind, func(t *testing.T) {
			var mu sync.Mutex
			var requests []map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				mu.Lock()
				requests = append(requests, body)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if kind == "anthropic" {
					fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":0}}}\n\n")
					fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s1\",\"content\":[{\"type\":\"web_search_result\",\"title\":\"Docs\",\"url\":\"https://example.com/docs\",\"encrypted_content\":\"SECRET\"}]}}\n\n")
					fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
					fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Search summary\"}}\n\n")
					fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":7}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					fmt.Fprint(w, `data: {"type":"response.output_item.done","item":{"id":"s1","type":"web_search_call","status":"completed","action":{"type":"search","queries":["test"]}}}

data: {"type":"response.output_item.done","item":{"id":"s2","type":"web_search_call","status":"completed","action":{"type":"open_page","url":"https://example.com/docs"}}}

`)
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Search summary\"}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":7,\"total_tokens\":19}}}\n\n")
				}
			}))
			defer srv.Close()
			on := true
			entry := config.ProviderEntry{Name: "search", Kind: kind, BaseURL: srv.URL, Model: "m", WebSearch: &on, Thinking: "enabled", ResponsesMode: "stateful"}
			cfg := &config.Config{Providers: []config.ProviderEntry{entry}}
			reg := tool.NewRegistry()
			var usageEvents []event.Event
			addWebSearch(reg, cfg, &entry, netclient.ProxySpec{Mode: netclient.ModeOff}, event.FuncSink(func(e event.Event) { usageEvents = append(usageEvents, e) }))
			applyUnifiedProviderToolSurface(reg)
			if schemas := reg.Schemas(); len(schemas) != 1 || schemas[0].Name != "web_search" {
				t.Fatalf("search not exposed: %+v", schemas)
			}
			search, ok := reg.Get("web_search")
			if !ok {
				t.Fatal("missing search tool")
			}
			for _, query := range []string{"first", "second"} {
				output, err := search.Execute(context.Background(), json.RawMessage(`{"query":"`+query+`"}`))
				if err != nil {
					t.Fatal(err)
				}
				sources := provider.ParseServerSearchOutput(output)
				if len(sources) != 1 || sources[0].URL != "https://example.com/docs" || !strings.Contains(output, "Search summary") || strings.Contains(output, "SECRET") {
					t.Fatalf("bad output: %s", output)
				}
			}
			if len(usageEvents) != 2 || usageEvents[0].UsageSource != "web-search" || usageEvents[0].Usage.RequestCount != 1 {
				t.Fatalf("usage not accounted: %+v", usageEvents)
			}
			main, err := NewProviderWithProxy(&entry, netclient.ProxySpec{Mode: netclient.ModeOff})
			if err != nil {
				t.Fatal(err)
			}
			old := provider.Message{Role: provider.RoleAssistant, Content: "old search"}
			if kind == "anthropic" {
				old.ServerSearch = []provider.ServerSearchCall{{ID: "old", Query: "legacy", Raw: json.RawMessage(`[]`)}}
			} else {
				old.ResponsesItems = []json.RawMessage{json.RawMessage(`{"id":"old","type":"web_search_call","status":"completed","action":{"type":"search","query":"legacy"}}`)}
			}
			stream, err := main.Stream(context.Background(), provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "MAIN HISTORY"}, old}, Tools: reg.Schemas()})
			if err != nil {
				t.Fatal(err)
			}
			for range stream {
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 3 {
				t.Fatalf("got %d requests", len(requests))
			}
			for i, req := range requests {
				tools := req["tools"].([]any)
				if len(tools) != 1 {
					t.Fatalf("duplicate or missing tools: %+v", tools)
				}
				wireTool := tools[0].(map[string]any)
				if i < 2 {
					if wireTool["type"] != "web_search_20250305" && wireTool["type"] != "web_search" {
						t.Fatalf("search lacks native tool: %+v", wireTool)
					}
					b, _ := json.Marshal(req)
					if strings.Contains(string(b), "MAIN HISTORY") || strings.Contains(string(b), "previous_response_id") || (i == 1 && strings.Contains(string(b), "first")) {
						t.Fatalf("search inherited history: %s", b)
					}
				} else {
					if wireTool["type"] == "web_search" || wireTool["type"] == "web_search_20250305" {
						t.Fatal("main request still contains native search")
					}
					b, _ := json.Marshal(req)
					if !strings.Contains(string(b), `"old"`) || !strings.Contains(string(b), `legacy`) {
						t.Fatalf("legacy search replay was lost: %s", b)
					}
				}
			}
		})
	}
}

func TestIndependentSearchDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	for _, kind := range []string{"anthropic", "responses"} {
		on := true
		entry := config.ProviderEntry{Name: "search", Kind: kind, BaseURL: source.URL, Model: "m", WebSearch: &on}
		reg := tool.NewRegistry()
		addWebSearch(reg, &config.Config{}, &entry, netclient.ProxySpec{Mode: netclient.ModeOff}, event.Discard)
		search, _ := reg.Get("web_search")
		if _, err := search.Execute(context.Background(), json.RawMessage(`{"query":"test"}`)); err == nil {
			t.Fatalf("%s redirect accepted", kind)
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("search followed a credential-bearing redirect")
	}
}

func TestBuildExposesIndependentSearch(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `default_model = "local/m"
[[providers]]
name = "local"
kind = "anthropic"
base_url = "http://localhost:12345"
model = "m"
web_search = true
`)
	ctrl, err := Build(context.Background(), Options{Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	// The assembled provider-visible inventory is part of the runtime snapshot.
	for _, entry := range ctrl.ToolContractEntries() {
		if entry.Name == "web_search" {
			return
		}
	}
	t.Fatal("Build did not install search")
}

func TestIndependentSearchHonorsOfflineAndToolAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name    string
		offline bool
		enabled []string
		want    bool
	}{
		{"default", false, nil, true},
		{"offline", true, nil, false},
		{"excluded", false, []string{"bash"}, false},
		{"included", false, []string{"web_search"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Environment.Offline = tc.offline
			cfg.Tools.Enabled = tc.enabled
			on := true
			entry := &config.ProviderEntry{Name: "search", Kind: "anthropic", BaseURL: "http://localhost:8080", Model: "m", WebSearch: &on}
			reg := tool.NewRegistry()
			addWebSearch(reg, cfg, entry, netclient.ProxySpec{Mode: netclient.ModeOff}, event.Discard)
			_, found := reg.Get("web_search")
			if found != tc.want {
				t.Fatalf("registered=%v want %v", found, tc.want)
			}
		})
	}
}
