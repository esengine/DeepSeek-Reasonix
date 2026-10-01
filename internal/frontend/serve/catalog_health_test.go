package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/session/control"
)

// The /mcp endpoint no longer builds each row itself: it reads one health list
// and then decides, per row, whether the live server, the recorded failure, or
// the schema cache can answer. Every one of those decisions is invisible to a
// unit test of the health list, and dropping one leaves the other tests in the
// tree green. This drives the endpoint itself.
func TestMcpEndpointAnswersEveryRowFromItsOwnSource(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A server that answers a handshake, so the row has a live connection to
	// read transport, source, description and tools from.
	live := newMCPHTTPServer(t, func() any {
		return map[string]any{"tools": []map[string]any{{
			"name":        "greet",
			"description": "Greet someone.",
			"inputSchema": map[string]any{"type": "object"},
			"annotations": map[string]any{"readOnlyHint": true},
		}}}
	})
	defer live.Close()

	// A server that refuses, so the row is a failure with a status code.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "go away", http.StatusUnauthorized)
	}))
	defer refusing.Close()

	// A server that never answers, so the spawn stays in flight and the row is
	// connecting rather than ready or failed.
	release := make(chan struct{})
	contacted := make(chan struct{}, 1)
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case contacted <- struct{}{}:
		default:
		}
		<-release
	}))
	defer func() { close(release); hanging.Close() }()

	// A project-declared server with a schema cache but no live connection. It
	// is nobody-has-answered-for-it off, which is a different row from a server
	// the user switched off, and its tool list can only come from the cache.
	atlasURL := "http://127.0.0.1:9/atlas"
	writePluginFile(t, filepath.Join(root, "reasonix.toml"), `
[[plugins]]
name = "atlas"
type = "http"
url = "`+atlasURL+`"
`)
	atlasSpec := plugin.Spec{Name: "atlas", Type: "http", URL: atlasURL, Dir: root, WorkspaceRoot: root}
	if err := plugin.SaveCachedSchema("atlas", plugin.CachedSchema{
		CacheKey:     plugin.SchemaCacheKey(atlasSpec),
		Instructions: "Maps a repository's symbols.",
		Tools: []plugin.CachedTool{{
			Name: "find_symbol", Description: "Locate a symbol by name.",
			Schema: json.RawMessage(`{"type":"object"}`), ReadOnly: true,
		}},
	}); err != nil {
		t.Fatalf("seed the schema cache: %v", err)
	}

	host, _ := plugin.StartAvailable(ctx, []plugin.Spec{
		{Name: "docs", Type: "http", URL: live.URL, ConfigSource: "user_config"},
		{Name: "broken", Type: "http", URL: refusing.URL},
	})
	defer host.Close()

	host.EnsureConnectedInBackground(ctx, plugin.Spec{Name: "ghost", Type: "http", URL: hanging.URL})
	select {
	case <-contacted:
	case <-time.After(10 * time.Second):
		t.Fatal("the hanging server was never contacted; the connecting row cannot be tested")
	}

	ctrl := control.New(control.Options{Host: host, WorkspaceRoot: root})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Servers []mcpEntry `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	rows := map[string]mcpEntry{}
	for _, row := range got.Servers {
		rows[row.Name] = row
	}

	docs, ok := rows["docs"]
	if !ok {
		t.Fatalf("the connected server is missing: %+v", got.Servers)
	}
	if docs.State != "ready" || docs.Transport != "http" || docs.Source != "user_config" {
		t.Errorf("ready row = %+v, want the live transport and config source", docs)
	}
	if docs.Description != "A live MCP server." || docs.Tools != 1 || len(docs.ToolList) != 1 {
		t.Errorf("ready row = %+v, want what the server itself said", docs)
	}
	if docs.Remembered {
		t.Error("a live row claimed its answer came from the cache")
	}

	broken, ok := rows["broken"]
	if !ok {
		t.Fatalf("the refused server is missing: %+v", got.Servers)
	}
	if broken.State != "failed" || broken.Transport != "http" || broken.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("failed row = %+v, want the transport and status code the host recorded", broken)
	}
	if broken.Error == "" {
		t.Error("the failed row lost the reason it failed")
	}

	ghost, ok := rows["ghost"]
	if !ok {
		t.Fatalf("the in-flight server is missing: %+v", got.Servers)
	}
	if ghost.State != "connecting" {
		t.Errorf("connecting row = %+v, want a spawn still in flight", ghost)
	}

	atlas, ok := rows["atlas"]
	if !ok {
		t.Fatalf("the project-declared server is missing: %+v", got.Servers)
	}
	// Nobody has answered for a repository-declared server, so it is off — but
	// not because the user turned it off, and a row that cannot say so reads as
	// a project's MCP that vanished.
	if atlas.State != "pending" {
		t.Errorf("project-declared row = %+v, want pending", atlas)
	}
	if atlas.Enabled {
		t.Error("an unanswered repository-declared server reported itself as on")
	}
	if !atlas.Remembered || atlas.Stale {
		t.Errorf("remembered = %v, stale = %v, want the cached answer reused as-is", atlas.Remembered, atlas.Stale)
	}
	if atlas.Description != "Maps a repository's symbols." || atlas.Tools != 1 {
		t.Errorf("cached row = %+v, want the last handshake's description and tools", atlas)
	}
}

// newMCPHTTPServer is a minimal Streamable HTTP MCP server whose tools/list
// answer the caller supplies. It is the same shape internal/ext/plugin's tests
// use, kept here so the endpoint test does not have to reach into that package's
// internals to get a server that connects.
func newMCPHTTPServer(t *testing.T, tools func() any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req.ID == nil { // notification
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "live", "version": "0"},
				"instructions":    "A live MCP server.",
			}
		case "tools/list":
			result = tools()
		case "prompts/list":
			result = map[string]any{"prompts": []any{}}
		case "resources/list":
			result = map[string]any{"resources": []any{}}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
}
