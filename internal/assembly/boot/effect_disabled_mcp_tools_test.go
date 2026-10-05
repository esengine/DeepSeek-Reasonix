package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
)

func disabledToolsMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "filtered", "version": "1"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{
				{"name": "read", "description": "Read fixture data.", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}},
				{"name": "write", "description": "WRITE_SCHEMA_MARKER", "inputSchema": map[string]any{"type": "object"}},
			}}
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
}

func TestEffectDisabledMCPToolsStayOutOfTheProviderAndCatalogAfterRestart(t *testing.T) {
	var rec *capabilityCallProvider
	provider.Register("boot-disabled-mcp", func(provider.Config) (provider.Provider, error) { return rec, nil })
	for _, load := range []string{"always", "deferred"} {
		t.Run(load, func(t *testing.T) {
			home := isolateConfigHome(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			t.Setenv("REASONIX_HOME", reasonixHome)
			workspace := robustTempDir(t)
			t.Chdir(workspace)
			server := disabledToolsMCPServer(t)
			defer server.Close()
			writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-disabled-mcp"
model = "x"
[[plugins]]
name = "filtered"
type = "http"
url = %q
load = %q
disabled_tools = ["write"]
`, server.URL, load))
			approveWorkspace(t, workspace)
			approveProjectServer(t, workspace, "filtered")
			for _, phase := range []string{"fresh", "cached restart"} {
				rec = &capabilityCallProvider{call: `{"action":"inspect","capability_id":"mcp-server:filtered"}`}
				ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(ctrl.Close)
				configured := ctrl.ConfiguredMCPServers()
				if len(configured) != 1 || configured[0].Entry.Name != "filtered" {
					t.Fatalf("%s: configured MCP servers = %+v, want filtered", phase, configured)
				}
				if _, err := ctrl.ConnectMCPServer(configured[0].Entry); err != nil {
					t.Fatal(err)
				}
				tools, err := ctrl.Host().ToolsFor(t.Context(), "filtered")
				if err != nil {
					t.Fatal(err)
				}
				if len(tools) != 1 || tools[0].Name() != "mcp__filtered__read" {
					t.Fatalf("%s: live tools = %v; want only read", phase, tools)
				}
				if got := ctrl.MCPCatalogTools()["filtered"]; got != 1 {
					t.Fatalf("%s: catalog tools=%d, want 1", phase, got)
				}
				if err := ctrl.Run(t.Context(), "describe available tools"); err != nil {
					t.Fatal(err)
				}
				requests := rec.requests()
				if len(requests) == 0 {
					t.Fatal("no provider request")
				}
				for _, req := range requests {
					body, err := json.Marshal(req)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(body), "mcp__filtered__write") || strings.Contains(string(body), "WRITE_SCHEMA_MARKER") {
						t.Fatalf("%s: disabled tool reached provider", phase)
					}
					if phase == "cached restart" && load == "always" && !slices.Contains(toolSchemaNames(req.Tools), "mcp__filtered__read") {
						t.Fatalf("%s: allowed tool missing from always-loaded provider schema", phase)
					}
				}
				results := effectToolResults(requests[len(requests)-1])
				if len(results) != 1 || !strings.Contains(results[0], "Read fixture data.") {
					t.Fatalf("%s: inspection did not return the allowed tool: %q", phase, results)
				}
				ctrl.Close()
			}
		})
	}
}

func TestDisabledMCPToolsAreMarkedBeforeRegistration(t *testing.T) {
	reg := tool.NewRegistry()
	plugin.ApplyDisabledMCPPolicy(reg, plugin.Spec{Name: "filtered", DisabledTools: []string{"write"}})
	if !reg.DisabledMCP("mcp__filtered__write") || reg.DisabledMCP("write") {
		t.Fatal("disabled MCP aliases were not marked")
	}
}

const disabledRefusalProviderKind = "boot-disabled-refusal"

type disabledRefusalProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *disabledRefusalProvider) Name() string { return disabledRefusalProviderKind }

func (p *disabledRefusalProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	call := len(p.reqs)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 2)
	switch call {
	case 0:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "parent-disabled", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:filtered/write","arguments":{}}`}}
	case 1:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "delegate-disabled", Name: "read_only_task", Arguments: `{"prompt":"try the disabled tool"}`}}
	case 2:
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "child-disabled", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:filtered/write","arguments":{}}`}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *disabledRefusalProvider) requests() []provider.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.reqs)
}

var (
	disabledRefusalRegister sync.Once
	disabledRefusalMu       sync.Mutex
	disabledRefusalCurrent  *disabledRefusalProvider
)

func useDisabledRefusalProvider(t *testing.T, p *disabledRefusalProvider) {
	t.Helper()
	disabledRefusalRegister.Do(func() {
		provider.Register(disabledRefusalProviderKind, func(provider.Config) (provider.Provider, error) {
			disabledRefusalMu.Lock()
			defer disabledRefusalMu.Unlock()
			if disabledRefusalCurrent == nil {
				return nil, fmt.Errorf("disabled refusal provider is not installed")
			}
			return disabledRefusalCurrent, nil
		})
	})
	disabledRefusalMu.Lock()
	disabledRefusalCurrent = p
	disabledRefusalMu.Unlock()
	t.Cleanup(func() {
		disabledRefusalMu.Lock()
		disabledRefusalCurrent = nil
		disabledRefusalMu.Unlock()
	})
}

func TestEffectDisabledMCPRefusalReachesReadOnlyChildren(t *testing.T) {
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	server := disabledToolsMCPServer(t)
	t.Cleanup(server.Close)
	writeFile(t, workspace, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[agent]
system_prompt = "BASE"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = %q
model = "x"
[[plugins]]
name = "filtered"
type = "http"
url = %q
load = "deferred"
disabled_tools = ["write"]
`, disabledRefusalProviderKind, server.URL))
	approveWorkspace(t, workspace)
	approveProjectServer(t, workspace, "filtered")

	rec := &disabledRefusalProvider{}
	useDisabledRefusalProvider(t, rec)
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, Home: reasonixHome, WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	if err := ctrl.Run(t.Context(), "try the disabled MCP tool directly and through a read-only child"); err != nil {
		t.Fatal(err)
	}

	results := toolResults(rec.requests())
	for _, id := range []string{"parent-disabled", "child-disabled"} {
		got := results[id]
		if !strings.Contains(got, tool.CodeMCPToolDisabled) || strings.Contains(got, "unresolved dynamic") {
			t.Fatalf("%s result = %q, want typed %s refusal", id, got, tool.CodeMCPToolDisabled)
		}
	}
}
