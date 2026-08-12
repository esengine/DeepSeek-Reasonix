package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func dynamicToolsMCPServer(t *testing.T, loaded *atomic.Bool, dynamicCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		var result any
		notifyChanged := false
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "dynamic", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
			}
		case "tools/list":
			tools := []map[string]any{{
				"name":        "load_toolset",
				"description": "Load the schematic toolset.",
				"inputSchema": map[string]any{"type": "object"},
			}}
			if loaded.Load() {
				tools = append(tools, map[string]any{
					"name":        "list_schematic_components",
					"description": "List schematic components.",
					"inputSchema": map[string]any{"type": "object"},
				})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(request.Params, &params)
			switch params.Name {
			case "load_toolset":
				loaded.Store(true)
				notifyChanged = true
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "loaded"}}}
			case "list_schematic_components":
				dynamicCalls.Add(1)
				result = map[string]any{"content": []map[string]any{{"type": "text", "text": "R1"}}}
			}
		}

		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
		if notifyChanged {
			w.Header().Set("Content-Type", "text/event-stream")
			notification, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "notifications/tools/list_changed",
			})
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\nevent: message\ndata: %s\n\n", notification, response)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
}

func TestMCPCapabilityRuntimeRefreshesDynamicToolsInSession(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var loaded atomic.Bool
	var dynamicCalls atomic.Int32
	server := dynamicToolsMCPServer(t, &loaded, &dynamicCalls)
	defer server.Close()

	host := plugin.NewHost()
	defer host.Close()
	registry := tool.NewRegistry()
	spec := plugin.Spec{Name: "dynamic", Type: "http", URL: server.URL, Authorized: true}
	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, registry, nil)
	frontend := runtime.NewFrontend(capability.NewLedger(), nil)
	registry.Add(frontend)
	initial, err := host.Add(ctx, spec)
	if err != nil {
		t.Fatalf("Host.Add: %v", err)
	}
	for _, candidate := range initial {
		registry.Add(candidate)
	}
	registry.SetProviderVisibleTools([]string{"use_capability"})
	providerSchemasBefore := registry.Schemas()

	if _, err := frontend.Execute(ctx, json.RawMessage(`{"action":"call","capability_id":"mcp-tool:dynamic/load_toolset","arguments":{}}`)); err != nil {
		t.Fatalf("load_toolset: %v", err)
	}

	wantName := "mcp__dynamic__list_schematic_components"
	deadline := time.Now().Add(2 * time.Second)
	var live []plugin.CachedTool
	for time.Now().Before(deadline) {
		live = runtime.ConnectedProxyTools()["dynamic"]
		if hasCachedTool(live, "list_schematic_components") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := registry.Get(wantName); !ok {
		t.Fatal("dynamic MCP tool was not registered for use_capability routing")
	}
	if got := registry.Schemas(); !reflect.DeepEqual(got, providerSchemasBefore) {
		t.Fatalf("provider-visible schemas changed after dynamic MCP refresh: before=%v after=%v", providerSchemasBefore, got)
	}
	if len(live) != 2 || !hasCachedTool(live, "list_schematic_components") {
		t.Fatalf("live capability tools = %+v, want refreshed dynamic tool", live)
	}
	if _, err := frontend.Execute(ctx, json.RawMessage(`{"action":"call","capability_id":"mcp-tool:dynamic/list_schematic_components","arguments":{}}`)); err != nil {
		t.Fatalf("dynamic tool call: %v", err)
	}
	if got := dynamicCalls.Load(); got != 1 {
		t.Fatalf("dynamic tools/call count = %d, want 1", got)
	}
}

func hasCachedTool(tools []plugin.CachedTool, name string) bool {
	return slices.ContainsFunc(tools, func(candidate plugin.CachedTool) bool {
		return candidate.Name == name
	})
}
