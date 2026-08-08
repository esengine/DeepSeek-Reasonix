package extension

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/extensioncontract"
)

func cap(ns, kind, id, ver, hash string) extensioncontract.Capability {
	return extensioncontract.Capability{
		Key:        extensioncontract.CapabilityKey{Namespace: ns, Kind: kind, ID: id},
		Version:    ver,
		SchemaHash: hash,
	}
}

func req(ns, kind, id, rangeExpr string, optional bool) extensioncontract.Requirement {
	return extensioncontract.Requirement{
		Capability:   extensioncontract.Capability{Key: extensioncontract.CapabilityKey{Namespace: ns, Kind: kind, ID: id}},
		VersionRange: rangeExpr,
		Optional:     optional,
	}
}

func TestDependencyGraphExactAndRange(t *testing.T) {
	g, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "host", Provides: []extensioncontract.Capability{cap("reasonix", "provider", "deepseek/v4", "1.2.0", "sha256:p")}},
		{ID: "plug", Requires: []extensioncontract.Requirement{req("reasonix", "provider", "deepseek/v4", ">=1.0.0", false)},
			Provides: []extensioncontract.Capability{cap("plugin/ex", "tool", "t", "1.0.0", "sha256:t")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	order := g.ActivateOrder()
	if len(order) != 2 || order[0] != "host" || order[1] != "plug" {
		t.Fatalf("activate order = %v", order)
	}
	drain := g.DrainOrder()
	if len(drain) != 2 || drain[0] != "plug" || drain[1] != "host" {
		t.Fatalf("drain order = %v", drain)
	}
}

func TestDependencyGraphSchemaMismatch(t *testing.T) {
	_, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "host", Provides: []extensioncontract.Capability{cap("reasonix", "provider", "p", "1.0.0", "sha256:a")}},
		{ID: "plug", Requires: []extensioncontract.Requirement{{
			Capability: extensioncontract.Capability{
				Key:        extensioncontract.CapabilityKey{Namespace: "reasonix", Kind: "provider", ID: "p"},
				SchemaHash: "sha256:b",
			},
			VersionRange: ">=1.0.0",
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "dependency_unsatisfied") {
		t.Fatalf("err = %v", err)
	}
}

func TestDependencyGraphOptionalMissing(t *testing.T) {
	g, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "plug", Requires: []extensioncontract.Requirement{req("reasonix", "provider", "missing", ">=1.0.0", true)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Diagnostics) == 0 {
		t.Fatal("expected optional diagnostic")
	}
}

func TestDependencyGraphDuplicateProvider(t *testing.T) {
	_, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "a", Provides: []extensioncontract.Capability{cap("ns", "provider", "p", "1.0.0", "sha256:x")}},
		{ID: "b", Provides: []extensioncontract.Capability{cap("ns", "provider", "p", "1.0.0", "sha256:x")}},
		{ID: "c", Requires: []extensioncontract.Requirement{req("ns", "provider", "p", ">=1.0.0", false)}},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate_provider") {
		t.Fatalf("err = %v", err)
	}
}

func TestDependencyGraphRequiredCycle(t *testing.T) {
	_, err := BuildDependencyGraph([]ComponentDescriptor{
		{ID: "a", Requires: []extensioncontract.Requirement{req("ns", "x", "b", "", false)},
			Provides: []extensioncontract.Capability{cap("ns", "x", "a", "1.0.0", "")}},
		{ID: "b", Requires: []extensioncontract.Requirement{req("ns", "x", "a", "", false)},
			Provides: []extensioncontract.Capability{cap("ns", "x", "b", "1.0.0", "")}},
	})
	if err == nil {
		t.Fatal("cycle accepted")
	}
	var ge *GraphError
	if !errors.As(err, &ge) || ge.Reason != "dependency_cycle" || len(ge.Cycle) < 2 {
		t.Fatalf("err = %#v", err)
	}
}

func TestDependencyGraphDeterministicOrder(t *testing.T) {
	comps := []ComponentDescriptor{
		{ID: "z", Priority: 1, Source: ContributionSource{Scope: ScopePlugin}},
		{ID: "a", Priority: 1, Source: ContributionSource{Scope: ScopePlugin}},
		{ID: "m", Priority: 10, Source: ContributionSource{Scope: ScopePlugin}},
	}
	g1, err := BuildDependencyGraph(comps)
	if err != nil {
		t.Fatal(err)
	}
	g2, err := BuildDependencyGraph([]ComponentDescriptor{comps[2], comps[0], comps[1]})
	if err != nil {
		t.Fatal(err)
	}
	o1, o2 := g1.ActivateOrder(), g2.ActivateOrder()
	if strings.Join(idsToStrings(o1), ",") != strings.Join(idsToStrings(o2), ",") {
		t.Fatalf("order not deterministic: %v vs %v", o1, o2)
	}
	// Higher priority first among independent nodes.
	if o1[0] != "m" {
		t.Fatalf("priority sort failed: %v", o1)
	}
}

func idsToStrings(ids []ComponentID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}
