package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestPartitionToolCallsParallelisesCapabilityDiscovery(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, nil))
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	calls := []provider.ToolCall{
		{ID: "1", Name: "use_capability", Arguments: `{"action":"search","query":"github"}`},
		{ID: "2", Name: "use_capability", Arguments: `{"action":"inspect","capability_id":"mcp-server:github"}`},
		{ID: "3", Name: "read_file", Arguments: `{"path":"a.go"}`},
	}
	got := partitionToolCalls(reg, calls)
	if len(got) != 1 || !got[0].parallel || got[0].end != 3 {
		t.Fatalf("partition = %+v, want one parallel batch of 3", got)
	}
}

func TestPartitionToolCallsKeepsUnknownCapabilitySerial(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, nil))
	calls := []provider.ToolCall{
		{ID: "1", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:unknown/write","arguments":{"x":1}}`},
	}
	got := partitionToolCalls(reg, calls)
	if len(got) != 1 || got[0].parallel {
		t.Fatalf("unknown MCP call must stay serial: %+v", got)
	}
}

func TestCapabilityWrappersForwardPureBatchClassification(t *testing.T) {
	inner := NewUseCapabilityTool(t.Context(), nil, nil, nil, nil, nil, nil)
	pathBound := pathBoundCapabilityProxy{inner: inner, resolver: inner}
	restricted := &restrictedCapabilityProxy{
		Tool: inner, resolver: inner,
		allowed: map[string]bool{"mcp-server:github": true}, servers: map[string]bool{"github": true},
	}
	for name, wrapped := range map[string]tool.Tool{"path-bound": pathBound, "restricted": restricted} {
		t.Run(name, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(wrapped)
			got := partitionToolCalls(reg, []provider.ToolCall{{
				ID: "1", Name: "use_capability", Arguments: `{"action":"inspect","capability_id":"mcp-server:github"}`,
			}})
			if len(got) != 1 || !got[0].parallel {
				t.Fatalf("wrapped inspect must remain parallel: %+v", got)
			}
			serial := partitionToolCalls(reg, []provider.ToolCall{{
				ID: "2", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:github/write","arguments":{}}`,
			}})
			if len(serial) != 1 || serial[0].parallel {
				t.Fatalf("unknown writer must remain serial: %+v", serial)
			}
		})
	}
}

func TestClassifyCallSearchIsReadOnlyParallel(t *testing.T) {
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, nil, nil, nil, nil)
	class := proxy.ClassifyCall(json.RawMessage(`{"action":"search","query":"x"}`))
	if !class.Known || !class.ReadOnly || !class.ParallelSafe {
		t.Fatalf("search class = %+v", class)
	}
}

