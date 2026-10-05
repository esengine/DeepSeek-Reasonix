package agentgraph

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestApplyUpsertsNodesByID(t *testing.T) {
	var g Graph
	g.Apply(Delta{Nodes: []Node{
		{ID: "a", Kind: KindWorker, State: StatePending, Label: "research", Profile: "explore", Grant: GrantRead},
	}})
	// An outcome-only update is what a producer sends once the node settles.
	g.Apply(Delta{Nodes: []Node{{ID: "a", State: StateCompleted, Ref: "sub-1", EndedAt: 42}}})

	if len(g.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 (the update forked the node)", len(g.Nodes))
	}
	n, ok := g.Node("a")
	if !ok {
		t.Fatal("node a missing")
	}
	if n.State != StateCompleted || n.Ref != "sub-1" || n.EndedAt != 42 {
		t.Fatalf("update did not land: %+v", n)
	}
	if n.Label != "research" || n.Profile != "explore" || n.Kind != KindWorker || n.Grant != GrantRead {
		t.Fatalf("outcome-only update blanked declared identity: %+v", n)
	}
}

func TestApplyIgnoresUnusableRecords(t *testing.T) {
	cases := []struct {
		name  string
		delta Delta
	}{
		{"node without id", Delta{Nodes: []Node{{State: StateRunning}}}},
		{"edge without source", Delta{Edges: []Edge{{To: "b", Kind: Depends}}}},
		{"edge without target", Delta{Edges: []Edge{{From: "a", Kind: Depends}}}},
		{"edge without kind", Delta{Edges: []Edge{{From: "a", To: "b"}}}},
		{"self edge", Delta{Edges: []Edge{{From: "a", To: "a", Kind: Depends}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var g Graph
			g.Apply(c.delta)
			if len(g.Nodes) != 0 || len(g.Edges) != 0 {
				t.Fatalf("recorded %+v", g)
			}
		})
	}
}

func TestApplyKeepsEdgesDistinctByKind(t *testing.T) {
	var g Graph
	// depends and context join the same pair and are not the same fact: an arm
	// that orders without delivering emits the first and not the second.
	edges := []Edge{
		{From: "a", To: "b", Kind: Depends},
		{From: "a", To: "b", Kind: Context},
		{From: "a", To: "b", Kind: Depends},
	}
	g.Apply(Delta{Edges: edges})
	if len(g.Edges) != 2 {
		t.Fatalf("edges = %v, want depends and context once each", g.Edges)
	}
	for _, want := range []Edge{{From: "a", To: "b", Kind: Depends}, {From: "a", To: "b", Kind: Context}} {
		if !slices.Contains(g.Edges, want) {
			t.Fatalf("missing %+v in %v", want, g.Edges)
		}
	}
}

func TestApplyIsOrderStable(t *testing.T) {
	build := func() Graph {
		var g Graph
		for _, id := range []string{"a", "b", "c"} {
			g.Apply(Delta{Nodes: []Node{{ID: id, Kind: KindWorker, State: StatePending}}})
		}
		g.Apply(Delta{Nodes: []Node{{ID: "b", State: StateRunning}}})
		return g
	}
	first, second := build(), build()
	if len(first.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(first.Nodes))
	}
	for i := range first.Nodes {
		if first.Nodes[i] != second.Nodes[i] {
			t.Fatalf("node %d diverged: %+v vs %+v", i, first.Nodes[i], second.Nodes[i])
		}
	}
	if first.Nodes[1].ID != "b" || first.Nodes[1].State != StateRunning {
		t.Fatalf("an update reordered the graph: %+v", first.Nodes)
	}
}

func TestStateVocabulary(t *testing.T) {
	cases := []struct {
		state    NodeState
		answered bool
		terminal bool
	}{
		{StatePending, false, false},
		// Queued is not terminal: the node has not run, and a reader that treats
		// it as settled stops waiting for the answer it is still going to get.
		{StateQueued, false, false},
		{StateRunning, false, false},
		{StateCompleted, true, true},
		{StateAdopted, true, true},
		{StateFailed, false, true},
		{StateCancelled, false, true},
		{StateSkipped, false, true},
	}
	for _, c := range cases {
		if got := c.state.Answered(); got != c.answered {
			t.Errorf("%s.Answered() = %v, want %v", c.state, got, c.answered)
		}
		if got := c.state.Terminal(); got != c.terminal {
			t.Errorf("%s.Terminal() = %v, want %v", c.state, got, c.terminal)
		}
	}
}

// A graph crosses a wire before it is drawn, so the folded value has to survive
// the round trip that every consumer outside this process reads it through.
func TestGraphSurvivesJSONRoundTrip(t *testing.T) {
	var g Graph
	g.Apply(Delta{
		Nodes: []Node{
			{ID: "grp", Kind: KindGroup, State: StateRunning, Label: "fleet(2)"},
			{ID: "grp/1", ParentID: "grp", Kind: KindWorker, State: StateCompleted, Model: "m", StartedAt: 1, EndedAt: 2},
			{ID: "old-ref", Kind: KindExternal, State: StateCompleted},
		},
		Edges: []Edge{
			{From: "grp", To: "grp/1", Kind: Spawn},
			{From: "old-ref", To: "grp/1", Kind: Adopt},
		},
	})
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var back Graph
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(back.Edges, g.Edges) {
		t.Fatalf("edges = %v, want %v", back.Edges, g.Edges)
	}
	if !slices.Equal(back.Nodes, g.Nodes) {
		t.Fatalf("nodes = %v, want %v", back.Nodes, g.Nodes)
	}
}
