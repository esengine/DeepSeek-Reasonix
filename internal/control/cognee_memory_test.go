package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func TestCogneeRecallConnectsEnabledServerOnDemand(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	var requests atomic.Int32
	var recalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests.Add(1)
		if len(req.ID) == 0 || string(req.ID) == "null" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"serverInfo":      map[string]any{"name": "Cognee", "version": "test"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "recall", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
					"query": map[string]any{"type": "string"}, "dataset_name": map[string]any{"type": "string"},
				}}},
				{"name": "remember", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
					"data": map[string]any{"type": "string"}, "dataset_name": map[string]any{"type": "string"},
				}}},
			}}
		case "tools/call":
			if req.Params.Name != "recall" {
				t.Errorf("tools/call name = %q, want recall", req.Params.Name)
			}
			if req.Params.Arguments["query"] == "" || req.Params.Arguments["dataset_name"] == "" {
				t.Errorf("recall arguments = %#v, want query and dataset_name", req.Params.Arguments)
			}
			recalls.Add(1)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "remembered project fact"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "cognee"
type = "http"
url = %q
`, server.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil || len(cfg.Plugins) != 1 {
		t.Fatalf("load Cognee config = (%+v, %v)", cfg.Plugins, err)
	}
	if err := config.DefaultMCPActivationStore().SetServerEnabled(cfg.Plugins[0], workspace, true); err != nil {
		t.Fatalf("enable Cognee MCP: %v", err)
	}
	host := plugin.NewHost()
	defer host.Close()
	ctrl := newOwnedTestController(t, Options{
		Host:          host,
		Registry:      tool.NewRegistry(),
		PluginCtx:     context.Background(),
		WorkspaceRoot: workspace,
	})
	defer ctrl.Close()
	if host.HasClientForSpec(ctrl.mcpSpec(cfg.Plugins[0])) {
		t.Fatal("Cognee unexpectedly connected before memory recall")
	}

	memory, notice := ctrl.cogneeRecallContext(context.Background(), "What project decisions were made?", workspace)
	if !strings.Contains(memory, "remembered project fact") || !strings.Contains(notice, "已检索") {
		t.Fatalf("recall = (%q, %q), want remembered context and success notice", memory, notice)
	}
	if got := recalls.Load(); got != 1 {
		t.Fatalf("recall calls = %d, want 1 (MCP requests=%d)", got, requests.Load())
	}
}

func TestCogneeRecallDoesNotConnectDisabledProjectServer(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "cognee"
type = "http"
url = %q
`, server.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrl := newOwnedTestController(t, Options{WorkspaceRoot: workspace})
	defer ctrl.Close()
	if _, notice := ctrl.cogneeRecallContext(context.Background(), "query", workspace); !strings.Contains(notice, "服务未连接或未授权") {
		t.Fatalf("disabled Cognee notice = %q, want a skipped-memory notice", notice)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("disabled Cognee server received %d requests, want no network access", got)
	}
}
