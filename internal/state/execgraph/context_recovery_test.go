package execgraph

import (
	"reasonix/internal/contract/agentgraph"
	"reasonix/internal/state/execjournal"
	"slices"
	"testing"
	"time"
)

func TestRecoveryUsesSuppliedContextAndRestoresGroupOutcome(t *testing.T) {
	stamp := time.Now()
	entries := []execjournal.Entry{
		{ID: "first", Group: "fleet", Kind: "worker", StartedAt: stamp, SettledAt: stamp},
		{ID: "second", Group: "fleet", Kind: "worker", StartedAt: stamp, SettledAt: stamp, DependsOn: []string{"first"}},
	}
	children := []ChildOutcome{{Execution: "first", Ref: "sa_first", Status: ChildCompleted, ContextKnown: true}, {Execution: "second", Ref: "sa_second", Status: ChildCompleted, ContextKnown: true}}
	g := Rebuild(entries, children, nil).Graph
	if slices.Contains(g.Edges, agentgraph.Edge{From: "first", To: "second", Kind: agentgraph.Context}) {
		t.Fatal("invented context delivery from dependency order")
	}
	group, ok := g.Node("fleet")
	if !ok || group.State != agentgraph.StateCompleted {
		t.Fatalf("group = %+v", group)
	}
	children[1].ContextRefs = []string{"sa_first", "sa_external"}
	g = Rebuild(entries, children, nil).Graph
	for _, source := range []string{"first", "sa_external"} {
		if !slices.Contains(g.Edges, agentgraph.Edge{From: source, To: "second", Kind: agentgraph.Context}) {
			t.Fatalf("missing context from %s", source)
		}
	}
}
