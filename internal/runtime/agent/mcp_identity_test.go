package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
)

type replacingIdentityTool struct {
	annotatedMCPTool
	registry *tool.Registry
}

func (t replacingIdentityTool) Execute(context.Context, json.RawMessage) (string, error) {
	t.registry.Add(annotatedMCPTool{fakeTool: t.fakeTool, server: "replacement", raw: "other"})
	return "done", nil
}

func TestMCPInvocationKeepsIdentityAcrossRegistryReplacement(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(replacingIdentityTool{
		annotatedMCPTool: annotatedMCPTool{fakeTool: fakeTool{name: "mcp__server__read", readOnly: true}, server: "server", raw: "original", serverAuthorized: true},
		registry:         reg,
	})
	ledger := capability.NewLedger()
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{CapabilityLedger: ledger}, event.Discard)
	a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "call", Name: "mcp__server__read", Arguments: `{}`})
	if entry, ok := ledger.Get("mcp-tool:server/original"); !ok || entry.Outcome != capability.OutcomeSucceeded || len(ledger.Snapshot()) != 1 {
		t.Fatalf("execution recorded a different adapter: %+v", ledger.Snapshot())
	}
}

func TestDirectMCPInvocationUsesExecutedIdentity(t *testing.T) {
	for _, tc := range []struct{ server, raw, strip string }{
		{"github", "search", ""},
		{"my.server", "search.issues", ""},
		{"my__server", strings.Repeat("long_tool_", 20), ""},
		{"github", "vendor_search", "vendor_"},
		{"github", "review", ""},
	} {
		t.Run(tc.server+"/"+tc.raw, func(t *testing.T) {
			t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
			var calls atomic.Int32
			var fail atomic.Bool
			srv := identityMCPServer(t, tc.raw, &calls, &fail)
			defer srv.Close()
			host := plugin.NewHost()
			defer host.Close()
			adapters, err := host.EnsureConnected(t.Context(), plugin.Spec{Name: tc.server, Type: "http", URL: srv.URL, Authorized: true, StripRawPrefix: tc.strip})
			if err != nil || len(adapters) != 1 {
				t.Fatalf("connect: tools=%d err=%v", len(adapters), err)
			}
			reg := tool.NewRegistry()
			reg.Add(adapters[0])
			id := "mcp-tool:" + tc.server + "/" + tc.raw
			cat := capability.BuildCatalog(capability.CatalogOptions{Tools: reg.AllContractEntries()})
			if entry, ok := cat.Lookup(id); !ok || entry.ToolName != adapters[0].Name() {
				t.Fatalf("live adapter catalog=%+v", cat.Entries)
			}
			for _, call := range []struct {
				name   string
				failed bool
			}{{adapters[0].Name(), false}, {id, false}, {tc.raw, false}, {adapters[0].Name(), true}} {
				fail.Store(call.failed)
				ledger := capability.NewLedger()
				a := New(nil, reg, sessionstore.NewSession("sys"), Options{CapabilityLedger: ledger}, event.Discard)
				a.SeedCapabilityRoute(capability.RouteDecision{Candidates: []capability.RouteCandidate{{Entry: capability.Entry{ID: id}, Policy: capability.AutoUseRequire}}})
				out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "call", Name: call.name, Arguments: `{}`})
				want := capability.OutcomeSucceeded
				if call.failed {
					want = capability.OutcomeFailed
				}
				if entry, ok := ledger.Get(id); !ok || entry.Outcome != want || len(ledger.Snapshot()) != 1 {
					t.Errorf("executed %q: ledger=%+v outcome=%+v", adapters[0].Name(), ledger.Snapshot(), out)
				}
			}
			ledger := capability.NewLedger()
			gate := permission.NewGate(permission.New("ask", nil, nil, []string{adapters[0].Name()}), nil)
			a := New(nil, reg, sessionstore.NewSession("sys"), Options{CapabilityLedger: ledger, Gate: gate}, event.Discard)
			out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "denied", Name: id, Arguments: `{}`})
			if !out.blocked || len(ledger.Snapshot()) != 0 || out.refusalCode != permission.RefusalDenyRule {
				t.Fatalf("permission gate lost callable identity: outcome=%+v ledger=%+v", out, ledger.Snapshot())
			}
			if calls.Load() != 4 {
				t.Fatalf("tools/call count=%d, want 4", calls.Load())
			}
		})
	}
}

func identityMCPServer(t *testing.T, raw string, calls *atomic.Int32, fail *atomic.Bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]string{"name": "identity", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": raw, "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]bool{"readOnlyHint": true}}}}
		case "tools/call":
			calls.Add(1)
			if req.Params.Name != raw {
				t.Errorf("tools/call name=%q, want %q", req.Params.Name, raw)
			}
			result = map[string]any{"isError": fail.Load(), "content": []map[string]string{{"type": "text", "text": "result"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result})
	}))
}

func TestMetadataLessMCPNameDoesNotCreateLedgerIdentity(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "mcp__github__search", readOnly: true})
	ledger := capability.NewLedger()
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{CapabilityLedger: ledger}, event.Discard)
	out := a.executeOne(t.Context(), &a.turn, provider.ToolCall{ID: "call", Name: "mcp__github__search", Arguments: `{}`})
	if out.blocked || out.errMsg != "" || len(ledger.Snapshot()) != 0 {
		t.Fatalf("metadata-less execution=%+v ledger=%+v", out, ledger.Snapshot())
	}
}
