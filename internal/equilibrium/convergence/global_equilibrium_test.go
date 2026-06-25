package convergence

import (
	"testing"

	controlgraph "reasonix/internal/controlplane/control_graph"
	controltypes "reasonix/internal/controlsemantics/types"
	globalstate "reasonix/internal/equilibrium/global_state"
)

func TestFilterDecisionConstrainsGlobalOscillationWithoutOverridingAction(t *testing.T) {
	history := []globalstate.DecisionSample{
		{Action: controlgraph.ActionExplore, ExplorationRatePercent: 12, Gain: 1.1},
		{Action: controlgraph.ActionDampen, ExplorationRatePercent: 3, Gain: 0.6},
		{Action: controlgraph.ActionExplore, ExplorationRatePercent: 12, Gain: 1.1},
		{Action: controlgraph.ActionDampen, ExplorationRatePercent: 3, Gain: 0.6},
	}
	decision := controlgraph.ControlDecision{
		Action:                 controlgraph.ActionExplore,
		Confidence:             0.9,
		ConsensusScore:         0.7,
		ExplorationRatePercent: 12,
		Gain:                   1.1,
		NodeInfluence: []controlgraph.NodeInfluence{
			{NodeID: "exploration-controller", Share: 0.5},
			{NodeID: "stability-controller", Share: 0.5},
		},
	}
	got, trace := FilterDecision(decision, history)
	if got.Action != controlgraph.ActionExplore {
		t.Fatalf("equilibrium must not override control action: %+v", got)
	}
	if got.ExplorationRatePercent != controlgraph.MinExplorationRatePercent {
		t.Fatalf("exploration rate = %d, want floor", got.ExplorationRatePercent)
	}
	if got.Gain >= decision.Gain {
		t.Fatalf("equilibrium should damp gain without changing action: before=%v after=%v", decision.Gain, got.Gain)
	}
	if got.EquilibriumState != "damping" {
		t.Fatalf("equilibrium state = %q, want damping", got.EquilibriumState)
	}
	if trace.OscillationReport.Severity != "high" {
		t.Fatalf("oscillation report = %+v, want high severity", trace.OscillationReport)
	}
	for _, signal := range trace.SemanticSignals {
		if signal.SourceLayer != controltypes.LayerEquilibrium {
			t.Fatalf("equilibrium emitted signal from wrong layer: %+v", signal)
		}
		if signal.Type != controltypes.SignalConstraint && signal.Type != controltypes.SignalWeight {
			t.Fatalf("equilibrium emitted forbidden signal type: %+v", signal)
		}
	}
}

func TestFilterDecisionIncreasesExplorationWeightAfterConvergence(t *testing.T) {
	history := []globalstate.DecisionSample{
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0, ControlGraphEntropy: 1},
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0, ControlGraphEntropy: 1},
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0, ControlGraphEntropy: 1},
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0, ControlGraphEntropy: 1},
	}
	decision := controlgraph.ControlDecision{
		Action:                 controlgraph.ActionBalanced,
		Confidence:             0.8,
		ConsensusScore:         0.8,
		ExplorationRatePercent: 10,
		Gain:                   1.0,
		NodeInfluence: []controlgraph.NodeInfluence{
			{NodeID: "exploration-controller", Share: 0.25},
			{NodeID: "stability-controller", Share: 0.25},
			{NodeID: "mutation-controller", Share: 0.25},
			{NodeID: "semantic-drift-controller", Share: 0.25},
		},
	}
	got, _ := FilterDecision(decision, history)
	if got.Action != controlgraph.ActionBalanced {
		t.Fatalf("equilibrium must preserve control action after convergence: %+v", got)
	}
	if got.ExplorationRatePercent != controlgraph.MaxExplorationRatePercent {
		t.Fatalf("exploration rate = %d, want max", got.ExplorationRatePercent)
	}
	if got.EquilibriumState != "converged" {
		t.Fatalf("equilibrium state = %q, want converged", got.EquilibriumState)
	}
}

func TestDetectOscillationReportsAffectedNodes(t *testing.T) {
	report := DetectOscillation([]globalstate.DecisionSample{
		{Action: controlgraph.ActionExplore, NodeInfluence: []controlgraph.NodeInfluence{{NodeID: "a", Share: 0.4}}},
		{Action: controlgraph.ActionDampen, NodeInfluence: []controlgraph.NodeInfluence{{NodeID: "a", Share: 0.3}}},
		{Action: controlgraph.ActionExplore, NodeInfluence: []controlgraph.NodeInfluence{{NodeID: "b", Share: 0.4}}},
	})
	if report.Severity != "high" {
		t.Fatalf("severity = %q, want high", report.Severity)
	}
	if len(report.AffectedNodes) == 0 {
		t.Fatalf("missing affected nodes: %+v", report)
	}
}
