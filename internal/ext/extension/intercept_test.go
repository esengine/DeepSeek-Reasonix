package extension

import (
	"testing"
)

// TestValidatePriorityBounds: the documented range is [-1000, 1000],
// inclusive. Unbounded priorities would let one contributor permanently
// outrank every future interceptor.
func TestValidatePriorityBounds(t *testing.T) {
	for _, p := range []int{-1000, -1, 0, 1, 1000} {
		if err := ValidatePriority(p); err != nil {
			t.Errorf("ValidatePriority(%d) = %v, want ok", p, err)
		}
	}
	for _, p := range []int{-1001, 1001, -100000, 100000} {
		if err := ValidatePriority(p); err == nil {
			t.Errorf("ValidatePriority(%d) succeeded, want error", p)
		}
	}
}

// TestSortInterceptors pins the exact execution order: priority ascending,
// then plugin ID ascending, then per-contributor registration order
// ascending. The input slice must not be mutated — chains are shared state.
func TestSortInterceptors(t *testing.T) {
	in := []Contribution{
		{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: 10, Source: src(ScopePlugin, "plug-a", "plugin"), Order: 0},
		{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: -5, Source: src(ScopePlugin, "plug-b", "plugin"), Order: 2},
		{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: -5, Source: src(ScopePlugin, "plug-a", "plugin"), Order: 7},
		{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: -5, Source: src(ScopePlugin, "plug-a", "plugin"), Order: 3},
		{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: 0, Source: src(ScopeProject, "", "project"), Order: 1},
	}
	got := SortInterceptors(in)
	type key struct {
		priority int
		plugin   string
		order    int
	}
	want := []key{
		{-5, "plug-a", 3},
		{-5, "plug-a", 7},
		{-5, "plug-b", 2},
		{0, "", 1},
		{10, "plug-a", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("sorted %d entries, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Priority != w.priority || g.Source.PluginID != w.plugin || g.Order != w.order {
			t.Fatalf("position %d: got (p=%d, plugin=%s, order=%d), want %+v",
				i, g.Priority, g.Source.PluginID, g.Order, w)
		}
	}
	// Input untouched: position 0 must still hold the priority-10 entry.
	if in[0].Priority != 10 {
		t.Fatal("SortInterceptors mutated its input")
	}
}

// TestInterceptorChainAssembly: the builder groups interceptors by point and
// orders each chain per SortInterceptors; points with no interceptors are
// absent from the map.
func TestInterceptorChainAssembly(t *testing.T) {
	b := NewBuilder()
	b.AddContributor(staticContributor("i",
		Contribution{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: 20, Source: src(ScopePlugin, "pb", "plugin"), Payload: "late"},
		Contribution{Kind: KindInterceptor, ID: string(PointToolBefore), Priority: -1, Source: src(ScopePlugin, "pa", "plugin"), Payload: "early"},
		Contribution{Kind: KindInterceptor, ID: string(PointProviderResponse), Priority: 0, Source: src(ScopeProject, "", "project"), Payload: "resp"},
	))
	snap, _, err := b.Build(t.Context())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	chains := snap.InterceptorChain()
	if len(chains) != 2 {
		t.Fatalf("chain points = %v, want tool.before + provider.response", chains)
	}
	before := chains[PointToolBefore]
	if len(before) != 2 || before[0].Payload != "early" || before[1].Payload != "late" {
		t.Fatalf("tool.before chain = %v, want early then late", before)
	}
	if _, present := chains[PointSessionStart]; present {
		t.Fatal("empty point present in chain map")
	}
}
