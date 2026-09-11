package boot

import (
	"testing"

	"reasonix/internal/extension"
	"reasonix/internal/extensioncontract"
)

func TestRebuildFromGraphUsesPreviousAsFrom(t *testing.T) {
	// Synthetic: from has plugin/a, to is empty → must not classify as no-op.
	from, err := extension.BuildDependencyGraph([]extension.ComponentDescriptor{
		{ID: "plugin/a", Provides: []extensioncontract.Capability{{
			Key:     extensioncontract.CapabilityKey{Namespace: "plugin/a", Kind: "ui", ID: "x"},
			Version: "1.0.0", SchemaHash: "sha256:a",
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Empty to graph.
	to := &extension.DependencyGraph{
		Components: map[extension.ComponentID]extension.ComponentDescriptor{},
		Edges:      map[extension.ComponentID][]extension.ComponentID{},
		Providers:  map[string][]extension.ComponentID{},
	}
	plan := extension.DiffRuntimePlan(from, to, 1, 2)
	if plan.IsNoOp() {
		t.Fatal("removing a component must not be a no-op plan")
	}
	if len(plan.Removed) == 0 {
		t.Fatalf("expected removed, plan=%+v", plan)
	}
	// Diff against self is no-op (the bug we fixed was using current for both).
	self := extension.DiffRuntimePlan(from, from, 1, 2)
	if !self.IsNoOp() {
		t.Fatal("same graph should be no-op")
	}
}
