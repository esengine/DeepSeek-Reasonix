package anticentralization

import (
	"math"

	controlgraph "reasonix/internal/controlplane/control_graph"
	equilibriumpolicy "reasonix/internal/equilibrium/equilibrium_policy"
	globalstate "reasonix/internal/equilibrium/global_state"
)

const MaxNodeDominanceRatio = 0.55

func Apply(decision controlgraph.ControlDecision, policy globalstate.EquilibriumPolicy, st globalstate.GlobalEquilibriumState) (controlgraph.ControlDecision, []string) {
	var adjustments []string
	if dominantShare(decision.NodeInfluence) > MaxNodeDominanceRatio {
		decision.Confidence = controlgraph.Clamp01(decision.Confidence * 0.92)
		decision.Gain = math.Min(decision.Gain, 1.0)
		if decision.Action != controlgraph.ActionSafeMode && decision.Action != controlgraph.ActionStabilize {
			decision.ExplorationRatePercent = controlgraph.ClampRate(maxInt(decision.ExplorationRatePercent, controlgraph.DefaultExplorationRatePercent))
		}
		adjustments = append(adjustments, "node dominance capped")
	}
	if st.ControlGraphEntropy < equilibriumpolicy.EntropyFloor {
		decision.Confidence = controlgraph.Clamp01(decision.Confidence * 0.94)
		if decision.Action != controlgraph.ActionSafeMode && decision.Action != controlgraph.ActionStabilize {
			decision.ExplorationRatePercent = controlgraph.ClampRate(maxInt(decision.ExplorationRatePercent, policy.ExplorationRatePercent))
		}
		adjustments = append(adjustments, "entropy floor enforced")
	}
	return decision, adjustments
}

func dominantShare(influence []controlgraph.NodeInfluence) float64 {
	maxShare := 0.0
	for _, item := range influence {
		if item.Share > maxShare {
			maxShare = item.Share
		}
	}
	return maxShare
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
