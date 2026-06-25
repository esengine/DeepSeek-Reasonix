package controllers

import controlgraph "reasonix/internal/controlplane/control_graph"

type node struct {
	id          string
	nodeType    controlgraph.NodeType
	weight      float64
	reliability float64
	signal      func(controlgraph.SystemState) controlgraph.ControlSignal
}

func (n node) ID() string                  { return n.id }
func (n node) Type() controlgraph.NodeType { return n.nodeType }
func (n node) Weight() float64             { return n.weight }
func (n node) Reliability() float64        { return n.reliability }
func (n node) Signal(st controlgraph.SystemState) controlgraph.ControlSignal {
	sig := n.signal(st)
	sig.NodeID = n.id
	sig.Type = n.nodeType
	sig.Weight = n.weight
	sig.Reliability = n.reliability
	sig.Strength = controlgraph.Clamp01(sig.Strength)
	sig.Confidence = controlgraph.Clamp01(sig.Confidence)
	sig.ExplorationRatePercent = controlgraph.ClampRate(sig.ExplorationRatePercent)
	if sig.Gain <= 0 {
		sig.Gain = 1
	}
	return sig
}

func BuiltInNodes() []controlgraph.ControlNode {
	return []controlgraph.ControlNode{
		StabilityNode(),
		ExplorationNode(),
		MutationNode(),
		SemanticDriftNode(),
	}
}

func StabilityNode() controlgraph.ControlNode {
	return node{
		id:          "stability-controller",
		nodeType:    controlgraph.NodeStability,
		weight:      1.0,
		reliability: 0.92,
		signal: func(st controlgraph.SystemState) controlgraph.ControlSignal {
			if st.HasDrift || st.Unstable || st.RecentFailures > 0 || st.MemoryNoisePatterns > 0 {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionStabilize,
					Strength:               0.86,
					Confidence:             0.90,
					ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
					Gain:                   0.70,
					Reason:                 "stability controller observed drift, failure, or memory noise",
				}
			}
			if st.Stable {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionBalanced,
					Strength:               0.42,
					Confidence:             0.78,
					ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
					Gain:                   1.05,
					Reason:                 "stability controller observed stable recent outcomes",
				}
			}
			return controlgraph.ControlSignal{
				Action:                 controlgraph.ActionBalanced,
				Strength:               0.50,
				Confidence:             0.70,
				ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
				Gain:                   1.0,
				Reason:                 "stability controller found no active instability",
			}
		},
	}
}

func ExplorationNode() controlgraph.ControlNode {
	return node{
		id:          "exploration-controller",
		nodeType:    controlgraph.NodeExploration,
		weight:      0.92,
		reliability: 0.86,
		signal: func(st controlgraph.SystemState) controlgraph.ControlSignal {
			if st.Oscillating {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionDampen,
					Strength:               0.82,
					Confidence:             0.88,
					ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
					Gain:                   0.65,
					Reason:                 "exploration controller damped oscillating strategy rotation",
				}
			}
			if st.Stable && len(st.SemanticShift) == 0 {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionExplore,
					Strength:               0.74,
					Confidence:             0.84,
					ExplorationRatePercent: controlgraph.MaxExplorationRatePercent,
					Gain:                   1.15,
					Reason:                 "exploration controller allowed bounded exploration under stable state",
				}
			}
			return controlgraph.ControlSignal{
				Action:                 controlgraph.ActionBalanced,
				Strength:               0.42,
				Confidence:             0.70,
				ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
				Gain:                   1.0,
				Reason:                 "exploration controller kept default entropy",
			}
		},
	}
}

func MutationNode() controlgraph.ControlNode {
	return node{
		id:          "mutation-controller",
		nodeType:    controlgraph.NodeMutation,
		weight:      0.88,
		reliability: 0.84,
		signal: func(st controlgraph.SystemState) controlgraph.ControlSignal {
			if st.MutationPressure > 1 || st.CompilerImprovements > 1 || st.Unstable {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionDampen,
					Strength:               0.78,
					Confidence:             0.82,
					ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
					Gain:                   0.62,
					Reason:                 "mutation controller reduced plasticity under mutation pressure",
				}
			}
			if st.Stable && st.RecentSuccesses >= 3 {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionExplore,
					Strength:               0.46,
					Confidence:             0.72,
					ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
					Gain:                   1.12,
					Reason:                 "mutation controller allowed moderate learning plasticity",
				}
			}
			return controlgraph.ControlSignal{
				Action:                 controlgraph.ActionBalanced,
				Strength:               0.45,
				Confidence:             0.68,
				ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
				Gain:                   1.0,
				Reason:                 "mutation controller held neutral mutation cadence",
			}
		},
	}
}

func SemanticDriftNode() controlgraph.ControlNode {
	return node{
		id:          "semantic-drift-controller",
		nodeType:    controlgraph.NodeSemanticDrift,
		weight:      1.0,
		reliability: 0.90,
		signal: func(st controlgraph.SystemState) controlgraph.ControlSignal {
			if len(st.SemanticShift) > 0 || st.RecentHardDrifts > 1 {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionStabilize,
					Strength:               0.92,
					Confidence:             0.91,
					ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
					Gain:                   0.48,
					Reason:                 "semantic drift controller detected accumulated meaning shift",
				}
			}
			if st.RecentSoftDrifts > 0 {
				return controlgraph.ControlSignal{
					Action:                 controlgraph.ActionDampen,
					Strength:               0.58,
					Confidence:             0.78,
					ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
					Gain:                   0.78,
					Reason:                 "semantic drift controller observed bounded soft variation",
				}
			}
			return controlgraph.ControlSignal{
				Action:                 controlgraph.ActionBalanced,
				Strength:               0.38,
				Confidence:             0.68,
				ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
				Gain:                   1.0,
				Reason:                 "semantic drift controller found no semantic shift",
			}
		},
	}
}
