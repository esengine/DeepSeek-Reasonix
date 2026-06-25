package controlplane

import (
	"reflect"
	"testing"

	controlgraph "reasonix/internal/controlplane/control_graph"
	globalstate "reasonix/internal/equilibrium/global_state"
)

func TestDistributedControlWorksWithoutAnySingleController(t *testing.T) {
	st := controlgraph.SystemState{Stable: true, RecentSuccesses: 4}
	graph := DefaultGraph()
	for _, node := range graph.Nodes {
		decision := DecideWithGraph(st, WithoutNode(graph, node.ID()))
		if decision.Controller != "distributed-control-plane" {
			t.Fatalf("controller = %q, want distributed-control-plane", decision.Controller)
		}
		if decision.Action == "" || decision.Confidence <= 0 {
			t.Fatalf("decision without %s is invalid: %+v", node.ID(), decision)
		}
		if decision.ExplorationRatePercent < controlgraph.MinExplorationRatePercent || decision.ExplorationRatePercent > controlgraph.MaxExplorationRatePercent {
			t.Fatalf("decision without %s has invalid exploration rate: %+v", node.ID(), decision)
		}
	}
}

func TestDistributedControlDecisionIsDeterministic(t *testing.T) {
	st := controlgraph.SystemState{
		Stable:          true,
		RecentSuccesses: 5,
	}
	first := Decide(st)
	for i := 0; i < 8; i++ {
		if got := Decide(st); !reflect.DeepEqual(first, got) {
			t.Fatalf("distributed decision is not deterministic:\nfirst=%+v\ngot=%+v", first, got)
		}
	}
}

func TestDistributedControlResolvesStabilityExplorationConflict(t *testing.T) {
	st := controlgraph.SystemState{
		Stable:      true,
		Oscillating: true,
	}
	decision := Decide(st)
	if decision.Action != controlgraph.ActionDampen {
		t.Fatalf("stability/exploration conflict should dampen, got %+v", decision)
	}
	if decision.ExplorationRatePercent != controlgraph.MinExplorationRatePercent {
		t.Fatalf("damped decision should use exploration floor, got %+v", decision)
	}
}

func TestDistributedControlSemanticShiftFallsBackToStablePolicy(t *testing.T) {
	st := controlgraph.SystemState{
		Stable:        true,
		SemanticShift: []string{"soft semantic variations accumulated across recent turns: 3"},
	}
	decision := Decide(st)
	if decision.Action != controlgraph.ActionStabilize && decision.Action != controlgraph.ActionSafeMode {
		t.Fatalf("semantic shift should stabilize or safe-mode, got %+v", decision)
	}
	if decision.ExplorationRatePercent != controlgraph.MinExplorationRatePercent {
		t.Fatalf("semantic shift should suppress exploration, got %+v", decision)
	}
}

func TestDistributedControlConsensusStaysBounded(t *testing.T) {
	decision := Decide(controlgraph.SystemState{Stable: true, RecentSuccesses: 4})
	if decision.ConsensusScore <= 0 || decision.ConsensusScore > 1 {
		t.Fatalf("invalid consensus score: %+v", decision)
	}
	if decision.Variance > 0.12 {
		t.Fatalf("stable consensus variance too high: %+v", decision)
	}
}

func TestDistributedControlAppliesGlobalEquilibriumWindow(t *testing.T) {
	history := []globalstate.DecisionSample{
		{Action: controlgraph.ActionExplore, ExplorationRatePercent: 12, Gain: 1.1},
		{Action: controlgraph.ActionDampen, ExplorationRatePercent: 3, Gain: 0.6},
		{Action: controlgraph.ActionExplore, ExplorationRatePercent: 12, Gain: 1.1},
		{Action: controlgraph.ActionDampen, ExplorationRatePercent: 3, Gain: 0.6},
	}
	st := controlgraph.SystemState{Stable: true, RecentSuccesses: 4}
	base := DecideWithHistory(st, nil)
	decision := DecideWithHistory(st, history)
	if decision.Action != base.Action {
		t.Fatalf("global equilibrium must preserve control action: base=%+v got=%+v", base, decision)
	}
	if decision.EquilibriumState != "damping" {
		t.Fatalf("equilibrium state = %q, want damping: %+v", decision.EquilibriumState, decision)
	}
	if decision.Gain >= base.Gain && base.Gain > 0 {
		t.Fatalf("global equilibrium should damp gain without overriding action: base=%+v got=%+v", base, decision)
	}
	if decision.OscillationIndex < 0.7 {
		t.Fatalf("oscillation index = %.3f, want high", decision.OscillationIndex)
	}
}

func TestCollectSignalsCapsActiveNodes(t *testing.T) {
	var nodes []controlgraph.ControlNode
	for i := 0; i < 20; i++ {
		nodes = append(nodes, testControlNode{
			id:          string(rune('a' + i)),
			weight:      1 + float64(i)/100,
			reliability: 0.9,
			action:      controlgraph.ActionBalanced,
		})
	}
	signals := CollectSignals(controlgraph.ControlGraph{Nodes: nodes}, controlgraph.SystemState{})
	if len(signals) != maxActiveControlNodes {
		t.Fatalf("signals = %d, want cap %d", len(signals), maxActiveControlNodes)
	}
}

type testControlNode struct {
	id          string
	weight      float64
	reliability float64
	action      controlgraph.Action
}

func (n testControlNode) ID() string                  { return n.id }
func (n testControlNode) Type() controlgraph.NodeType { return controlgraph.NodeStability }
func (n testControlNode) Weight() float64             { return n.weight }
func (n testControlNode) Reliability() float64        { return n.reliability }
func (n testControlNode) Signal(controlgraph.SystemState) controlgraph.ControlSignal {
	return controlgraph.ControlSignal{
		NodeID:                 n.id,
		Type:                   n.Type(),
		Action:                 n.action,
		Strength:               0.5,
		Confidence:             0.8,
		Weight:                 n.weight,
		Reliability:            n.reliability,
		ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
		Gain:                   1,
		Reason:                 "test node",
	}
}
