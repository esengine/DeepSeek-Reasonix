package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/capability"
	"reasonix/internal/config"
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
	providerSchemasBefore, err := json.Marshal(registry.Schemas())
	if err != nil {
		t.Fatalf("marshal provider schemas before refresh: %v", err)
	}

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
	providerSchemasAfter, err := json.Marshal(registry.Schemas())
	if err != nil {
		t.Fatalf("marshal provider schemas after refresh: %v", err)
	}
	if !bytes.Equal(providerSchemasAfter, providerSchemasBefore) {
		t.Fatalf("provider-visible schema bytes changed after dynamic MCP refresh: before=%s after=%s", providerSchemasBefore, providerSchemasAfter)
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

func TestMCPCapabilityRuntimeReplaysCatalogChangedBeforeSubscription(t *testing.T) {
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
	initial, err := host.Add(ctx, spec)
	if err != nil {
		t.Fatalf("Host.Add: %v", err)
	}
	for _, candidate := range initial {
		registry.Add(candidate)
	}

	changed := make(chan []tool.Tool, 1)
	unsubscribe := host.SubscribeToolListChanges(ctx, func(changedSpec plugin.Spec, tools []tool.Tool) {
		if plugin.MCPRuntimeSpecMatches(changedSpec, spec) {
			changed <- tools
		}
	})
	loader := initial[0]
	if _, err := loader.Execute(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("load_toolset before runtime subscription: %v", err)
	}
	select {
	case refreshed := <-changed:
		if findMCPTool(refreshed, "list_schematic_components", "") == nil {
			t.Fatalf("refreshed tools missing dynamic tool: %v", refreshed)
		}
	case <-ctx.Done():
		t.Fatalf("wait for pre-subscription refresh: %v", ctx.Err())
	}
	unsubscribe()

	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, registry, nil)
	frontend := runtime.NewFrontend(capability.NewLedger(), nil)
	registry.Add(frontend)
	registry.SetProviderVisibleTools([]string{"use_capability"})

	wantName := "mcp__dynamic__list_schematic_components"
	if _, ok := registry.Get(wantName); !ok {
		t.Fatal("late runtime subscription did not replay the current dynamic tool catalog")
	}
	currentLoader, ok := registry.Get("mcp__dynamic__load_toolset")
	if !ok || currentLoader == loader {
		t.Fatal("late runtime subscription retained the stale pre-refresh adapter")
	}
	if live := runtime.ConnectedProxyTools()["dynamic"]; len(live) != 2 || !hasCachedTool(live, "list_schematic_components") {
		t.Fatalf("replayed capability tools = %+v, want the complete current catalog", live)
	}
	if _, err := frontend.Execute(ctx, json.RawMessage(`{"action":"call","capability_id":"mcp-tool:dynamic/list_schematic_components","arguments":{}}`)); err != nil {
		t.Fatalf("dynamic tool call after replay: %v", err)
	}
	if got := dynamicCalls.Load(); got != 1 {
		t.Fatalf("dynamic tools/call count = %d, want 1", got)
	}
}

