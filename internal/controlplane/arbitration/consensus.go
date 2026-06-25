package arbitration

import (
	"math"
	"sort"

	"reasonix/internal/controlplane/consensus"
	controlgraph "reasonix/internal/controlplane/control_graph"
)

const highVarianceThreshold = 0.115

func Arbitrate(graph controlgraph.ControlGraph, st controlgraph.SystemState, signals []controlgraph.ControlSignal) controlgraph.ControlDecision {
	result := consensus.Aggregate(signals, graph.Edges)
	if len(signals) == 0 || result.TotalWeight <= 0 {
		return safeDecision(st, "no control signals available")
	}
	action := topAction(result.Scores)
	decision := controlgraph.ControlDecision{
		Action:                 action,
		Confidence:             round(result.ConsensusScore * (1 - math.Min(result.Variance, 0.5))),
		ConsensusScore:         round(result.ConsensusScore),
		Variance:               round(result.Variance),
		ExplorationRatePercent: result.ExplorationRatePercent,
		Gain:                   round(result.Gain),
		Controller:             "distributed-control-plane",
		SemanticShift:          append([]string(nil), st.SemanticShift...),
		NodeInfluence:          append([]controlgraph.NodeInfluence(nil), result.NodeInfluence...),
		Signals:                signalSummaries(result.Signals),
		Reasons:                dominantReasons(result.Signals, action),
	}
	decision = resolveConflicts(decision, result, st)
	decision.ExplorationRatePercent = controlgraph.ClampRate(decision.ExplorationRatePercent)
	if decision.Gain <= 0 {
		decision.Gain = 1
	}
	decision.Gain = round(decision.Gain)
	decision.Confidence = round(controlgraph.Clamp01(decision.Confidence))
	decision.ConsensusScore = round(controlgraph.Clamp01(decision.ConsensusScore))
	decision.Variance = round(decision.Variance)
	return decision
}

func topAction(scores map[controlgraph.Action]float64) controlgraph.Action {
	if len(scores) == 0 {
		return controlgraph.ActionSafeMode
	}
	actions := make([]controlgraph.Action, 0, len(scores))
	for action := range scores {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(i, j int) bool {
		if scores[actions[i]] == scores[actions[j]] {
			return actions[i] < actions[j]
		}
		return scores[actions[i]] > scores[actions[j]]
	})
	return actions[0]
}

func safeDecision(st controlgraph.SystemState, reason string) controlgraph.ControlDecision {
	return controlgraph.ControlDecision{
		Action:                 controlgraph.ActionSafeMode,
		Confidence:             1,
		ConsensusScore:         1,
		ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
		Gain:                   0.50,
		Controller:             "distributed-control-plane",
		SemanticShift:          append([]string(nil), st.SemanticShift...),
		Reasons:                []string{reason},
		SafeMode:               true,
	}
}

func signalSummaries(signals []controlgraph.ControlSignal) []string {
	out := make([]string, 0, len(signals))
	for _, sig := range signals {
		out = append(out, sig.NodeID+":"+string(sig.Action))
	}
	return out
}

func dominantReasons(signals []controlgraph.ControlSignal, action controlgraph.Action) []string {
	var out []string
	for _, sig := range signals {
		if sig.Action == action && sig.Reason != "" {
			out = append(out, sig.Reason)
		}
	}
	if len(out) == 0 {
		for _, sig := range signals {
			if sig.Reason != "" {
				out = append(out, sig.Reason)
				break
			}
		}
	}
	if len(out) > 4 {
		out = out[:4]
	}
	return out
}

func round(v float64) float64 {
	return math.Round(v*1000) / 1000
}
