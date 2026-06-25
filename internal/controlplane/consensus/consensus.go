package consensus

import (
	"math"
	"sort"

	controlgraph "reasonix/internal/controlplane/control_graph"
)

const maxNodeShare = 0.55

type Result struct {
	Scores                 map[controlgraph.Action]float64
	ConsensusScore         float64
	Variance               float64
	DominantShare          float64
	ExplorationRatePercent int
	Gain                   float64
	TotalWeight            float64
	Signals                []controlgraph.ControlSignal
	NodeInfluence          []controlgraph.NodeInfluence
}

func Aggregate(signals []controlgraph.ControlSignal, edges []controlgraph.ControlEdge) Result {
	if len(signals) == 0 {
		return Result{Scores: map[controlgraph.Action]float64{}}
	}
	ordered := append([]controlgraph.ControlSignal(nil), signals...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].NodeID < ordered[j].NodeID })
	weights := adjustedWeights(ordered, edges)
	weights = capDominantWeights(weights)
	result := Result{
		Scores:  map[controlgraph.Action]float64{},
		Signals: ordered,
	}
	var weightedRate float64
	var weightedGain float64
	var weightedAction float64
	for i, sig := range ordered {
		w := weights[i]
		if w <= 0 {
			continue
		}
		result.TotalWeight += w
		result.Scores[sig.Action] += w
		weightedRate += float64(sig.ExplorationRatePercent) * w
		weightedGain += sig.Gain * w
		weightedAction += actionValue(sig.Action) * w
		if result.DominantShare < w {
			result.DominantShare = w
		}
	}
	if result.TotalWeight <= 0 {
		return result
	}
	result.DominantShare /= result.TotalWeight
	result.ExplorationRatePercent = controlgraph.ClampRate(int(math.Round(weightedRate / result.TotalWeight)))
	result.Gain = weightedGain / result.TotalWeight
	mean := weightedAction / result.TotalWeight
	for i, sig := range ordered {
		w := weights[i]
		if w <= 0 {
			continue
		}
		delta := actionValue(sig.Action) - mean
		result.Variance += w * delta * delta
	}
	result.Variance /= result.TotalWeight
	for _, score := range result.Scores {
		if score/result.TotalWeight > result.ConsensusScore {
			result.ConsensusScore = score / result.TotalWeight
		}
	}
	for i, sig := range ordered {
		w := weights[i]
		if w <= 0 {
			continue
		}
		result.NodeInfluence = append(result.NodeInfluence, controlgraph.NodeInfluence{
			NodeID:      sig.NodeID,
			Type:        sig.Type,
			Action:      sig.Action,
			Share:       w / result.TotalWeight,
			Weight:      sig.Weight,
			Reliability: sig.Reliability,
		})
	}
	return result
}

func adjustedWeights(signals []controlgraph.ControlSignal, edges []controlgraph.ControlEdge) []float64 {
	weights := make([]float64, len(signals))
	index := map[string]int{}
	for i, sig := range signals {
		index[sig.NodeID] = i
		weights[i] = sig.Weight * sig.Reliability * sig.Confidence * sig.Strength
	}
	for _, edge := range edges {
		from, okFrom := index[edge.From]
		to, okTo := index[edge.To]
		if !okFrom || !okTo || from == to {
			continue
		}
		sourceStrength := controlgraph.Clamp01(signals[from].Strength)
		switch edge.Influence {
		case controlgraph.InfluenceAmplify:
			weights[to] *= 1 + sourceStrength*0.10
		case controlgraph.InfluenceSuppress:
			weights[to] *= 1 - sourceStrength*0.25
		case controlgraph.InfluenceBalance:
			gap := math.Abs(signals[from].Strength - signals[to].Strength)
			weights[to] *= 1 - controlgraph.Clamp01(gap)*0.10
		}
		if weights[to] < 0 {
			weights[to] = 0
		}
	}
	return weights
}

func capDominantWeights(weights []float64) []float64 {
	if len(weights) < 2 {
		return weights
	}
	// Bound each node to maxNodeShare of the POST-cap total. Capping against the
	// pre-cap total is ineffective: clamping [100,1] to 0.55*101=55.55 still
	// leaves that node at 55.55/56.55 ≈ 98% of the total. For a dominant node we
	// need w/(w+others) == maxNodeShare, i.e. w = maxNodeShare*others/(1-maxNodeShare).
	// Capping one node lowers the total for the rest, so iterate (at most one new
	// dominant node can appear per pass, bounded by len(weights)).
	for range weights {
		total := 0.0
		for _, w := range weights {
			total += w
		}
		if total <= 0 {
			return weights
		}
		capValue := total * maxNodeShare
		idx := -1
		for i, w := range weights {
			if w > capValue+1e-9 && (idx == -1 || w > weights[idx]) {
				idx = i
			}
		}
		if idx == -1 {
			break
		}
		others := total - weights[idx]
		weights[idx] = maxNodeShare * others / (1 - maxNodeShare)
	}
	return weights
}

func actionValue(action controlgraph.Action) float64 {
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