func TestConfiguredDisabledSessionDropsSharedHostReplayAndGenericAlias(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var toolCalls atomic.Int32
	server := explicitReaderMCPServer(t, nil, &toolCalls)
	defer server.Close()
	spec := plugin.Spec{Name: "shared-disabled", Type: "http", URL: server.URL, Authorized: true}
	host := plugin.NewHost()
	defer host.Close()
	initial, err := host.Add(ctx, spec)
	if err != nil {
		t.Fatalf("Host.Add: %v", err)
	}

	registry := tool.NewRegistry()
	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, registry, nil)
	modelName := plugin.ModelToolName(spec.Name, "search")
	if _, ok := registry.Get(modelName); !ok {
		t.Fatal("test requires constructor replay to register the shared Host tool")
	}
	frontend := runtime.NewFrontend(capability.NewLedger(), nil)
	generic := json.RawMessage(fmt.Sprintf(`{"action":"call","capability_id":"tool:%s","arguments":{}}`, modelName))
	resolvedGeneric, err := frontend.ResolveCall(ctx, generic)
	if err != nil || resolvedGeneric.Target == nil {
		t.Fatalf("resolve generic replayed adapter = %+v, %v", resolvedGeneric, err)
	}
	runtime.ConfigureServers(
		[]config.PluginEntry{{Name: spec.Name, Type: spec.Type, URL: spec.URL, Source: config.MCPSourceUserConfig}},
		[]plugin.Spec{spec},
		map[string]bool{spec.Name: false},
	)
	if _, ok := registry.Get(modelName); ok {
		t.Fatal("disabled session retained the replayed shared Host adapter")
	}
	if _, err := resolvedGeneric.Target.Execute(ctx, resolvedGeneric.Args); err == nil || !strings.Contains(strings.ToLower(err.Error()), "disabled") {
		t.Fatalf("resolved generic adapter after disable error = %v", err)
	}
	canonical := json.RawMessage(`{"action":"call","capability_id":"mcp-tool:shared-disabled/search","arguments":{}}`)
	out, err := frontend.Execute(ctx, canonical)
	if detail := strings.ToLower(out + " " + fmt.Sprint(err)); !strings.Contains(detail, "disabled") {
		t.Fatalf("canonical disabled call = %q, %v, want disabled refusal", out, err)
	}

	// Even if another registry owner retains an adapter, generic tool: routing
	// must re-check this runtime's current authorization boundary.
	staleRegistry := tool.NewRegistry()
	staleRegistry.Add(initial[0])
	staleFrontend := runtime.NewFrontend(capability.NewLedger(), nil)
	staleFrontend.registry = staleRegistry
	out, err = staleFrontend.Execute(ctx, generic)
	if detail := strings.ToLower(out + " " + fmt.Sprint(err)); !strings.Contains(detail, "disabled") {
		t.Fatalf("generic disabled call = %q, %v, want disabled refusal", out, err)
	}
	if got := toolCalls.Load(); got != 0 {
		t.Fatalf("disabled session executed tools/call %d times", got)
	}
}

func TestMCPResolveReleasesDispatchLockBeforeCatalogCallback(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	catalogEntered := make(chan struct{})
	releaseCatalog := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCatalog) }) }
	t.Cleanup(release)
	spec := plugin.Spec{Name: "catalog-lock", Type: "http", URL: "http://127.0.0.1:1", Authorized: true}
	host := plugin.NewHost()
	defer host.Close()
	runtime := NewMCPCapabilityRuntime(ctx, host, []plugin.Spec{spec}, tool.NewRegistry(), func() capability.Catalog {
		close(catalogEntered)
		<-releaseCatalog
		return capability.Catalog{}
	})
	frontend := runtime.NewFrontend(capability.NewLedger(), nil)

	resolveDone := make(chan struct {
		call tool.ResolvedCall
		err  error
	}, 1)
	go func() {
		resolved, err := frontend.ResolveCall(ctx, json.RawMessage(`{"action":"call","capability_id":"mcp-tool:catalog-lock/search","arguments":{}}`))
		resolveDone <- struct {
			call tool.ResolvedCall
			err  error
		}{call: resolved, err: err}
	}()
	select {
	case <-catalogEntered:
	case <-ctx.Done():
		t.Fatalf("catalog callback was not entered: %v", ctx.Err())
	}

	disableDone := make(chan bool, 1)
	go func() { disableDone <- runtime.SetServerEnabled(spec.Name, false) }()
	select {
	case ok := <-disableDone:
		if !ok {
			t.Fatal("disable did not find the configured server")
		}
	case <-time.After(time.Second):
		release()
		<-disableDone
		t.Fatal("runtime writer blocked behind catalog callback; resolve retained dispatchMu across catalog lookup")
	}
	release()
	resolved := <-resolveDone
	if resolved.err != nil || resolved.call.Target == nil {
		t.Fatalf("resolve = %+v, %v", resolved.call, resolved.err)
	}
	if _, err := resolved.call.Target.Execute(ctx, resolved.call.Args); err == nil || !strings.Contains(strings.ToLower(err.Error()), "disabled") {
		t.Fatalf("resolved target after concurrent disable error = %v", err)
	}
}

func hasCachedTool(tools []plugin.CachedTool, name string) bool {
	return slices.ContainsFunc(tools, func(candidate plugin.CachedTool) bool {
		return candidate.Name == name
	})
}
