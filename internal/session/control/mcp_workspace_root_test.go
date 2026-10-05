package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

func TestAddMCPServerExpandsWorkspaceRootInLiveHeaders(t *testing.T) {
	isolateControlConfigHome(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "/inherited/from/a/parent/hook")
	workspace := testenv.TempDir(t)
	want, err := filepath.Abs(workspace)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var project, claude string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Method == "initialize" {
			mu.Lock()
			project, claude = r.Header.Get("X-Project"), r.Header.Get("X-Claude")
			mu.Unlock()
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"serverInfo":      map[string]any{"name": "idea", "version": "1"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name": "search", "description": "Search.", "inputSchema": map[string]any{"type": "object"},
			}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()

	host := plugin.NewHost()
	defer host.Close()
	ctrl := New(Options{Host: host, Registry: tool.NewRegistry(), PluginCtx: context.Background(), WorkspaceRoot: workspace})
	entry := config.PluginEntry{Name: "idea", Type: "http", URL: server.URL, Headers: map[string]string{
		"X-Project": "${REASONIX_WORKSPACE_ROOT}",
		"X-Claude":  "${CLAUDE_PROJECT_DIR}",
	}}
	if n, err := ctrl.AddMCPServer(entry); err != nil || n != 1 {
		t.Fatalf("AddMCPServer(idea) = (%d, %v), want one connected tool", n, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if project != want || claude != want {
		t.Fatalf("live headers = (%q, %q), want the workspace root %q for both", project, claude, want)
	}
}