func TestPartitionIndependentReadOnlyMCPCallsAreParallel(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	var calls atomic.Int32
	alpha := readonlyMCPServer(t, "alpha", &calls)
	beta := readonlyMCPServer(t, "beta", &calls)
	defer alpha.Close()
	defer beta.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{
		{Name: "alpha", Type: "http", URL: alpha.URL, Authorized: true},
		{Name: "beta", Type: "http", URL: beta.URL, Authorized: true},
	}
	if _, err := host.Add(ctx, specs[0]); err != nil {
		t.Fatalf("connect alpha: %v", err)
	}
	if _, err := host.Add(ctx, specs[1]); err != nil {
		t.Fatalf("connect beta: %v", err)
	}
	runtime := NewMCPCapabilityRuntime(ctx, host, specs, tool.NewRegistry(), nil)
	runtime.ConfigureServers([]config.PluginEntry{{Name: "alpha"}, {Name: "beta"}}, specs, map[string]bool{"alpha": true, "beta": true})
	proxy := runtime.NewFrontend(capability.NewLedger(), nil)
	reg := tool.NewRegistry()
	reg.Add(proxy)
	got := partitionToolCalls(reg, []provider.ToolCall{
		{ID: "1", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:alpha/search","arguments":{}}`},
		{ID: "2", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:beta/search","arguments":{}}`},
	})
	if len(got) != 1 || !got[0].parallel || got[0].end != 2 {
		t.Fatalf("independent read-only MCP partition = %+v, want one parallel batch", got)
	}

	serial := partitionToolCalls(reg, []provider.ToolCall{
		{ID: "w", Name: "use_capability", Arguments: `{"action":"call","capability_id":"mcp-tool:unknown/write","arguments":{}}`},
	})
	if len(serial) != 1 || serial[0].parallel {
		t.Fatalf("unknown/write MCP must stay serial: %+v", serial)
	}
}

func TestPartitionStatefulBrowserMCPStaysSerial(t *testing.T) {
	proxy := NewUseCapabilityTool(t.Context(), nil, nil, nil, nil, nil, nil)
	proxy.runtime = &MCPCapabilityRuntime{servers: map[string]mcpRuntimeServer{
		"chrome-devtools": {entry: config.PluginEntry{Name: "chrome-devtools"}},
	}}
	class := proxy.ClassifyCall(json.RawMessage(`{"action":"call","capability_id":"mcp-tool:chrome-devtools/navigate","arguments":{}}`))
	if class.ParallelSafe {
		t.Fatalf("stateful browser MCP must not be parallel-safe: %+v", class)
	}
}

func TestOnDemandConnectEmitsOneSessionRemoteToolsListObservation(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	var toolCalls atomic.Int32
	server := readonlyMCPServer(t, "observed", &toolCalls)
	defer server.Close()
	host := plugin.NewHost()
	defer host.Close()
	spec := plugin.Spec{Name: "observed", Type: "http", URL: server.URL, Authorized: true}
	proxy := NewUseCapabilityTool(t.Context(), host, []plugin.Spec{spec}, tool.NewRegistry(), nil, nil, nil)
	var observations []mcpListObservation
	proxy.bindMCPListObserver(func(observation mcpListObservation) { observations = append(observations, observation) })
	if _, err := proxy.ensureServerToolsForSpec(t.Context(), spec.Name, spec); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if len(observations) != 1 || observations[0].Source != "remote" || observations[0].Trigger != "connect" || !observations[0].NetworkCall || observations[0].ToolCount != 1 {
		t.Fatalf("observations = %+v", observations)
	}
	if _, err := proxy.ensureServerToolsForSpec(t.Context(), spec.Name, spec); err != nil {
		t.Fatalf("shared-host reuse: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("shared-host reuse emitted a remote list: %+v", observations)
	}
}

func TestListChangedIsAttributedOnlyToActiveRuntimeFrontend(t *testing.T) {
	runtime := NewMCPCapabilityRuntime(t.Context(), nil, nil, tool.NewRegistry(), nil)
	audit := &capability.Audit{}
	frontend := runtime.NewFrontend(nil, audit)
	var observations []mcpListObservation
	frontend.bindMCPListObserver(func(observation mcpListObservation) { observations = append(observations, observation) })
	release := frontend.activateMCPListObserver()
	runtime.notifyToolListChanged("svc", []tool.Tool{fakeTool{name: "mcp__svc__read", readOnly: true}})
	if len(observations) != 1 || observations[0].Trigger != "list_changed" || !observations[0].NetworkCall {
		t.Fatalf("observations = %+v", observations)
	}
	snapshot := audit.Snapshot()
	if snapshot.MCPLists.Remote != 1 || snapshot.MCPLists.Triggers["list_changed"] != 1 {
		t.Fatalf("MCP list audit = %+v", snapshot.MCPLists)
	}
	release()
	runtime.notifyToolListChanged("svc", []tool.Tool{fakeTool{name: "mcp__svc__read", readOnly: true}})
	if len(observations) != 1 || audit.Snapshot().MCPLists.Remote != 1 {
		t.Fatalf("inactive frontend retained list_changed attribution: observations=%+v audit=%+v", observations, audit.Snapshot().MCPLists)
	}
}

func readonlyMCPServer(t *testing.T, name string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
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
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": name, "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name": "search", "description": "search",
				"inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"readOnlyHint": true},
			}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
}
