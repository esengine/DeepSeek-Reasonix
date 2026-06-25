package stabilitywindow

import (
	"math"

	controlgraph "reasonix/internal/controlplane/control_graph"
	globalstate "reasonix/internal/equilibrium/global_state"
)

const DefaultSize = 8

func SampleFromDecision(decision controlgraph.ControlDecision) globalstate.DecisionSample {
	return globalstate.DecisionSample{
		Action:                 decision.Action,
		ExplorationRatePercent: decision.ExplorationRatePercent,
		Gain:                   decision.Gain,
		ConsensusScore:         decision.ConsensusScore,
		Variance:               decision.Variance,
		ControlGraphEntropy:    decision.ControlGraphEntropy,
		SystemStabilityScore:   decision.SystemStabilityScore,
		ConvergenceVelocity:    decision.ConvergenceVelocity,
		OscillationIndex:       decision.OscillationIndex,
		NodeInfluence:          append([]controlgraph.NodeInfluence(nil), decision.NodeInfluence...),
	}
}

func Append(history []globalstate.DecisionSample, current globalstate.DecisionSample, size int) []globalstate.DecisionSample {
	if size <= 0 {
		size = DefaultSize
	}
	out := append([]globalstate.DecisionSample(nil), history...)
	out = append(out, current)
	if len(out) > size {
		out = out[len(out)-size:]
	}
	return out
}

func Analyze(samples []globalstate.DecisionSample) globalstate.GlobalEquilibriumState {
	st := globalstate.GlobalEquilibriumState{WindowSize: len(samples)}
	if len(samples) == 0 {
		st.ControlGraphEntropy = 1
		st.SystemStabilityScore = 1
		return st
	}
	latest := samples[len(samples)-1]
	st.ControlGraphEntropy = entropyFor(latest)
	st.OscillationIndex = transitionRate(samples)
	st.ConvergenceVelocity = convergenceVelocity(samples)
	st.SystemStabilityScore = stabilityScore(samples)
	return st
}

func entropyFor(sample globalstate.DecisionSample) float64 {
	if len(sample.NodeInfluence) == 0 {
		if sample.ControlGraphEntropy > 0 {
			return clamp01(sample.ControlGraphEntropy)
		}
		return 1
	}
	total := 0.0
	for _, influence := range sample.NodeInfluence {
		if influence.Share > 0 {
			total += influence.Share
		}
	}
	if total <= 0 {
		return 1
	}
	positive := 0
	entropy := 0.0
	for _, influence := range sample.NodeInfluence {
		if influence.Share <= 0 {
			continue
		}
		positive++
		p := influence.Share / total
		entropy -= p * math.Log(p)
	}
	if positive <= 1 {
		return 0
	}
	return clamp01(entropy / math.Log(float64(positive)))
}

func transitionRate(samples []globalstate.DecisionSample) float64 {
	if len(samples) < 2 {
		return 0
	}
	transitions := 0
	for i := 1; i < len(samples); i++ {
		if samples[i].Action != samples[i-1].Action {
			transitions++
		}
	}
	return clamp01(float64(transitions) / float64(len(samples)-1))
}

func convergenceVelocity(samples []globalstate.DecisionSample) float64 {
	if len(samples) < 2 {
		return 0
	}
	total := 0.0
	for i := 1; i < len(samples); i++ {
		actionDelta := math.Abs(ActionValue(samples[i].Action) - ActionValue(samples[i-1].Action))
		rateDelta := math.Abs(float64(samples[i].ExplorationRatePercent-samples[i-1].ExplorationRatePercent)) /
			float64(controlgraph.MaxExplorationRatePercent-controlgraph.MinExplorationRatePercent)
		gainDelta := math.Abs(samples[i].Gain-samples[i-1].Gain) / 1.25
		total += (actionDelta + rateDelta + gainDelta) / 3
	}
	return clamp01(total / float64(len(samples)-1))
}

func stabilityScore(samples []globalstate.DecisionSample) float64 {
	if len(samples) < 2 {
		return 1
	}
	minRate, maxRate := samples[0].ExplorationRatePercent, samples[0].ExplorationRatePercent
	minGain, maxGain := samples[0].Gain, samples[0].Gain
	variance := 0.0
	for _, sample := range samples {
		if sample.ExplorationRatePercent < minRate {
			minRate = sample.ExplorationRatePercent
		}
		if sample.ExplorationRatePercent > maxRate {
			maxRate = sample.ExplorationRatePercent
		}
		if sample.Gain < minGain {
			minGain = sample.Gain
		}
		if sample.Gain > maxGain {
			maxGain = sample.Gain
		}
		variance += clamp01(sample.Variance)
	}
	rateRange := float64(maxRate-minRate) / float64(controlgraph.MaxExplorationRatePercent-controlgraph.MinExplorationRatePercent)
	gainRange := (maxGain - minGain) / 1.25
	instability := transitionRate(samples)*0.50 + clamp01(rateRange)*0.20 + clamp01(gainRange)*0.20 + clamp01(variance/float64(len(samples)))*0.10
	return clamp01(1 - instability)
}

func ActionValue(action controlgraph.Action) float64 {
	switch action {
	case controlgraph.ActionSafeMode:
		return 0
	case controlgraph.ActionStabilize:
		return 0.10
	case controlgraph.ActionDampen:
		return 0.30
	case controlgraph.ActionBalanced:
		return 0.55
	case controlgraph.ActionExplore:
		return 0.95
	default:
		return 0.55
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
