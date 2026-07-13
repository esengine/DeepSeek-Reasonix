package capability

import (
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func boolPtr(b bool) *bool { return &b }

func TestLoadCachedToolsForSpecsHonorsFingerprint(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	fresh := plugin.Spec{Name: "gh", Type: "stdio", Command: "gh-mcp"}
	if err := plugin.SaveCachedSchema("gh", plugin.CachedSchema{
		SpecHash: plugin.SpecFingerprint(fresh),
		Tools:    []plugin.CachedTool{{Name: "search_issues", Description: "search", ReadOnly: true}},
	}); err != nil {
		t.Fatal(err)
	}
	stale := plugin.Spec{Name: "old", Type: "stdio", Command: "old-mcp"}
	if err := plugin.SaveCachedSchema("old", plugin.CachedSchema{
		SpecHash: "some-other-fingerprint",
		Tools:    []plugin.CachedTool{{Name: "do_thing"}},
	}); err != nil {
		t.Fatal(err)
	}

	cached, hashOK := LoadCachedToolsForSpecs([]plugin.Spec{fresh, stale, {Name: "absent"}})
	if len(cached["gh"]) != 1 || !hashOK["gh"] {
		t.Fatalf("fresh cache: tools=%v hashOK=%v", cached["gh"], hashOK["gh"])
	}
	if len(cached["old"]) != 1 || hashOK["old"] {
		t.Fatalf("stale cache must load with hashOK=false: tools=%v hashOK=%v", cached["old"], hashOK["old"])
	}
	if _, ok := cached["absent"]; ok {
		t.Fatal("server without cache must be absent")
	}
}

func TestBuildCatalogSurfacesCachedToolsForAutoStartFalse(t *testing.T) {
	cached := map[string][]plugin.CachedTool{
		"gh":  {{Name: "search_issues", Description: "search", ReadOnly: true}},
		"old": {{Name: "do_thing"}},
	}
	hashOK := map[string]bool{"gh": true, "old": false}
	cat := BuildCatalog(CatalogOptions{
		Plugins: []config.PluginEntry{
			{Name: "gh", AutoStart: boolPtr(false)},
			{Name: "old", AutoStart: boolPtr(false)},
		},
		Profile:     ProfileDelivery,
		CachedTools: cached,
		CacheHashOK: hashOK,
	})
	byID := map[string]Entry{}
	for _, e := range cat.Entries {
		byID[e.ID] = e
	}
	toolEntry, ok := byID["mcp-tool:gh/search_issues"]
	if !ok {
		t.Fatalf("cached tool missing from catalog: %v", cat.Entries)
	}
	if !toolEntry.ReadOnly || toolEntry.ToolName == "" {
		t.Fatalf("cached tool entry lost metadata: %+v", toolEntry)
	}
	if server := byID["mcp-server:old"]; server.Status != StatusStale {
		t.Fatalf("fingerprint-mismatched cache should mark the server stale, got %q", server.Status)
	}
	if staleTool, ok := byID["mcp-tool:old/do_thing"]; !ok {
		t.Fatal("stale cached tools should still appear as candidates")
	} else if staleTool.Status != StatusStale {
		t.Fatalf("stale server's cached tools must inherit stale, got %q", staleTool.Status)
	}
}

func TestRecordRouterUsageAccumulates(t *testing.T) {
	a := &Audit{}
	a.RecordRouterUsage(100, 20, 0.005, 340)
	a.RecordRouterUsage(50, 10, 0.002, 160)
	snap := a.Snapshot()
	if snap.RouterPromptTokens != 150 || snap.RouterCompletionTokens != 30 {
		t.Fatalf("token counters: prompt=%d completion=%d", snap.RouterPromptTokens, snap.RouterCompletionTokens)
	}
	if snap.RouterCost < 0.0069 || snap.RouterCost > 0.0071 {
		t.Fatalf("cost = %v", snap.RouterCost)
	}
	if snap.RouterLatencyMs != 500 {
		t.Fatalf("latency = %v", snap.RouterLatencyMs)
	}
}

func TestAuditRecordsDecisionFunnelAndDecline(t *testing.T) {
	a := &Audit{}
	a.RecordDecision(RouteDecision{Candidates: []RouteCandidate{
		{Policy: AutoUseRequire},
		{Policy: AutoUsePrefer},
		{Policy: AutoUseSuggest},
	}})
	a.RecordDecline()
	snap := a.Snapshot()
	if snap.RoutedCandidates != 3 || snap.RoutedRequire != 1 || snap.RoutedPrefer != 1 || snap.RoutedSuggest != 1 || snap.Declines != 1 {
		t.Fatalf("decision funnel audit: candidates=%d require=%d prefer=%d suggest=%d declines=%d",
			snap.RoutedCandidates, snap.RoutedRequire, snap.RoutedPrefer, snap.RoutedSuggest, snap.Declines)
	}
}

func TestDeliveryRouteRenderKeepsCapabilityIDAndProxyInstruction(t *testing.T) {
	entry := Entry{
		ID: "mcp-tool:gh/search_issues", Kind: KindMCPTool, Name: "gh/search_issues",
		Status: StatusConfigured, ConnectSource: "mcp", ConnectName: "gh",
	}
	d := RouteDecision{Delivery: true, Candidates: []RouteCandidate{{Entry: entry, Policy: AutoUsePrefer, Reason: "matches task"}}}
	out := RenderTransientBlock(d)
	if !strings.Contains(out, "mcp-tool:gh/search_issues") {
		t.Fatalf("delivery render must keep the concrete capability id:\n%s", out)
	}
	if !strings.Contains(out, `use_capability(action="call", capability_id="mcp-tool:gh/search_issues"`) {
		t.Fatalf("delivery render must instruct the proxy call:\n%s", out)
	}
	if strings.Contains(out, "connect_tool_source") {
		t.Fatalf("connect_tool_source is not registered in Delivery:\n%s", out)
	}
	// Server entries direct the model to connect-and-list via the same proxy.
	server := Entry{ID: "mcp-server:gh", Kind: KindMCPServer, Name: "gh", Status: StatusConfigured, ConnectSource: "mcp", ConnectName: "gh"}
	out = RenderTransientBlock(RouteDecision{Delivery: true, Candidates: []RouteCandidate{{Entry: server, Policy: AutoUseSuggest, Reason: "r"}}})
	if !strings.Contains(out, `use_capability(action="call", capability_id="mcp-server:gh")`) || !strings.Contains(out, "list its tools") {
		t.Fatalf("server candidate must instruct connect-and-list:\n%s", out)
	}
	// Non-delivery keeps the historical connect_tool_source instruction.
	d.Delivery = false
	out = RenderTransientBlock(d)
	if !strings.Contains(out, "connect_tool_source") {
		t.Fatalf("non-delivery render lost connect_tool_source:\n%s", out)
	}
}

func TestCatalogKeepsProxyToolsAfterConnect(t *testing.T) {
	proxy := map[string][]plugin.CachedTool{
		"gh": {{Name: "search_issues", Description: "search", ReadOnly: true}},
	}
	cat := BuildCatalog(CatalogOptions{
		Plugins:    []config.PluginEntry{{Name: "gh", AutoStart: boolPtr(false)}},
		Profile:    ProfileDelivery,
		Connected:  map[string]bool{"gh": true}, // server is ready now
		ProxyTools: proxy,
	})
	byID := map[string]Entry{}
	for _, e := range cat.Entries {
		byID[e.ID] = e
	}
	toolEntry, ok := byID["mcp-tool:gh/search_issues"]
	if !ok {
		t.Fatalf("proxy-connected tool vanished from catalog: %+v", cat.Entries)
	}
	if toolEntry.Status != StatusReady {
		t.Fatalf("proxy-connected tool should be ready, got %q", toolEntry.Status)
	}
	// When the same server's tools are already on the registry, no duplicates.
	cat = BuildCatalog(CatalogOptions{
		Tools:      []tool.ContractEntry{{Name: plugin.ModelToolName("gh", "search_issues")}},
		Plugins:    []config.PluginEntry{{Name: "gh", AutoStart: boolPtr(false)}},
		Profile:    ProfileDelivery,
		Connected:  map[string]bool{"gh": true},
		ProxyTools: proxy,
	})
	count := 0
	for _, e := range cat.Entries {
		if e.ID == "mcp-tool:gh/search_issues" {
			count++
		}
	}
	// The registry's own ToolEntries contribution is the single source here;
	// the proxy snapshot must not add a duplicate.
	if count != 1 {
		t.Fatalf("registry-backed server should have exactly one catalog entry, got %d", count)
	}
}
