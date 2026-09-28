package boot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/plugin"
)

// deployMCPServer offers one tool with no read-only hint. writeTo, when set,
// is a file the call writes, standing in for a local server that edits the
// workspace; otherwise the call acts only on the server's side.
func deployMCPServer(t *testing.T, writeTo string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "ops", "version": "1"},
				"capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "deploy", "description": "Deploy.", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if writeTo != "" {
				if err := os.WriteFile(writeTo, []byte("deployed\n"), 0o644); err != nil {
					t.Error(err)
				}
			}
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "deployed"}}}
		default:
			http.Error(w, "unsupported method", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
}

// An MCP call is a change to the workspace only when the workspace shows one.
// One that acts on the server's side owes no check afterwards; one that wrote
// a file here owes it, as any change does.
func TestEffectAnMCPCallOwesACheckOnlyWhenTheWorkspaceChanged(t *testing.T) {
	var rec *capabilityCallProvider
	provider.Register("boot-mcp-mutation", func(provider.Config) (provider.Provider, error) { return rec, nil })
	for _, writes := range []bool{false, true} {
		isolateConfigHome(t)
		dir := robustTempDir(t)
		t.Chdir(dir)
		rec = &capabilityCallProvider{direct: "mcp__ops__deploy"}
		writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-mcp-mutation"
model = "x"
`)
		approveWorkspace(t, dir)
		target := ""
		if writes {
			target = filepath.Join(dir, "deploy.log")
		}
		server := deployMCPServer(t, target)
		ctrl, err := Build(context.Background(), Options{
			Sink:         event.Discard,
			ExtraPlugins: []plugin.Spec{{Name: "ops", Type: "http", URL: server.URL, Authorized: true}},
		})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if err := ctrl.Run(context.Background(), "deploy"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		results := effectToolResults(rec.last())
		ctrl.Close()
		server.Close()
		if len(results) != 1 || !strings.Contains(results[0], "deployed") {
			t.Fatalf("writes=%v: tool results = %q", writes, results)
		}
		if owes := strings.Contains(results[0], "stale_verification"); owes != writes {
			t.Fatalf("writes=%v: owes a check = %v; result:\n%s", writes, owes, results[0])
		}
	}
}
