package boot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/state/agentpreset"
)

type compositionLateMCPTool struct{ tool.Tool }

func (compositionLateMCPTool) Name() string           { return "mcp__docs__unbound" }
func (compositionLateMCPTool) MCPServerName() string  { return "docs" }
func (compositionLateMCPTool) MCPRawToolName() string { return "unbound" }

func TestCompositionSessionsKeepIndependentMCPParametersAndConnections(t *testing.T) {
	var mu sync.Mutex
	nextID := 0
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			mu.Lock()
			nextID++
			id := fmt.Sprint(nextID)
			mu.Unlock()
			w.Header().Set("Mcp-Session-Id", id)
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "docs", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "echo", "description": "Echo session parameters", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		case "tools/call":
			id := r.Header.Get("Mcp-Session-Id")
			mu.Lock()
			calls[id]++
			count := calls[id]
			mu.Unlock()
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("%s:%s:%d", r.URL.Path, r.Header.Get("X-Flavor"), count)}}}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
	defer server.Close()
	home, root := compositionTestWorkspace(t, "openai")
	t.Setenv("COMPOSITION_LEFT", "left")
	t.Setenv("COMPOSITION_RIGHT", "right")
	store := agentpreset.New(home, root)
	results := []*BuildResult{}
	for _, side := range []string{"left", "right"} {
		d := agentcomposition.Builtins()[0]
		d.ID = "user:" + side
		params, _ := json.Marshal(map[string]any{"url": server.URL + "/" + side, "headers": map[string]string{"X-Flavor": "${COMPOSITION_" + strings.ToUpper(side) + "}"}})
		d.MCP = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "docs", Parameters: params}}}
		if _, err := store.Save(d, ""); err != nil {
			t.Fatal(err)
		}
		built, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: d.ID, resolvedShell: pinnedEffectShell(), ExtraPlugins: []plugin.Spec{{Name: "docs", Type: "http", URL: server.URL, Authorized: true}}})
		if err != nil {
			t.Fatal(err)
		}
		defer built.Controller.Close()
		reader, ok := built.Assembly.Registry.Get("read_file")
		if !ok {
			t.Fatal("reader missing")
		}
		built.Assembly.Registry.Add(compositionLateMCPTool{Tool: reader})
		if _, ok := built.Assembly.Registry.Get("mcp__docs__unbound"); ok {
			t.Fatal("late tool widened a selected MCP server's bound surface")
		}
		results = append(results, built)
	}
	for _, step := range []struct {
		index, count int
		side         string
	}{{0, 1, "left"}, {1, 1, "right"}, {0, 2, "left"}} {
		proxy, ok := results[step.index].Assembly.Registry.Get("use_capability")
		if !ok {
			t.Fatal("proxy missing")
		}
		out, err := proxy.Execute(t.Context(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:docs/echo","arguments":{}}`))
		if err != nil || !strings.Contains(out, fmt.Sprintf("/%s:%s:%d", step.side, step.side, step.count)) {
			t.Fatalf("session parameters leaked: %s %v", out, err)
		}
	}
}
