package usecap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/capability"
)

func TestMCPCapabilityRuntimeConcurrentUpdatesAndSnapshots(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", testenv.TempDir(t))
	runtime := NewMCPCapabilityRuntime(context.Background(), plugin.NewHost(), nil, tool.NewRegistry(), nil)
	defer runtime.host.Close()
	frontend := runtime.NewFrontend(nil, nil)
	entry := config.PluginEntry{Name: "race", Type: "http", Source: config.MCPSourceUserConfig}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			entry.URL = fmt.Sprintf("http://127.0.0.1:%d", 10000+i)
			runtime.UpsertServer(entry, plugin.Spec{Name: "race", Type: "http", URL: entry.URL, Authorized: true}, true)
			runtime.state.setLiveTools("race", []plugin.CachedTool{{Name: "query", ReadOnly: true}})
			runtime.SetServerEnabled("race", i%2 == 0)
			if i%10 == 0 {
				runtime.RemoveServer("race")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_, _ = frontend.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
			_, _, _, _, _ = runtime.CapabilityCatalogState()
		}
	}()
	wg.Wait()
}

func TestUseCapabilityResolveCallIsSideEffectFree(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	specs := []plugin.Spec{{
		Name:    "lazy",
		Type:    "stdio",
		Command: "reasonix-test-definitely-missing-binary",
	}}
	tl := NewUseCapabilityTool(context.Background(), host, specs, tool.NewRegistry(), capability.NewLedger(), nil, nil)

	resolved, err := tl.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:lazy/do_write","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SkipExecute || resolved.Target == nil {
		t.Fatalf("expected a deferred target, got %+v", resolved)
	}
	if resolved.ReadOnly {
		t.Fatal("unstarted tool without read-only metadata must resolve as a writer")
	}
	if host.HasClient("lazy") {
		t.Fatal("ResolveCall must not start the MCP server")
	}
	// Execution is where the connect finally happens — and fails for the
	// missing binary, marking the capability unavailable.
	ledger := capability.NewLedger()
	tl.ledger = ledger
	if _, err := resolved.Target.Execute(context.Background(), resolved.Args); err == nil {
		t.Fatal("expected connect failure for missing binary")
	}
	if e, ok := ledger.Get("mcp-tool:lazy/do_write"); !ok || e.Outcome != capability.OutcomeUnavailable {
		t.Fatalf("expected unavailable outcome, got %+v ok=%v", e, ok)
	}
}

func TestUseCapabilityReturnsTypedDisabledRefusal(t *testing.T) {
	reg := tool.NewRegistry()
	reg.ReplaceDisabledMCP("mock", []tool.MCPBinding{{
		Server:       "mock",
		RawName:      "write",
		VisibleName:  "write",
		CallableName: "mcp__mock__write",
		CapabilityID: "mcp-tool:mock/write",
	}})
	proxy := NewUseCapabilityTool(context.Background(), nil, nil, reg, nil, nil, nil)

	resolved, err := proxy.ResolveCall(context.Background(), json.RawMessage(
		`{"action":"call","capability_id":"mcp-tool:mock/write"}`,
	))
	if err != nil {
		t.Fatalf("ResolveCall: %v", err)
	}
	if !resolved.Unavailable || !resolved.SkipExecute {
		t.Fatalf("disabled capability should resolve as unavailable: %+v", resolved)
	}
	if resolved.RefusalCode != tool.CodeMCPToolDisabled {
		t.Fatalf("refusal code = %q, want %q", resolved.RefusalCode, tool.CodeMCPToolDisabled)
	}
	if !strings.Contains(resolved.Result, tool.CodeMCPToolDisabled) {
		t.Fatalf("result lacks typed refusal identity: %q", resolved.Result)
	}
}
