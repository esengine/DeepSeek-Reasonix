package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
)

func TestChooseMCPExposureKeepsSmallKnownSurfaceDirect(t *testing.T) {
	specs := []plugin.Spec{{Name: "one"}}
	cached := map[string][]plugin.CachedTool{"one": cachedMCPTools(15, 32)}
	decision := chooseMCPExposure(specs, cached, map[string]bool{"one": true}, 128_000)
	if decision.Mode != mcpExposurePerTool {
		t.Fatalf("mode = %q, want %q: %+v", decision.Mode, mcpExposurePerTool, decision)
	}
	if decision.KnownTools != 15 || decision.EstimatedTools != 15 || decision.UnknownServers != 0 {
		t.Fatalf("unexpected decision metrics: %+v", decision)
	}
}

func TestChooseMCPExposureCollapsesLargeKnownToolSurface(t *testing.T) {
	specs := []plugin.Spec{{Name: "one"}}
	cached := map[string][]plugin.CachedTool{"one": cachedMCPTools(autoMCPToolThreshold, 32)}
	decision := chooseMCPExposure(specs, cached, map[string]bool{"one": true}, 128_000)
	if decision.Mode != mcpExposureCapability {
		t.Fatalf("mode = %q, want %q: %+v", decision.Mode, mcpExposureCapability, decision)
	}
	if !strings.Contains(decision.Reason, "tool count") {
		t.Fatalf("reason = %q, want tool-count explanation", decision.Reason)
	}
}

func TestChooseMCPExposureCollapsesLargeSchemaSurface(t *testing.T) {
	specs := []plugin.Spec{{Name: "one"}}
	cached := map[string][]plugin.CachedTool{"one": cachedMCPTools(1, autoMCPMinSchemaBytes)}
	decision := chooseMCPExposure(specs, cached, map[string]bool{"one": true}, 0)
	if decision.Mode != mcpExposureCapability {
		t.Fatalf("mode = %q, want %q: %+v", decision.Mode, mcpExposureCapability, decision)
	}
	if !strings.Contains(decision.Reason, "schema size") {
		t.Fatalf("reason = %q, want schema-size explanation", decision.Reason)
	}
}

func TestChooseMCPExposureEstimatesColdServersWithoutTrustingStaleCache(t *testing.T) {
	specs := []plugin.Spec{{Name: "cold-a"}, {Name: "cold-b"}, {Name: "cold-a"}}
	cached := map[string][]plugin.CachedTool{
		"cold-a": cachedMCPTools(1, 32),
		"cold-b": cachedMCPTools(1, 32),
	}
	decision := chooseMCPExposure(specs, cached, map[string]bool{
		"cold-a": false, // stale spec hash: do not trust the concrete tool count
		"cold-b": false,
	}, 128_000)
	if decision.Mode != mcpExposureCapability {
		t.Fatalf("mode = %q, want %q: %+v", decision.Mode, mcpExposureCapability, decision)
	}
	if decision.KnownTools != 0 || decision.UnknownServers != 2 || decision.EstimatedTools != autoMCPToolThreshold {
		t.Fatalf("unexpected cold-surface metrics: %+v", decision)
	}
}

func TestChooseMCPExposureCollapsesColdServerToKeepSurfaceStable(t *testing.T) {
	decision := chooseMCPExposure([]plugin.Spec{{Name: "cold"}}, nil, nil, 128_000)
	if decision.Mode != mcpExposureCapability {
		t.Fatalf("mode = %q, want %q: %+v", decision.Mode, mcpExposureCapability, decision)
	}
	if decision.EstimatedTools != autoMCPUnknownToolsEstimate || decision.UnknownServers != 1 {
		t.Fatalf("unexpected cold-server estimate: %+v", decision)
	}
	if !strings.Contains(decision.Reason, "unavailable or stale") {
		t.Fatalf("reason = %q, want cache-stability explanation", decision.Reason)
	}
}

func TestMCPSchemaBudgetScalesWithLargeContextWindows(t *testing.T) {
	if got := mcpSchemaBudget(0); got != autoMCPMinSchemaBytes {
		t.Fatalf("zero-window budget = %d, want %d", got, autoMCPMinSchemaBytes)
	}
	if got := mcpSchemaBudget(32_000); got != autoMCPMinSchemaBytes {
		t.Fatalf("small-window budget = %d, want floor %d", got, autoMCPMinSchemaBytes)
	}
	if got := mcpSchemaBudget(1_000_000); got != 200_000 {
		t.Fatalf("large-window budget = %d, want 200000", got)
	}
}

