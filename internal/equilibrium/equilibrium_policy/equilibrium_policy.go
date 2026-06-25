package equilibriumpolicy

import (
	controlgraph "reasonix/internal/controlplane/control_graph"
	globalstate "reasonix/internal/equilibrium/global_state"
)

const (
	EntropyFloor                 = 0.55
	HighOscillationThreshold     = 0.70
	LowConvergenceVelocity       = 0.04
	StableConvergenceThreshold   = 0.78
	DefaultConsensusThreshold    = 0.50
	StabilizedConsensusThreshold = 0.68
)

func ForState(st globalstate.GlobalEquilibriumState, report globalstate.OscillationReport) globalstate.EquilibriumPolicy {
	policy := globalstate.EquilibriumPolicy{
		ExplorationRatePercent: controlgraph.DefaultExplorationRatePercent,
		DampingFactor:          1,
		ConsensusThreshold:     DefaultConsensusThreshold,
		Actions:                []string{"maintain global equilibrium"},
	}
	if report.Severity == "high" || st.OscillationIndex >= HighOscillationThreshold {
		policy.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		policy.DampingFactor = 0.55
		policy.ConsensusThreshold = StabilizedConsensusThreshold
		policy.Actions = []string{"apply global damping constraint"}
		return policy
	}
	if st.ControlGraphEntropy < EntropyFloor {
		policy.ExplorationRatePercent = controlgraph.MaxExplorationRatePercent
		policy.DampingFactor = 0.82
		policy.ConsensusThreshold = StabilizedConsensusThreshold
		policy.Actions = []string{"enforce entropy floor weight"}
		return policy
	}
	if st.WindowSize >= 4 && st.ConvergenceVelocity < LowConvergenceVelocity && st.SystemStabilityScore >= StableConvergenceThreshold && st.OscillationIndex < 0.25 {
		policy.ExplorationRatePercent = controlgraph.MaxExplorationRatePercent
		policy.DampingFactor = 1.05
		policy.ConsensusThreshold = DefaultConsensusThreshold
		policy.Actions = []string{"increase exploration weight"}
		return policy
	}
	if report.Severity == "medium" || st.OscillationIndex >= 0.45 {
		policy.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		policy.DampingFactor = 0.72
		policy.ConsensusThreshold = StabilizedConsensusThreshold
		policy.Actions = []string{"apply emerging oscillation constraint"}
	}
	return policy
}
