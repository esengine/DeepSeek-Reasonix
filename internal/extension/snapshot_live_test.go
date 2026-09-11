package extension

import (
	"testing"

	"reasonix/internal/provider"
)

func TestWithLiveContributionsPreservesCacheHash(t *testing.T) {
	base := &RuntimeSnapshot{
		generation:   1,
		systemPrompt: "sys",
		toolSchemas:  []provider.ToolSchema{{Name: "bash"}},
		cacheHash:    "hash-stable",
		systemHash:   "sys-h",
		toolsHash:    "tools-h",
		interceptorChain: map[InterceptorPoint][]Contribution{
			PointToolBefore: {{Kind: KindInterceptor, ID: string(PointToolBefore), Source: ContributionSource{Scope: ScopePlugin, PluginID: "old"}}},
		},
		catalog: NewCatalog(),
	}
	base.catalog.Add(Contribution{Kind: KindTool, ID: "bash", Source: ContributionSource{Scope: ScopeBuiltin}})
	base.catalog.freeze()

	live := []Contribution{
		{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: 1, Source: ContributionSource{Scope: ScopePlugin, PluginID: "new"}, Payload: "x"},
		{Kind: KindProvider, ID: "p/m", Source: ContributionSource{Scope: ScopePlugin, PluginID: "new"}, Payload: provider.Descriptor{Ref: "p/m"}},
	}
	next := base.WithLiveContributions(9, live)
	if next.Generation() != 9 {
		t.Fatalf("gen = %d", next.Generation())
	}
	if next.CacheHash() != "hash-stable" {
		t.Fatal("CacheHash must not churn on live contribution refresh")
	}
	if next.SystemPrompt() != "sys" {
		t.Fatal("system prompt moved")
	}
	chain := next.InterceptorChain()[PointToolBefore]
	if len(chain) != 1 || chain[0].Source.PluginID != "new" {
		t.Fatalf("chain = %+v", chain)
	}
	// Empty live must clear plugin interceptors (plugin removed).
	cleared := base.WithLiveContributions(10, nil)
	if len(cleared.InterceptorChain()[PointToolBefore]) != 0 {
		t.Fatal("empty live must drop plugin interceptors")
	}
	// Builtin tool must still be present.
	found := false
	for _, ct := range next.Catalog().ByKind(KindTool) {
		if ct.ID == "bash" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected host tool retained")
	}
}
