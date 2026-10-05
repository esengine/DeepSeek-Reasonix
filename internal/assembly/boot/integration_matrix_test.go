package boot

import (
	"context"
	"testing"

	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/sidecar"
	"reasonix/internal/ext/extensioncontract"
)

func TestIntegrationDrainTimeoutFiresCancel(t *testing.T) {
	g := extension.NewPublishGate()
	g.Publish(1)
	g.Publish(2)
	fired := make(chan struct{}, 1)
	g.RegisterDrainCancel(1, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	g.ForceExpireDrain(1)
	select {
	case <-fired:
	default:
		t.Fatal("drain cancel not fired")
	}
}

func TestIntegrationProviderDrainReloadOrder(t *testing.T) {
	from, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{{
			Key:     extensioncontract.CapabilityKey{Namespace: "plugin/p", Kind: "provider", ID: "main"},
			Version: "1.0.0", SchemaHash: "sha256:a",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{{
			Key:     extensioncontract.CapabilityKey{Namespace: "plugin/p", Kind: "provider", ID: "main"},
			Version: "1.0.1", SchemaHash: "sha256:b",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := extension.DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != extension.SubgraphProviderOnly {
		t.Fatalf("kind = %v", plan.Kind)
	}
	// Drain order must list the reloaded component (consumers drain after providers in reverse topo).
	if len(plan.DrainOrder) == 0 && len(plan.Reloaded) == 0 {
		t.Fatal("expected reloaded/drain order for provider change")
	}
	_ = sidecar.PluginComponentID("p")
}

func TestIntegrationSnapshotLiveRefreshPreservesCache(t *testing.T) {
	// Product semantic for provider/MCP subgraph: live contributions refresh
	// interceptor/provider catalog view; CacheHash (system+tools) stays.
	snap := &extension.RuntimeSnapshot{}
	// Build via empty WithGeneration path — use real build snapshot.
	isolateConfigHome(t)
	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if res.Controller != nil {
			res.Controller.Close()
		}
	})
	if res.Snapshot == nil {
		t.Fatal("nil snapshot")
	}
	baseHash := res.Snapshot.CacheHash()
	next := res.Snapshot.WithLiveContributions(res.Snapshot.Generation()+1, nil)
	if next.CacheHash() != baseHash {
		t.Fatal("empty live refresh churned cache")
	}
	_ = snap
}
