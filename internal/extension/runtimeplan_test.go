package extension

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/extensioncontract"
)

func TestRuntimePlanNoOp(t *testing.T) {
	g, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "host", Provides: []extensioncontract.Capability{cap("reasonix", "provider", "p", "1.0.0", "sha256:p")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := DiffRuntimePlan(g, g, 1, 2)
	if !plan.IsNoOp() || plan.PrefixChanged || plan.ProviderChanged {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.MayChangePrefix() {
		t.Fatal("no-op plan must not predict a prefix change")
	}
	if len(plan.Unchanged) != 1 || plan.Unchanged[0] != "host" {
		t.Fatalf("unchanged = %v", plan.Unchanged)
	}
}

func TestRuntimePlanProviderOnlyChange(t *testing.T) {
	from, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "host", Provides: []extensioncontract.Capability{cap("reasonix", "provider", "p", "1.0.0", "sha256:a")}},
		{ID: "consumer", Requires: []extensioncontract.Requirement{req("reasonix", "provider", "p", ">=1.0.0", false)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "host", Provides: []extensioncontract.Capability{cap("reasonix", "provider", "p", "1.1.0", "sha256:b")}},
		{ID: "consumer", Requires: []extensioncontract.Requirement{req("reasonix", "provider", "p", ">=1.0.0", false)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := DiffRuntimePlan(from, to, 1, 2)
	if plan.IsNoOp() {
		t.Fatal("expected reload")
	}
	if plan.PrefixChanged {
		t.Fatal("graph diff must not report an observed prefix change")
	}
	if !plan.ProviderChanged {
		t.Fatal("provider-only change must set ProviderChanged")
	}
	if !plan.MayChangePrefix() {
		t.Fatal("provider-only change should conservatively rebuild/cache-check the snapshot")
	}
	// Host identity changed; consumer epoch changed → both reloaded.
	reloaded := map[ComponentID]bool{}
	for _, id := range plan.Reloaded {
		reloaded[id] = true
	}
	if !reloaded["host"] || !reloaded["consumer"] {
		t.Fatalf("reloaded = %v", plan.Reloaded)
	}
}

func TestRuntimePlanRemovedProviderDetected(t *testing.T) {
	from, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{cap("plugin/p", "provider", "x", "1.0.0", "sha256:a")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := BuildDependencyGraph(nil)
	if err != nil {
		t.Fatal(err)
	}
	plan := DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != SubgraphProviderOnly {
		t.Fatalf("kind = %v, want provider-only", plan.Kind)
	}
	if !plan.ProviderChanged {
		t.Fatal("removed provider must set ProviderChanged")
	}
	if plan.PrefixChanged {
		t.Fatal("graph diff must not invent an observed prefix change")
	}
}

func TestRuntimePlanMCPSchemaChangeRequiresFullRebuild(t *testing.T) {
	from, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/m", Provides: []extensioncontract.Capability{cap("plugin/m", "mcp", "server", "1.0.0", "sha256:a")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/m", Provides: []extensioncontract.Capability{cap("plugin/m", "mcp", "server", "1.0.0", "sha256:b")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != SubgraphFull {
		t.Fatalf("kind = %v, want full rebuild for MCP schema change", plan.Kind)
	}
	if plan.ProviderChanged {
		t.Fatal("MCP-only change must not set ProviderChanged")
	}
}

func TestRuntimePlanMCPBackendChangeKeepsNarrowPlan(t *testing.T) {
	from, err := BuildDependencyGraph([]ComponentDescriptor{
		{
			ID:       "plugin/m",
			Source:   ContributionSource{Scope: ScopePlugin, PluginID: "m", Version: "1.0.0"},
			Provides: []extensioncontract.Capability{cap("plugin/m", "mcp", "server", "1.0.0", "sha256:stable")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := BuildDependencyGraph([]ComponentDescriptor{
		{
			ID:       "plugin/m",
			Source:   ContributionSource{Scope: ScopePlugin, PluginID: "m", Version: "1.0.1"},
			Provides: []extensioncontract.Capability{cap("plugin/m", "mcp", "server", "1.0.0", "sha256:stable")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != SubgraphMCPOnly {
		t.Fatalf("kind = %v, want MCP-only", plan.Kind)
	}
	if plan.PrefixChanged || plan.ProviderChanged {
		t.Fatalf("observed flags must remain false before snapshot comparison: %+v", plan)
	}
}

func TestRuntimePlanViewUsesObservedDiagnosticFields(t *testing.T) {
	raw, err := json.Marshal(PlanView(&RuntimePlan{PrefixChanged: true, ProviderChanged: true}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"prefixChanged":true`, `"providerChanged":true`} {
		if !strings.Contains(text, field) {
			t.Fatalf("plan JSON %s missing %s", text, field)
		}
	}
	if strings.Contains(text, "cacheChanged") {
		t.Fatalf("plan JSON retains ambiguous cacheChanged field: %s", text)
	}
}

func TestRuntimePlanAddedRemoved(t *testing.T) {
	from, _ := BuildDependencyGraph([]ComponentDescriptor{{ID: "a"}})
	to, _ := BuildDependencyGraph([]ComponentDescriptor{{ID: "b"}})
	plan := DiffRuntimePlan(from, to, 1, 2)
	if len(plan.Added) != 1 || plan.Added[0] != "b" {
		t.Fatalf("added = %v", plan.Added)
	}
	if len(plan.Removed) != 1 || plan.Removed[0] != "a" {
		t.Fatalf("removed = %v", plan.Removed)
	}
}
