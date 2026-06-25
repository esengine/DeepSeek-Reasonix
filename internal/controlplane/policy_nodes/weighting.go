package policynodes

import (
	"math"

	controlgraph "reasonix/internal/controlplane/control_graph"
)

type weightedNode struct {
	base        controlgraph.ControlNode
	weight      float64
	reliability float64
}

func (n weightedNode) ID() string                  { return n.base.ID() }
func (n weightedNode) Type() controlgraph.NodeType { return n.base.Type() }
func (n weightedNode) Weight() float64             { return n.weight }
func (n weightedNode) Reliability() float64        { return n.reliability }
func (n weightedNode) Signal(st controlgraph.SystemState) controlgraph.ControlSignal {
	sig := n.base.Signal(st)
	sig.Weight = n.weight
	sig.Reliability = n.reliability
	return sig
}

func ApplyDynamicWeights(graph controlgraph.ControlGraph, st controlgraph.SystemState) controlgraph.ControlGraph {
	out := controlgraph.CloneGraph(graph)
	out.Nodes = make([]controlgraph.ControlNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		weight, reliability := dynamicNodeWeight(node, st)
		out.Nodes = append(out.Nodes, weightedNode{base: node, weight: weight, reliability: reliability})
	}
	return out
}

func LearnControlGraph(graph controlgraph.ControlGraph, st controlgraph.SystemState) controlgraph.ControlGraph {
	out := controlgraph.CloneGraph(graph)
	for i, edge := range out.Edges {
		switch {
		case st.Oscillating && edge.From == "stability-controller" && edge.To == "exploration-controller":
			out.Edges[i].Influence = controlgraph.InfluenceSuppress
		case len(st.SemanticShift) > 0 && edge.From == "semantic-drift-controller" && edge.To == "exploration-controller":
			out.Edges[i].Influence = controlgraph.InfluenceSuppress
		case st.Stable && edge.From == "exploration-controller" && edge.To == "mutation-controller":
			out.Edges[i].Influence = controlgraph.InfluenceAmplify
		case st.Unstable && edge.From == "mutation-controller" && edge.To == "stability-controller":
			out.Edges[i].Influence = controlgraph.InfluenceBalance
		}
	}
	return out
}

func dynamicNodeWeight(node controlgraph.ControlNode, st controlgraph.SystemState) (float64, float64) {
	weight := node.Weight()
	reliability := node.Reliability()
	successRate := recentSuccessRate(st)
	effectiveness := 0.85 + successRate*0.30
	stabilityFactor := 1.0
	if st.Stable {
		stabilityFactor = 1.08
	}
	if st.Unstable || st.HasDrift {
		stabilityFactor = 0.92
	}
	switch node.Type() {
	case controlgraph.NodeStability:
		if st.Unstable || st.HasDrift {
			weight *= 1.22
			reliability += 0.04
		}
	case controlgraph.NodeExploration:
		if st.Stable && len(st.SemanticShift) == 0 {
			weight *= 1.12
		}
		if st.Oscillating || len(st.SemanticShift) > 0 {
			weight *= 0.82
		}
	case controlgraph.NodeMutation:
		if st.MutationPressure > 0 || st.CompilerImprovements > 0 {
			weight *= 1.10
		}
		if st.Unstable {
			weight *= 0.90
		}
	case controlgraph.NodeSemanticDrift:
		if len(st.SemanticShift) > 0 || st.RecentHardDrifts > 0 {
			weight *= 1.25
			reliability += 0.05
		}
	}
	decay := math.Exp(-float64(st.RecentFailures+st.RecentHardDrifts) / 18)
	weight = clamp(weight*effectiveness*stabilityFactor*decay, 0.35, 1.35)
	reliability = clamp(reliability*decay, 0.35, 0.98)
	return weight, reliability
}

func recentSuccessRate(st controlgraph.SystemState) float64 {
	total := st.RecentSuccesses + st.RecentFailures
	if total <= 0 {
		return 0.5
	}
	return float64(st.RecentSuccesses) / float64(total)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
