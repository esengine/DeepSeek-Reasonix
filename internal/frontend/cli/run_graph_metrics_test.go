package cli

import (
	"testing"

	"reasonix/internal/contract/agentgraph"
	"reasonix/internal/contract/event"
)

func graphEvent(nodes []agentgraph.Node, edges []agentgraph.Edge) event.Event {
	return event.Event{Kind: event.GraphDelta, Graph: &agentgraph.Delta{Nodes: nodes, Edges: edges}}
}

// The scalars exist to answer one question a counter cannot: whether the shape
// bought anything. A fan-out that ran three items in the wall clock of two is
// only visible as work against wall, and only the graph carries both.
func TestFanoutMetricsPriceTheParallelismTheGraphBought(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	group := "t1"
	s.Emit(graphEvent([]agentgraph.Node{
		{ID: group, Kind: agentgraph.KindGroup, State: agentgraph.StateRunning, StartedAt: 1000},
		{ID: "t1/fleet-1", ParentID: group, Kind: agentgraph.KindWorker, QueuedAt: 1000, StartedAt: 1100},
		{ID: "t1/fleet-2", ParentID: group, Kind: agentgraph.KindWorker},
		{ID: "t1/fleet-3", ParentID: group, Kind: agentgraph.KindWorker, QueuedAt: 1000, StartedAt: 1100},
		{ID: "t1/fleet-4", ParentID: group, Kind: agentgraph.KindWorker, State: agentgraph.StateAdopted},
	}, []agentgraph.Edge{
		{From: "t1/fleet-1", To: "t1/fleet-2", Kind: agentgraph.Depends},
	}))
	s.Emit(graphEvent([]agentgraph.Node{
		{ID: "t1/fleet-1", State: agentgraph.StateCompleted, EndedAt: 1500},
		{ID: "t1/fleet-3", State: agentgraph.StateCompleted, EndedAt: 1400},
		{ID: "t1/fleet-2", State: agentgraph.StateCompleted, QueuedAt: 1500, StartedAt: 1600, EndedAt: 1900},
		{ID: group, State: agentgraph.StateCompleted, EndedAt: 1900},
	}, nil))

	got := s.Snapshot().Fanout
	if got == nil {
		t.Fatal("a run that fanned out priced nothing")
	}
	want := FanoutMetrics{
		Groups: 1, Workers: 4, Adopted: 1,
		// 400 + 300 ran side by side inside the group's 900, and the ordered
		// pair 400 → 300 is the floor no scheduler could have gone below.
		WallMs: 900, WorkMs: 1000, CriticalPathMs: 700, SlotWaitMs: 300,
	}
	if *got != want {
		t.Fatalf("fan-out priced %+v, want %+v", *got, want)
	}
}

// A single-agent arm must report nothing here. Zeros would be read as a fan-out
// that bought nothing, which is a different finding from one that never ran.
func TestFanoutMetricsAreAbsentWhenNothingFannedOut(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "t1", Name: "bash"}})
	if got := s.Snapshot().Fanout; got != nil {
		t.Fatalf("a run with no fan-out priced %+v, want nothing", *got)
	}
}

// A killed run leaves members that started and never ended. Their time is not
// zero, it is unknown — and the two must not be added together.
func TestFanoutMetricsRefuseToPriceWorkNoStampCovers(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(graphEvent([]agentgraph.Node{
		{ID: "t1", Kind: agentgraph.KindGroup, StartedAt: 1000},
		{ID: "t1/fleet-1", ParentID: "t1", Kind: agentgraph.KindWorker, StartedAt: 1100},
	}, nil))

	got := s.Snapshot().Fanout
	if got == nil {
		t.Fatal("a run that fanned out priced nothing")
	}
	if got.WorkMs != 0 || got.WallMs != 0 || got.CriticalPathMs != 0 {
		t.Fatalf("unfinished work was priced: %+v", *got)
	}
	if got.Groups != 1 || got.Workers != 1 {
		t.Fatalf("counted %d groups / %d workers, want the one that ran", got.Groups, got.Workers)
	}
}

// One figure counted every wait between ready and running, and its name said
// the concurrency ceiling caused it. A member kept out by a write path someone
// else holds waits the same way and no ceiling releases it, so the number that
// drives "raise max_subagent_concurrency" was quoting waits that would not move.
func TestClaimWaitIsNotPricedAsCeilingWait(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	group := "t1"
	s.Emit(graphEvent([]agentgraph.Node{
		{ID: group, Kind: agentgraph.KindGroup, State: agentgraph.StateRunning, StartedAt: 1000},
		{ID: "t1/fleet-1", ParentID: group, Kind: agentgraph.KindWorker,
			Wait: agentgraph.WaitSlots, QueuedAt: 1000, StartedAt: 1100},
		{ID: "t1/fleet-2", ParentID: group, Kind: agentgraph.KindWorker,
			Wait: agentgraph.WaitClaim, QueuedAt: 1000, StartedAt: 1500},
	}, nil))
	s.Emit(graphEvent([]agentgraph.Node{
		{ID: "t1/fleet-1", State: agentgraph.StateCompleted, EndedAt: 1400},
		{ID: "t1/fleet-2", State: agentgraph.StateCompleted, EndedAt: 1900},
		{ID: group, State: agentgraph.StateCompleted, EndedAt: 1900},
	}, nil))

	got := s.Snapshot().Fanout
	if got == nil {
		t.Fatal("a run that fanned out priced nothing")
	}
	if got.SlotWaitMs != 100 {
		t.Errorf("ceiling wait = %d, want 100 — only the member a ceiling held", got.SlotWaitMs)
	}
	if got.ClaimWaitMs != 500 {
		t.Errorf("claim wait = %d, want 500 — the member a held path kept out", got.ClaimWaitMs)
	}
}
