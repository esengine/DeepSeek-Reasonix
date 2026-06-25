package arbitration

import (
	"math"

	"reasonix/internal/controlplane/consensus"
	controlgraph "reasonix/internal/controlplane/control_graph"
)

func resolveConflicts(decision controlgraph.ControlDecision, result consensus.Result, st controlgraph.SystemState) controlgraph.ControlDecision {
	stabilityScore := result.Scores[controlgraph.ActionStabilize] + result.Scores[controlgraph.ActionSafeMode]
	explorationScore := result.Scores[controlgraph.ActionExplore]
	mutationDampingScore := result.Scores[controlgraph.ActionDampen]
	total := math.Max(result.TotalWeight, 1)
	stabilityShare := stabilityScore / total
	explorationShare := explorationScore / total
	dampingShare := mutationDampingScore / total

	if st.Oscillating {
		decision.Action = controlgraph.ActionDampen
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = math.Min(decision.Gain, 0.75)
		decision.Reasons = append(decision.Reasons, "oscillation signal triggered distributed damping")
		return decision
	}

	if result.Variance > highVarianceThreshold {
		if st.HasDrift || st.Unstable || len(st.SemanticShift) > 0 {
			decision = safeDecision(st, "high control variance fell back to safe mode")
			decision.Variance = round(result.Variance)
			return decision
		}
		if st.Stable {
			decision.Action = controlgraph.ActionExplore
			decision.ExplorationRatePercent = controlgraph.MaxExplorationRatePercent
			decision.Gain = 1.05
			decision.Reasons = append(decision.Reasons, "high variance under stable state triggered controlled exploration")
			return decision
		}
		decision.Action = controlgraph.ActionDampen
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = 0.75
		decision.Reasons = append(decision.Reasons, "high control variance triggered damping")
		return decision
	}

	if stabilityShare > 0.24 && explorationShare > 0.20 {
		if len(st.SemanticShift) > 0 {
			decision.Action = controlgraph.ActionStabilize
			decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
			decision.Gain = math.Min(decision.Gain, 0.62)
			decision.Reasons = append(decision.Reasons, "semantic signal resolved stability versus exploration conflict")
			return decision
		}
		decision.Action = controlgraph.ActionDampen
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = math.Min(decision.Gain, 0.82)
		decision.Reasons = append(decision.Reasons, "stability versus exploration conflict resolved by damped blend")
		return decision
	}

	if stabilityShare > 0.28 && dampingShare > 0.18 {
		decision.Action = controlgraph.ActionStabilize
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = math.Min(decision.Gain, 0.70)
		decision.Reasons = append(decision.Reasons, "mutation versus stability conflict prioritized stability")
		return decision
	}

	switch decision.Action {
	case controlgraph.ActionSafeMode:
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = math.Min(decision.Gain, 0.50)
		decision.SafeMode = true
	case controlgraph.ActionStabilize:
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = math.Min(decision.Gain, 0.72)
	case controlgraph.ActionDampen:
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = math.Min(decision.Gain, 0.82)
	case controlgraph.ActionExplore:
		decision.ExplorationRatePercent = controlgraph.MaxExplorationRatePercent
		decision.Gain = math.Max(decision.Gain, 1.05)
	default:
		decision.Action = controlgraph.ActionBalanced
		decision.ExplorationRatePercent = controlgraph.DefaultExplorationRatePercent
		if decision.Gain <= 0 {
			decision.Gain = 1
		}
	}
	return decision
}
