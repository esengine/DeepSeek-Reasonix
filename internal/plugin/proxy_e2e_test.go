package plugin_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/capability"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

// mockSpec mirrors helperSpec (lazy_test.go): the test binary re-runs itself
// as a stdio MCP server via TestHelperProcess.
func mockSpec() plugin.Spec {
	return plugin.Spec{
		Name:       "mock",
		Command:    os.Args[0],
		Args:       []string{"-test.run=TestHelperProcess", "--"},
		Env:        map[string]string{"GO_WANT_HELPER_PROCESS": "1"},
		Authorized: true, // install/boot sets this; proxy never invents trust
	}
}

// TestUseCapabilityFirstDiscoveryConnectListCall is the reviewer-demanded end
// to end: no schema cache, action=call on the server id resolves to a gated
// connect target (no process before approval), Execute connects and lists the
// tools, and a follow-up mcp-tool call executes against the live server.
func TestUseCapabilityFirstDiscoveryConnectListCall(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	lifeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ledger := capability.NewLedger()
	audit := &capability.Audit{}
	proxy := agent.NewUseCapabilityTool(lifeCtx, host, []plugin.Spec{mockSpec()}, tool.NewRegistry(), ledger, audit, nil)

	resolved, err := proxy.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-server:mock"}`))
	if err != nil {
		t.Fatalf("resolve server call: %v", err)
	}
	if resolved.SkipExecute || resolved.Target == nil {
		t.Fatalf("expected a deferred connect target, got %+v", resolved)
	}
	if resolved.TargetName != plugin.MCPConnectPermissionName("mock") {
		t.Fatalf("connect permission name = %q, want %q", resolved.TargetName, plugin.MCPConnectPermissionName("mock"))
	}
	if resolved.ReadOnly {
		t.Fatal("connect must not claim read-only: it spawns a subprocess")
	}
	if host.HasClient("mock") {
		t.Fatal("resolution must not start the server")
	}

	listing, err := resolved.Target.Execute(context.Background(), resolved.Args)
	if err != nil {
		t.Fatalf("connect execute: %v", err)
	}
	if !strings.Contains(listing, "mcp-tool:mock/echo") {
		t.Fatalf("listing missing echo tool id:\n%s", listing)
	}
	if !host.HasClient("mock") {
		t.Fatal("server should be connected after approved execute")
	}
	if got := proxy.ConnectedProxyTools(); len(got["mock"]) == 0 {
		t.Fatalf("proxy snapshot empty after connect: %v", got)
	}

	// A repeated server-level call now lists side-effect-free.
	again, err := proxy.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-server:mock"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !again.SkipExecute || !strings.Contains(again.Result, "mcp-tool:mock/echo") {
		t.Fatalf("connected server call should list immediately, got %+v", again)
	}
	if _, err := proxy.Execute(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-server:mock"}`)); err != nil {
		t.Fatalf("connected server call execute: %v", err)
	}
	if entry, ok := ledger.Get("mcp-server:mock"); !ok || entry.Outcome != capability.OutcomeSucceeded {
		t.Fatalf("connected server call ledger = %+v, found=%v", entry, ok)
	}
	if snap := audit.Snapshot(); snap.MCPCall != 1 || snap.MCPCallFailures != 0 {
		t.Fatalf("connected server call audit = %d/%d, want 1/0", snap.MCPCall, snap.MCPCallFailures)
	}

	// Then the discovered tool is callable end to end.
	call, err := proxy.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:mock/echo","arguments":{"msg":"hi"}}`))
	if err != nil {
		t.Fatal(err)
	}
	callCtx, cancelCall := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCall()
	out, err := call.Target.Execute(callCtx, call.Args)
	if err != nil {
		t.Fatalf("tool execute: %v", err)
	}
	if out != "echo: hi" {
		t.Fatalf("echo result = %q", out)
	}
}

// TestUseCapabilitySharedHostSnapshot covers the cross-tab gap: a server
// connected by another controller on the shared host must still populate this
// proxy's snapshot (and thus the catalog) through resolve and inspect.
func TestUseCapabilitySharedHostSnapshot(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()
	lifeCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callCtx, cancelCall := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCall()
	// "Tab A" connects directly (not through any proxy).
	if _, err := host.AddWithLifecycle(lifeCtx, callCtx, mockSpec()); err != nil {
		t.Fatalf("tab A connect: %v", err)
	}

	// "Tab B": empty registry (auto_start=false semantics), fresh proxy.
	proxyB := agent.NewUseCapabilityTool(lifeCtx, host, []plugin.Spec{mockSpec()}, tool.NewRegistry(), capability.NewLedger(), nil, nil)
	resolved, err := proxyB.ResolveCall(context.Background(), json.RawMessage(`{"action":"call","capability_id":"mcp-tool:mock/echo","arguments":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target == nil {
		t.Fatalf("expected live target via shared host, got %+v", resolved)
	}
	if got := proxyB.ConnectedProxyTools(); len(got["mock"]) == 0 {
		t.Fatalf("resolve on a shared-host server must build the snapshot, got %v", got)
	}

	// A third proxy sees it via inspect too.
	proxyC := agent.NewUseCapabilityTool(lifeCtx, host, []plugin.Spec{mockSpec()}, tool.NewRegistry(), capability.NewLedger(), nil, func() capability.Catalog {
		return capability.Catalog{Entries: []capability.Entry{{
			ID: "mcp-server:mock", Kind: capability.KindMCPServer, Name: "mock", Source: "mock", Status: capability.StatusReady,
		}}}
	})
	if _, err := proxyC.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:mock"}`)); err != nil {
		t.Fatal(err)
	}
	if got := proxyC.ConnectedProxyTools(); len(got["mock"]) == 0 {
		t.Fatalf("inspect on a shared-host server must build the snapshot, got %v", got)
	}
}
