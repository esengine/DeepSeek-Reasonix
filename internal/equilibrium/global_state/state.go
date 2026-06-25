package globalstate

import (
	controlgraph "reasonix/internal/controlplane/control_graph"
	controltypes "reasonix/internal/controlsemantics/types"
)

type DecisionSample struct {
	Action                 controlgraph.Action
	ExplorationRatePercent int
	Gain                   float64
	ConsensusScore         float64
	Variance               float64
	ControlGraphEntropy    float64
	SystemStabilityScore   float64
	ConvergenceVelocity    float64
	OscillationIndex       float64
	NodeInfluence          []controlgraph.NodeInfluence
}

type GlobalEquilibriumState struct {
	ControlGraphEntropy  float64
	SystemStabilityScore float64
	ConvergenceVelocity  float64
	OscillationIndex     float64
	WindowSize           int
}

type OscillationReport struct {
	Severity      string
	AffectedNodes []string
	Frequency     float64
}

type EquilibriumPolicy struct {
	ExplorationRatePercent int
	DampingFactor          float64
	ConsensusThreshold     float64
	Actions                []string
}

type EquilibriumTrace struct {
	State             GlobalEquilibriumState
	Policy            EquilibriumPolicy
	OscillationReport OscillationReport
	SemanticSignals   []controltypes.TypedSignal
	Adjustments       []string
}
