package controlplane

import (
	"sort"

	"reasonix/internal/controlplane/arbitration"
	controlgraph "reasonix/internal/controlplane/control_graph"
	"reasonix/internal/controlplane/controllers"
	policynodes "reasonix/internal/controlplane/policy_nodes"
	semanticrouter "reasonix/internal/controlsemantics/semantic_router"
	controltypes "reasonix/internal/controlsemantics/types"
	"reasonix/internal/equilibrium/convergence"
	globalstate "reasonix/internal/equilibrium/global_state"
)

const maxActiveControlNodes = 8

func DefaultGraph() controlgraph.ControlGraph {
	return controllers.DefaultGraph()
}

func Decide(st controlgraph.SystemState) controlgraph.ControlDecision {
	return DecideWithGraph(st, DefaultGraph())
}

func DecideWithGraph(st controlgraph.SystemState, graph controlgraph.ControlGraph) controlgraph.ControlDecision {
	return DecideWithGraphAndHistory(st, graph, nil)
}

func DecideWithHistory(st controlgraph.SystemState, history []globalstate.DecisionSample) controlgraph.ControlDecision {
	return DecideWithGraphAndHistory(st, DefaultGraph(), history)
}

func DecideWithGraphAndHistory(st controlgraph.SystemState, graph controlgraph.ControlGraph, history []globalstate.DecisionSample) controlgraph.ControlDecision {
	graph = policynodes.LearnControlGraph(graph, st)
	graph = policynodes.ApplyDynamicWeights(graph, st)
	signals := CollectSignals(graph, st)
	decision := arbitration.Arbitrate(graph, st, signals)
	if _, err := semanticrouter.Route(controltypes.NewSignal(controltypes.SignalDecision, controltypes.LayerControl, decision.Action, "control plane arbitration")); err != nil {
		return controlgraph.ControlDecision{
			Action:                 controlgraph.ActionSafeMode,
			Confidence:             1,
			ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
			Gain:                   0.5,
			Controller:             "distributed-control-plane",
			Reasons:                []string{err.Error()},
			SafeMode:               true,
		}
	}
	decision, _ = convergence.FilterDecision(decision, history)
	return decision
}

func CollectSignals(graph controlgraph.ControlGraph, st controlgraph.SystemState) []controlgraph.ControlSignal {
	nodes := activeNodes(graph.Nodes)
	signals := make([]controlgraph.ControlSignal, 0, len(nodes))
	for _, node := range nodes {
		signals = append(signals, node.Signal(st))
	}
	return signals
}

func activeNodes(nodes []controlgraph.ControlNode) []controlgraph.ControlNode {
	out := append([]controlgraph.ControlNode(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i].Weight() * out[i].Reliability()
		right := out[j].Weight() * out[j].Reliability()
		if left == right {
			return out[i].ID() < out[j].ID()
		}
		return left > right
	})
	if len(out) > maxActiveControlNodes {
		out = out[:maxActiveControlNodes]
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ID() < out[j].ID()
	})
	return out
}

func WithoutNode(graph controlgraph.ControlGraph, id string) controlgraph.ControlGraph {
	return controlgraph.WithoutNode(graph, id)
}
