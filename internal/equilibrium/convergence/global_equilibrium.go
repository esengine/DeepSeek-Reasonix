package convergence

import (
	"math"

	controlgraph "reasonix/internal/controlplane/control_graph"
	semanticrouter "reasonix/internal/controlsemantics/semantic_router"
	controltypes "reasonix/internal/controlsemantics/types"
	anticentralization "reasonix/internal/equilibrium/anti_centralization"
	equilibriumpolicy "reasonix/internal/equilibrium/equilibrium_policy"
	globalstate "reasonix/internal/equilibrium/global_state"
	stabilitywindow "reasonix/internal/equilibrium/stability_window"
)

func FilterDecision(decision controlgraph.ControlDecision, history []globalstate.DecisionSample) (controlgraph.ControlDecision, globalstate.EquilibriumTrace) {
	current := stabilitywindow.SampleFromDecision(decision)
	window := stabilitywindow.Append(history, current, stabilitywindow.DefaultSize)
	st := stabilitywindow.Analyze(window)
	report := DetectOscillation(window)
	policy := equilibriumpolicy.ForState(st, report)
	adjustments := append([]string(nil), policy.Actions...)
	semanticSignals, err := semanticrouter.RouteLayer(controltypes.LayerEquilibrium, equilibriumSignals(st, report, policy))
	if err != nil {
		adjustments = append(adjustments, "semantic router rejected equilibrium signal: "+err.Error())
		semanticSignals = nil
	}

	decision = applyPolicyWeights(decision, st, report, policy, &adjustments)
	var guardAdjustments []string
	decision, guardAdjustments = anticentralization.Apply(decision, policy, st)
	adjustments = append(adjustments, guardAdjustments...)

	decision.EquilibriumState = stateLabel(st, report)
	decision.EquilibriumActions = limitStrings(dedupeStrings(adjustments), 6)
	decision.ControlGraphEntropy = round(st.ControlGraphEntropy)
	decision.SystemStabilityScore = round(st.SystemStabilityScore)
	decision.ConvergenceVelocity = round(st.ConvergenceVelocity)
	decision.OscillationIndex = round(st.OscillationIndex)
	decision.ExplorationRatePercent = controlgraph.ClampRate(decision.ExplorationRatePercent)
	decision.Gain = round(decision.Gain)
	decision.Confidence = round(controlgraph.Clamp01(decision.Confidence))
	decision.Reasons = limitStrings(dedupeStrings(append(decision.Reasons, decision.EquilibriumActions...)), 8)

	trace := globalstate.EquilibriumTrace{
		State:             st,
		Policy:            policy,
		OscillationReport: report,
		SemanticSignals:   append([]controltypes.TypedSignal(nil), semanticSignals...),
		Adjustments:       append([]string(nil), decision.EquilibriumActions...),
	}
	return decision, trace
}

func equilibriumSignals(st globalstate.GlobalEquilibriumState, report globalstate.OscillationReport, policy globalstate.EquilibriumPolicy) []controltypes.TypedSignal {
	signals := []controltypes.TypedSignal{
		controltypes.NewSignal(controltypes.SignalWeight, "", map[string]any{
			"exploration_rate_percent": policy.ExplorationRatePercent,
			"damping_factor":           policy.DampingFactor,
			"consensus_threshold":      policy.ConsensusThreshold,
			"control_graph_entropy":    round(st.ControlGraphEntropy),
			"system_stability_score":   round(st.SystemStabilityScore),
			"convergence_velocity":     round(st.ConvergenceVelocity),
			"oscillation_index":        round(st.OscillationIndex),
		}, "equilibrium weight adjustment"),
	}
	if report.Severity == "high" || st.OscillationIndex >= equilibriumpolicy.HighOscillationThreshold {
		signals = append(signals, controltypes.NewSignal(controltypes.SignalConstraint, "", "global oscillation must be damped through weight limits", "global oscillation constraint"))
		return signals
	}
	if st.WindowSize >= 3 && report.Severity == "medium" {
		signals = append(signals, controltypes.NewSignal(controltypes.SignalConstraint, "", "medium oscillation requires tighter consensus threshold", "emerging oscillation constraint"))
	}
	if st.ControlGraphEntropy < equilibriumpolicy.EntropyFloor {
		signals = append(signals, controltypes.NewSignal(controltypes.SignalConstraint, "", "control graph entropy must remain above floor", "anti-centralization constraint"))
	}
	return signals
}

func applyPolicyWeights(decision controlgraph.ControlDecision, st globalstate.GlobalEquilibriumState, report globalstate.OscillationReport, policy globalstate.EquilibriumPolicy, adjustments *[]string) controlgraph.ControlDecision {
	if report.Severity == "high" || st.OscillationIndex >= equilibriumpolicy.HighOscillationThreshold {
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = minPositive(decision.Gain, policy.DampingFactor)
		*adjustments = append(*adjustments, "global oscillation constrained")
		return decision
	}
	if st.WindowSize >= 3 && decision.ConsensusScore < policy.ConsensusThreshold && (report.Severity == "medium" || st.OscillationIndex >= 0.45) {
		decision.ExplorationRatePercent = controlgraph.MinExplorationRatePercent
		decision.Gain = minPositive(decision.Gain, policy.DampingFactor)
		*adjustments = append(*adjustments, "low consensus gated by equilibrium")
		return decision
	}
	if st.WindowSize >= 4 && st.ConvergenceVelocity < equilibriumpolicy.LowConvergenceVelocity && st.SystemStabilityScore >= equilibriumpolicy.StableConvergenceThreshold && st.OscillationIndex < 0.25 {
		if decision.Action != controlgraph.ActionSafeMode && decision.Action != controlgraph.ActionStabilize {
			decision.ExplorationRatePercent = controlgraph.MaxExplorationRatePercent
			decision.Gain = math.Max(decision.Gain, policy.DampingFactor)
			*adjustments = append(*adjustments, "converged window increased exploration weight")
		}
	}
	return decision
}

func stateLabel(st globalstate.GlobalEquilibriumState, report globalstate.OscillationReport) string {
	switch {
	case report.Severity == "high" || st.OscillationIndex >= equilibriumpolicy.HighOscillationThreshold:
		return "damping"
	case st.ControlGraphEntropy < equilibriumpolicy.EntropyFloor:
		return "entropy_guard"
	case st.WindowSize >= 4 && st.ConvergenceVelocity < equilibriumpolicy.LowConvergenceVelocity && st.SystemStabilityScore >= equilibriumpolicy.StableConvergenceThreshold:
		return "converged"
	case report.Severity == "medium" || st.OscillationIndex >= 0.45:
		return "watch"
	default:
		return "stable"
	}
}

func minPositive(a, b float64) float64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}

func round(v float64) float64 {
	if v > -0.00005 && v < 0.00005 {
		return 0
	}
	return math.Round(v*10000) / 10000
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func limitStrings(in []string, n int) []string {
	if len(in) > n {
		return append([]string(nil), in[:n]...)
	}
	if in == nil {
		return []string{}
	}
	return append([]string(nil), in...)
}