func TestMCPExposureNoticeContainsOnlyBoundedMetrics(t *testing.T) {
	decision := mcpExposureDecision{
		Mode:                 mcpExposureCapability,
		KnownTools:           20,
		EstimatedTools:       20,
		EstimatedSchemaBytes: 30_000,
		SchemaBudget:         16_384,
	}
	notice := decision.notice()
	for _, want := range []string{"using use_capability automatically", "20 known tools", "30000 estimated schema bytes"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice %q missing %q", notice, want)
		}
	}
}

func TestBuildAutomaticallyUsesCapabilityForLargeMCPSurface(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("auto-mcp-large", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "alpha"
command = "reasonix-missing-alpha"

[[plugins]]
name = "beta"
command = "reasonix-missing-beta"
`)
	seedMCPExposureCaches(t, dir, autoMCPToolThreshold/2)

	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	})})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "capture automatic MCP surface"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if !requestHasTool(reqs[0], "use_capability") {
		t.Fatalf("large MCP surface must expose use_capability; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	if requestHasToolPrefix(reqs[0], "mcp__") {
		t.Fatalf("large MCP surface must hide concrete mcp__ tools; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	if failures := ctrl.Host().Failures(); len(failures) != 0 {
		t.Fatalf("automatic proxy mode must not start missing MCP processes at boot; failures=%v", failures)
	}
	foundNotice := false
	for _, notice := range notices {
		if strings.Contains(notice.Text, "using use_capability automatically") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("automatic proxy decision notice missing: %+v", notices)
	}
}

func TestBuildAutomaticMCPProxyRoutesThroughCapability(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("auto-mcp-route", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "alpha"
command = "reasonix-missing-alpha"
`)
	seedMCPExposureCaches(t, dir, autoMCPToolThreshold)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "use alpha mcp"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := prov.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if !requestHasTool(reqs[0], "use_capability") {
		t.Fatalf("automatic MCP proxy must expose use_capability; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	if requestHasTool(reqs[0], "connect_tool_source") {
		t.Fatalf("Balanced automatic MCP proxy must not expose connect_tool_source; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	if !requestMessageContains(reqs[0].Messages, provider.RoleUser, "use_capability(action=\"call\"") {
		t.Fatalf("automatic MCP route must direct the model to use_capability:\n%s", requestUserContent(reqs[0]))
	}
	if requestMessageContains(reqs[0].Messages, provider.RoleUser, "connect_tool_source") {
		t.Fatalf("automatic MCP route must not direct the model to connect_tool_source:\n%s", requestUserContent(reqs[0]))
	}
}

func TestBuildKeepsSmallMCPSurfaceDirect(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	registerBootTokenProfileTestProvider()
	prov := testutil.NewMock("auto-mcp-small", testutil.Turn{Text: "done"})
	setBootTokenProfileTestProvider(t, prov)
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"

[[plugins]]
name = "small"
command = "reasonix-missing-small"
`)
	seedMCPExposureCaches(t, dir, 2)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "capture direct MCP surface"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := prov.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if requestHasTool(reqs[0], "use_capability") {
		t.Fatalf("small MCP surface should stay direct; tools=%v", toolSchemaNames(reqs[0].Tools))
	}
	for _, want := range []string{"mcp__small__tool_0", "mcp__small__tool_1"} {
		if !requestHasTool(reqs[0], want) {
			t.Fatalf("small MCP surface missing %q; tools=%v", want, toolSchemaNames(reqs[0].Tools))
		}
	}
}

func seedMCPExposureCaches(t *testing.T, root string, toolsPerServer int) {
	t.Helper()
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	specs := PluginSpecsForRootWithOptions(cfg.Plugins, root, PluginSpecOptions{})
	if len(specs) == 0 {
		t.Fatal("expected configured MCP specs")
	}
	for _, spec := range specs {
		if err := plugin.SaveCachedSchema(spec.Name, plugin.CachedSchema{
			CacheKey:     plugin.SchemaCacheKey(spec),
			Capabilities: map[string]bool{"tools": true},
			Tools:        cachedMCPTools(toolsPerServer, 32),
		}); err != nil {
			t.Fatalf("SaveCachedSchema(%s): %v", spec.Name, err)
		}
	}
}

func cachedMCPTools(count, schemaBytes int) []plugin.CachedTool {
	tools := make([]plugin.CachedTool, 0, count)
	for i := 0; i < count; i++ {
		tools = append(tools, plugin.CachedTool{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: "cached MCP tool",
			Schema:      json.RawMessage(`{"type":"object","description":"` + strings.Repeat("x", schemaBytes) + `"}`),
		})
	}
	return tools
}

func requestUserContent(req provider.Request) string {
	var content strings.Builder
	for _, message := range req.Messages {
		if message.Role != provider.RoleUser {
			continue
		}
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}
		content.WriteString(message.Content)
	}
	return content.String()
}
