package extension

import (
	"testing"

	"reasonix/internal/extensioncontract"
)

func TestClassifySubgraphNone(t *testing.T) {
	g, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "host", Provides: []extensioncontract.Capability{cap("reasonix", "provider", "p", "1.0.0", "sha256:p")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := DiffRuntimePlan(g, g, 1, 2)
	if plan.Kind != SubgraphNone || plan.MayChangePrefix() {
		t.Fatalf("kind=%v mayChangePrefix=%v", plan.Kind, plan.MayChangePrefix())
	}
}

func TestClassifySubgraphInterceptorOnly(t *testing.T) {
	from, _ := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/a", Intercepts: []InterceptorPoint{PointToolBefore}},
	})
	to, _ := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/a", Intercepts: []InterceptorPoint{PointToolBefore, PointToolAfter},
			Provides: []extensioncontract.Capability{{
				Key: extensioncontract.CapabilityKey{Namespace: "plugin/a", Kind: "interceptors", ID: "default"}, Version: "1.0.0",
			}}},
	})
	plan := DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != SubgraphInterceptorOnly {
		t.Fatalf("kind = %v, want interceptor-only", plan.Kind)
	}
	if plan.MayChangePrefix() {
		t.Fatal("interceptor-only must not affect cache")
	}
	if !plan.AffectsInterceptors() {
		t.Fatal("expected AffectsInterceptors")
	}
}

func TestClassifySubgraphProviderOnly(t *testing.T) {
	from, _ := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{cap("plugin/p", "provider", "x", "1.0.0", "sha256:a")}},
	})
	to, _ := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plugin/p", Provides: []extensioncontract.Capability{cap("plugin/p", "provider", "x", "1.1.0", "sha256:b")}},
	})
	plan := DiffRuntimePlan(from, to, 1, 2)
	if plan.Kind != SubgraphProviderOnly {
		t.Fatalf("kind = %v, want provider-only", plan.Kind)
	}
	if !plan.AffectsProviders() || !plan.MayChangePrefix() {
		t.Fatal("provider-only should affect providers and conservatively check the prefix")
	}
	if !plan.ProviderChanged || plan.PrefixChanged {
		t.Fatalf("provider-only observation flags = provider:%v prefix:%v", plan.ProviderChanged, plan.PrefixChanged)
	}
}
