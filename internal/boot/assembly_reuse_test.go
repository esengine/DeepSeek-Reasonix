package boot

import (
	"testing"

	"reasonix/internal/extension"
)

func TestShouldReuseDiscoveryKinds(t *testing.T) {
	cases := []struct {
		kind extension.SubgraphKind
		want bool
	}{
		{extension.SubgraphNone, true},
		{extension.SubgraphInterceptorOnly, true},
		{extension.SubgraphUIOnly, true},
		{extension.SubgraphProviderOnly, true},
		{extension.SubgraphMCPOnly, true},
		{extension.SubgraphSidecar, false},
		{extension.SubgraphFull, false},
	}
	for _, tc := range cases {
		plan := &extension.RuntimePlan{Kind: tc.kind, Added: []extension.ComponentID{"x"}}
		if tc.kind == extension.SubgraphNone {
			plan.Added = nil
		}
		if got := shouldReuseDiscovery(plan); got != tc.want {
			t.Fatalf("kind %v: got %v want %v", tc.kind, got, tc.want)
		}
	}
	if shouldReuseDiscovery(nil) {
		t.Fatal("nil plan must not reuse")
	}
}

func TestShouldReuseSnapshot(t *testing.T) {
	if !shouldReuseSnapshot(&extension.RuntimePlan{Kind: extension.SubgraphNone}) {
		t.Fatal("no-op should reuse snapshot")
	}
	if !shouldReuseSnapshot(&extension.RuntimePlan{Kind: extension.SubgraphUIOnly, Reloaded: []extension.ComponentID{"a"}}) {
		t.Fatal("UI-only should reuse snapshot body")
	}
	if shouldReuseSnapshot(&extension.RuntimePlan{Kind: extension.SubgraphProviderOnly, Reloaded: []extension.ComponentID{"a"}}) {
		t.Fatal("provider-only must not claim snapshot cache reuse")
	}
}
