package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/capability"
	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

func TestUseCapabilityListSummarizesMCPWithoutExpandingCachedDirectories(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	specs := []plugin.Spec{
		{Name: "disabled", Type: "stdio", Command: "disabled-mcp", Authorized: true},
		{Name: "enabled", Type: "stdio", Command: "enabled-mcp", Authorized: true},
	}
	entries := []config.PluginEntry{
		{Name: "disabled", Type: "stdio", Command: "disabled-mcp"},
		{Name: "enabled", Type: "stdio", Command: "enabled-mcp"},
	}
	for _, spec := range specs {
		cached := make([]plugin.CachedTool, 64)
		for i := range cached {
			cached[i] = plugin.CachedTool{
				Name:        fmt.Sprintf("tool_%03d", i),
				Description: fmt.Sprintf("catalog-bloat-sentinel-%s-%03d-%s", spec.Name, i, strings.Repeat("x", 256)),
				ReadOnly:    true,
			}
		}
		if err := plugin.SaveCachedSchema(spec.Name, plugin.CachedSchema{
			CacheKey: plugin.SchemaCacheKey(spec),
			Tools:    cached,
		}); err != nil {
			t.Fatal(err)
		}
	}

	host := plugin.NewHost()
	defer host.Close()
	reg := tool.NewRegistry()
	var runtime *MCPCapabilityRuntime
	catalogFn := func() capability.Catalog {
		plugins, cached, keyOK, disabled, proxyTools := runtime.CapabilityCatalogState()
		catalog := capability.BuildCatalog(capability.CatalogOptions{
			Tools:       reg.AllContractEntries(),
			Plugins:     plugins,
			Disabled:    disabled,
			CachedTools: cached,
			CacheKeyOK:  keyOK,
			ProxyTools:  proxyTools,
		})
		catalog.Entries = append(catalog.Entries, capability.Entry{
			ID: "skill:review", Kind: capability.KindSkill, Name: "review", Status: capability.StatusReady,
		})
		return catalog
	}
	runtime = NewMCPCapabilityRuntime(context.Background(), host, specs, reg, catalogFn)
	runtime.ConfigureServers(entries, specs, map[string]bool{"enabled": true})
	frontend := runtime.NewFrontend(nil, nil)

	out, err := frontend.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Capabilities []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"capabilities"`
		Servers []listServerInfo `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode list result: %v\n%s", err, out)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0].ID != "skill:review" {
		t.Fatalf("list expanded MCP entries instead of keeping only non-MCP capabilities: %+v", payload.Capabilities)
	}
	if len(payload.Servers) != 2 || payload.Servers[0].Name != "disabled" || payload.Servers[0].Status != "disabled" || payload.Servers[1].Name != "enabled" || payload.Servers[1].Status != "configured" {
		t.Fatalf("server summaries = %+v, want disabled/configured in stable name order", payload.Servers)
	}
	if strings.Contains(out, "catalog-bloat-sentinel") {
		t.Fatalf("list leaked concrete MCP directory entries:\n%s", out)
	}
	if len(out) >= 4096 {
		t.Fatalf("summary list grew with cached tool descriptions: bytes=%d", len(out))
	}
	t.Logf("compact list bytes=%d for %d cached MCP tools", len(out), len(specs)*64)

	inspected, err := frontend.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:enabled"}`))
	if err != nil || !strings.Contains(inspected, "catalog-bloat-sentinel-enabled-000") {
		t.Fatalf("inspect did not preserve the selected server's cached directory: %v\n%s", err, inspected)
	}
	if host.HasClient("enabled") {
		t.Fatal("inspect started the selected MCP server")
	}
	disabledInspect, err := frontend.Execute(context.Background(), json.RawMessage(`{"action":"inspect","capability_id":"mcp-server:disabled"}`))
	if err != nil || !strings.Contains(disabledInspect, "disabled") || strings.Contains(disabledInspect, "catalog-bloat-sentinel") {
		t.Fatalf("disabled inspect exposed a non-actionable cached directory: %v\n%s", err, disabledInspect)
	}
}
