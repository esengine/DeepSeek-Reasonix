package boot

// What the model can reach after a server rewrites its catalog is a property of
// the whole assembly: the client that hears the notice, the registry it
// re-registers into, and the request built from it.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/session/control"
)

// helperListChangedResult answers the stdio helper's MCP methods. Before the
// first call it offers alpha and beta; after it, beta is gone and gamma and
// delta have arrived.
func helperListChangedResult(method string, mutated bool) any {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]any{"name": "live", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}},
		}
	case "tools/list":
		return map[string]any{"tools": listChangedCatalog(mutated)}
	case "tools/call":
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}
	}
	return map[string]any{}
}

func listChangedCatalog(mutated bool) []map[string]any {
	names := []string{"alpha", "beta"}
	if mutated {
		names = []string{"alpha", "gamma", "delta"}
	}
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]any{
			"name":        name,
			"description": "tool " + name,
			"inputSchema": map[string]any{"type": "object"},
		})
	}
	return out
}

// listChangedServerBlock is the [[plugins]] entry for one transport. The HTTP
// server announces in the SSE stream of the call it is answering, which is
// where a notification reaches a client that opens no standalone stream.
func listChangedServerBlock(t *testing.T, transport string) string {
	t.Helper()
	if transport == "stdio" {
		return fmt.Sprintf(`
[[plugins]]
name = "live"
type = "stdio"
command = %q
args = ["-test.run=TestHelperProcess", "--"]
load = "always"
[plugins.env]
GO_WANT_HELPER_PROCESS = "1"
GO_WANT_HELPER_LIST_CHANGED = "1"
`, os.Args[0])
	}
	var mutated atomic.Bool
	var announces atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := helperListChangedResult(req.Method, mutated.Load())
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
		if req.Method != "tools/call" || announces.Add(1) > 2 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		mutated.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
	}))
	t.Cleanup(srv.Close)
	return fmt.Sprintf(`
[[plugins]]
name = "live"
type = "http"
url = %q
load = "always"
`, srv.URL)
}

