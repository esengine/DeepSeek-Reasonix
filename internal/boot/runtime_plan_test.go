package boot

import (
	"context"
	"testing"

	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/sidecar"
	"reasonix/internal/extensioncontract"
	"reasonix/internal/provider"
)

func TestRebuildPlanNoOpCacheGuard(t *testing.T) {
	isolateConfigHome(t)
	oldRes, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(func() {
		if oldRes.Controller != nil {
			oldRes.Controller.Close()
		}
	})
	res, err := RebuildFrom(context.Background(), oldRes, Options{})
	if err != nil {
		t.Fatalf("RebuildFrom: %v", err)
	}
	t.Cleanup(func() {
		if res.Controller != nil {
			res.Controller.Close()
		}
	})
	if res.Plan == nil {
		t.Fatal("expected plan")
	}
	if res.Plan.IsNoOp() && res.Plan.PrefixChanged {
		t.Fatal("no-op plan marked PrefixChanged")
	}
	if oldRes.Snapshot != nil && res.Snapshot != nil && res.Plan.IsNoOp() {
		if oldRes.Snapshot.CacheHash() != res.Snapshot.CacheHash() {
			t.Fatalf("no-op rebuild changed CacheHash: %s -> %s", oldRes.Snapshot.CacheHash(), res.Snapshot.CacheHash())
		}
	}
}

func TestAttachPlanAndStatusObservesActualPrefixHash(t *testing.T) {
	empty := buildPlanGraph(t, nil)
	uiFrom := buildPlanGraph(t, []extension.ComponentDescriptor{{
		ID: "plugin/ui", Provides: []extensioncontract.Capability{planCapability("plugin/ui", "ui", "panel", "1.0.0", "sha256:a")},
	}})
	uiTo := buildPlanGraph(t, []extension.ComponentDescriptor{{
		ID: "plugin/ui", Provides: []extensioncontract.Capability{planCapability("plugin/ui", "ui", "panel", "1.0.1", "sha256:b")},
	}})
	providerFrom := buildPlanGraph(t, []extension.ComponentDescriptor{{
		ID: "plugin/provider", Provides: []extensioncontract.Capability{planCapability("plugin/provider", "provider", "main", "1.0.0", "sha256:a")},
	}})
	providerTo := buildPlanGraph(t, []extension.ComponentDescriptor{{
		ID: "plugin/provider", Provides: []extensioncontract.Capability{planCapability("plugin/provider", "provider", "main", "1.0.1", "sha256:b")},
	}})
	mcpFrom := buildPlanGraph(t, []extension.ComponentDescriptor{{
		ID:       "plugin/mcp",
		Source:   extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: "mcp", Version: "1.0.0"},
		Provides: []extensioncontract.Capability{planCapability("plugin/mcp", "mcp", "server", "1.0.0", "sha256:stable")},
	}})
	mcpTo := buildPlanGraph(t, []extension.ComponentDescriptor{{
		ID:       "plugin/mcp",
		Source:   extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: "mcp", Version: "1.0.1"},
		Provides: []extensioncontract.Capability{planCapability("plugin/mcp", "mcp", "server", "1.0.0", "sha256:stable")},
	}})
	stable := buildPlanSnapshot(t, "system-a", provider.ToolSchema{Name: "bash", Description: "stable"})
	changedPrompt := buildPlanSnapshot(t, "system-b", provider.ToolSchema{Name: "bash", Description: "stable"})
	changedTool := buildPlanSnapshot(t, "system-a", provider.ToolSchema{Name: "bash", Description: "changed"})

	tests := []struct {
		name         string
		from         *extension.DependencyGraph
		to           *extension.DependencyGraph
		old          *extension.RuntimeSnapshot
		neo          *extension.RuntimeSnapshot
		wantPrefix   bool
		wantProvider bool
	}{
		{name: "no-op", from: empty, to: empty, old: stable, neo: stable.WithGeneration(2)},
		{name: "UI-only", from: uiFrom, to: uiTo, old: stable, neo: stable.WithGeneration(2)},
		{name: "provider-only", from: providerFrom, to: providerTo, old: stable, neo: stable.WithGeneration(2), wantProvider: true},
		{name: "MCP backend only", from: mcpFrom, to: mcpTo, old: stable, neo: stable.WithGeneration(2)},
		{name: "system prompt", from: empty, to: empty, old: stable, neo: changedPrompt, wantPrefix: true},
		{name: "tool schema", from: empty, to: empty, old: stable, neo: changedTool, wantPrefix: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := &BuildResult{Snapshot: tc.neo}
			attachPlanAndStatus(res, tc.from, tc.to, 1, tc.old)
			if res.Plan == nil || res.Plan.PrefixChanged != tc.wantPrefix || res.Plan.ProviderChanged != tc.wantProvider {
				t.Fatalf("plan = %+v, want PrefixChanged=%v ProviderChanged=%v", res.Plan, tc.wantPrefix, tc.wantProvider)
			}
			if res.Status == nil || res.Status.Plan == nil || res.Status.Plan.PrefixChanged != tc.wantPrefix || res.Status.Plan.ProviderChanged != tc.wantProvider {
				t.Fatalf("status plan = %+v, want PrefixChanged=%v ProviderChanged=%v", res.Status, tc.wantPrefix, tc.wantProvider)
			}
		})
	}
}

func buildPlanGraph(t *testing.T, components []extension.ComponentDescriptor) *extension.DependencyGraph {
	t.Helper()
	graph, err := extension.BuildDependencyGraph(components)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

func planCapability(namespace, kind, id, version, schemaHash string) extensioncontract.Capability {
	return extensioncontract.Capability{
		Key:     extensioncontract.CapabilityKey{Namespace: namespace, Kind: kind, ID: id},
		Version: version, SchemaHash: schemaHash,
	}
}

func buildPlanSnapshot(t *testing.T, prompt string, schema provider.ToolSchema) *extension.RuntimeSnapshot {
	t.Helper()
	builder := extension.NewBuilder().WithSystemPrompt(prompt).WithGeneration(2)
	builder.AddContributor(extension.ContributorFunc{
		ContributorName: "plan-test-tool",
		Fn: func(context.Context) ([]extension.Contribution, error) {
			return []extension.Contribution{{
				Kind:    extension.KindTool,
				ID:      schema.Name,
				Source:  extension.ContributionSource{Scope: extension.ScopeBuiltin, Origin: "plan-test"},
				Payload: schema,
			}}, nil
		},
	})
	snapshot, runtimeSet, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtimeSet != nil {
		t.Cleanup(func() { _ = runtimeSet.Close() })
	}
	return snapshot
}

func TestStartPackagesWithPlanEmptyHome(t *testing.T) {
	plan := &extension.RuntimePlan{
		Unchanged: []extension.ComponentID{"plugin/demo"},
	}
	session := protocol.SessionContext{SessionID: "s", WorkspaceRoot: t.TempDir(), Generation: 1}
	m, warnings, err := sidecar.StartPackagesWithPlan(context.Background(), t.TempDir(), session, nil, nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if m == nil {
		t.Fatal("nil manager")
	}
	if len(m.Clients()) != 0 {
		t.Fatalf("clients = %d, want 0", len(m.Clients()))
	}
	_ = m.Close()
}