func listChangedConfig(kind, serverBlock string) string {
	return fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = %q
model = "x"
`, kind) + serverBlock
}

// listChangedProvider issues one scripted use_capability call per round and
// finishes the turn on any round the script leaves out. Rounds are counted
// across turns, so the script says which turn each call lands in.
type listChangedProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
	// round counts provider requests across turns, so a script says which turn
	// each call lands in.
	round int
	// calls are use_capability calls by round; direct are calls to a tool the
	// provider schema itself carries, which is the surface a pinned server has.
	calls  map[int]string
	direct map[int]string
}

func (p *listChangedProvider) Name() string { return "boot-mcp-list-changed" }

func (p *listChangedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	round := p.round
	p.round++
	call, direct := p.calls[round], p.direct[round]
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 2)
	switch {
	case call != "":
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("probe-%d", round), Name: "use_capability", Arguments: call,
		}}
	case direct != "":
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("probe-%d", round), Name: direct, Arguments: "{}",
		}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *listChangedProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.reqs)
}

func capabilityCall(id string) string {
	return fmt.Sprintf(`{"action":"call","capability_id":%q,"arguments":{}}`, id)
}

func TestEffectMCPListChangedReachesTheNextRequest(t *testing.T) {
	for _, transport := range []string{"stdio", "http"} {
		t.Run(transport, func(t *testing.T) {
			home := isolateConfigHome(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			t.Setenv("REASONIX_HOME", reasonixHome)
			workspace := robustTempDir(t)
			t.Chdir(workspace)

			kind := "boot-mcp-list-changed-" + transport
			rec := &listChangedProvider{calls: map[int]string{
				0: capabilityCall("mcp-tool:live/alpha"),
				2: capabilityCall("mcp-tool:live/alpha"),
				4: `{"action":"inspect","capability_id":"mcp-server:live"}`,
				5: capabilityCall("mcp-tool:live/gamma"),
				6: capabilityCall("mcp-tool:live/beta"),
			}}
			provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec, nil })
			writeFile(t, workspace, "reasonix.toml", listChangedConfig(kind, listChangedServerBlock(t, transport)))
			writeUserConfig(t, "[tools.mcp_load]\nlive = \"always\"\n")
			approveWorkspace(t, workspace)
			approveProjectServer(t, workspace, "live")

			ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			t.Cleanup(ctrl.Close)

			// The first turn calls alpha, which connects the server and is what
			// makes it announce the catalog it moved to.
			if err := ctrl.Run(t.Context(), "call the live tool"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := toolResults(rec.requests())["probe-0"]; !strings.Contains(got, "ok") {
				t.Fatalf("the first call returned %q, want the server's result", got)
			}
			// The refresh went through the judgement, so what the server added
			// is held until the user approves it rather than offered on the
			// server's word.
			acceptHeldTools(t, ctrl, "live", "delta", "gamma")

			// The approval reaches the model on the next listing, which the
			// next call's notice brings.
			if err := ctrl.Run(t.Context(), "call the live tool again"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			waitForRegisteredTool(t, ctrl, "mcp__live__gamma")

			// The third turn reads the catalog, calls what arrived, then what left.
			if err := ctrl.Run(t.Context(), "read the catalog and call both tools"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			requests := rec.requests()
			results := toolResults(requests)
			catalog := results["probe-4"]
			for _, want := range []string{"gamma", "delta"} {
				if !strings.Contains(catalog, want) {
					t.Fatalf("catalog after the notice = %q, missing %q", catalog, want)
				}
			}
			if strings.Contains(catalog, "beta") {
				t.Fatalf("catalog after the notice still offers the dropped tool: %q", catalog)
			}
			if got := results["probe-5"]; !strings.Contains(got, "ok") {
				t.Fatalf("call to the tool the server added returned %q, want its result", got)
			}
			if got := results["probe-6"]; !strings.Contains(got, `not found on server "live"`) {
				t.Fatalf("call to the dropped tool returned %q, want the typed not-found failure", got)
			}

			first, last := requests[0], requests[len(requests)-1]
			if before, after := systemMessage(first.Messages), systemMessage(last.Messages); before != after {
				t.Fatalf("the cache-stable prefix moved with the catalog:\n before: %.200q\n  after: %.200q", before, after)
			}
			if got := mcpToolNames(last); len(got) != 0 {
				t.Fatalf("a server discovered during the session pinned %v into the provider schema", got)
			}
		})
	}
}

// mcpToolNames is the live server's share of one request's tool surface.
func mcpToolNames(req provider.Request) []string {
	var out []string
	for _, name := range toolSchemaNames(req.Tools) {
		if strings.HasPrefix(name, "mcp__live__") {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// acceptHeldTools approves what the refresh withheld, as the trust command
// does. A definition the server adds mid-session is not one the user approved,
// so the judgement holds it out of the model; the approval reaches the model on
// the listing after it. Holding is what proves the refresh was judged at all.
func acceptHeldTools(t *testing.T, ctrl *control.Controller, server string, want ...string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		held, ok := ctrl.Host().HeldTools(server)
		if ok && len(held) == len(want) {
			names := make([]string, 0, len(held))
			for _, h := range held {
				names = append(names, h.RawName)
			}
			slices.Sort(names)
			if !slices.Equal(names, want) {
				t.Fatalf("the refresh held %v, want %v", names, want)
			}
			if _, err := ctrl.Host().AcceptHeldTools(server, plugin.HeldDigest(held)); err != nil {
				t.Fatalf("AcceptHeldTools: %v", err)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the refresh never held %v for approval: an unapproved definition reached the model", want)
}

// waitForRegisteredTool waits for the refresh the server announced to reach
// this session's registry, which is where the next request is built from.
func waitForRegisteredTool(t *testing.T, ctrl *control.Controller, name string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range ctrl.AllToolContractEntries() {
			if entry.Name == name {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never reached the registry after notifications/tools/list_changed", name)
}

// A pinned server's tools are in the provider-visible array from the session's
// first request, so one it drops would rewrite that array. It does not move:
// the schema stays, its call fails with the typed not-found, the model is told
// on the turn tail, and the next session never lists it. stdio only — each
// Build spawns a helper from the same catalog; both transports are covered above.
func TestEffectMCPListChangedKeepsAPinnedToolSurface(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)

	kind := "boot-mcp-list-changed-pinned"
	var rec atomic.Pointer[listChangedProvider]
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return rec.Load(), nil })
	writeFile(t, workspace, "reasonix.toml", listChangedConfig(kind, listChangedServerBlock(t, "stdio")))
	writeUserConfig(t, "[tools.mcp_load]\nlive = \"always\"\n")
	approveWorkspace(t, workspace)
	approveProjectServer(t, workspace, "live")

	// Session one only warms the schema cache: no tool is called, so the server
	// never announces and the catalog it cached is the one it started from.
	warm := &listChangedProvider{}
	rec.Store(warm)
	first, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	waitForRegisteredTool(t, first, "mcp__live__beta")
	first.Close()
	waitForCachedMCPTool(t, "live", "beta")

	// Session two is pinned: the server's tools are in the provider schema from
	// its first request, and it drops one mid-session.
	live := &listChangedProvider{direct: map[int]string{0: "mcp__live__alpha", 2: "mcp__live__alpha", 4: "mcp__live__beta"}}
	rec.Store(live)
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)

	if err := ctrl.Run(t.Context(), "call the pinned tool"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pinned := mcpToolNames(live.requests()[0])
	if !slices.Equal(pinned, []string{"mcp__live__alpha", "mcp__live__beta"}) {
		t.Fatalf("the first request carried %v, want the cached catalog pinned into the schema", pinned)
	}
	acceptHeldTools(t, ctrl, "live", "delta", "gamma")
	if err := ctrl.Run(t.Context(), "call the pinned tool again"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	waitForRegisteredTool(t, ctrl, "mcp__live__gamma")

	if err := ctrl.Run(t.Context(), "call the tool the server dropped"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requests := live.requests()
	last := requests[len(requests)-1]
	if moved := toolSurfaceDrift(requests[0], last); len(moved) != 0 {
		t.Fatalf("the provider-visible tools array moved with the catalog: %v", moved)
	}
	if got := toolResults(requests)["probe-4"]; !strings.Contains(got, `not found on server "live"`) {
		t.Fatalf("call to the withdrawn tool returned %q, want the typed not-found", got)
	}
	if !containsMessage(last, "withdrew mcp__live__beta") {
		t.Fatalf("the turn tail never told the model what the server withdrew:\n%s", strings.Join(messageTexts(last), "\n"))
	}
	if sys, after := systemMessage(requests[0].Messages), systemMessage(last.Messages); sys != after {
		t.Fatalf("the cache-stable prefix moved with the catalog:\n before: %.200q\n  after: %.200q", sys, after)
	}

	// Session three reads the refreshed cache: the withdrawn tool is gone from
	// the surface, and what the server added is pinned in its place.
	next := &listChangedProvider{}
	rec.Store(next)
	third, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(third.Close)
	if err := third.Run(t.Context(), "say ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := mcpToolNames(next.requests()[0])
	if !slices.Equal(got, []string{"mcp__live__alpha", "mcp__live__delta", "mcp__live__gamma"}) {
		t.Fatalf("the next session pinned %v, want the catalog the refresh cached", got)
	}
}

// toolSurfaceDrift names the entries two requests do not share. The whole array
// is compared — a changed description moves the prefix as a changed name does —
// but only what moved is worth printing.
func toolSurfaceDrift(before, after provider.Request) []string {
	entries := func(req provider.Request) map[string]string {
		out := make(map[string]string, len(req.Tools))
		for _, s := range req.Tools {
			out[s.Name] = s.Description + "|" + string(s.Parameters)
		}
		return out
	}
	was, is := entries(before), entries(after)
	var moved []string
	for name, body := range was {
		switch current, ok := is[name]; {
		case !ok:
			moved = append(moved, name+" (dropped)")
		case current != body:
			moved = append(moved, name+" (rewritten)")
		}
	}
	for name := range is {
		if _, ok := was[name]; !ok {
			moved = append(moved, name+" (added)")
		}
	}
	slices.Sort(moved)
	return moved
}

func messageTexts(req provider.Request) []string {
	out := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		out = append(out, string(m.Role)+": "+m.Content)
	}
	return out
}

func containsMessage(req provider.Request, want string) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, want) {
			return true
		}
	}
	return false
}

// waitForCachedMCPTool waits for the handshake's schema write, which the next
// session pins its provider surface from. The write is detached, so a session
// holding the tools may not have cached them yet.
func waitForCachedMCPTool(t *testing.T, server, tool string) {
	t.Helper()
	// Entries are per spec (<server>.<key>.json); any of the server's entries will do.
	pattern := filepath.Join(config.CacheDir(), "mcp", server+".*json")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := filepath.Glob(pattern)
		for _, path := range entries {
			if body, err := os.ReadFile(path); err == nil && strings.Contains(string(body), `"`+tool+`"`) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never reached the schema cache (%s)", tool, pattern)
}
