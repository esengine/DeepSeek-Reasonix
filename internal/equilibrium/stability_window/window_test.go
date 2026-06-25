package stabilitywindow

import (
	"testing"

	controlgraph "reasonix/internal/controlplane/control_graph"
	globalstate "reasonix/internal/equilibrium/global_state"
)

func TestAnalyzeDetectsStableWindowAndEntropy(t *testing.T) {
	samples := []globalstate.DecisionSample{
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0},
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0},
		{Action: controlgraph.ActionBalanced, ExplorationRatePercent: 10, Gain: 1.0},
		{
			Action:                 controlgraph.ActionBalanced,
			ExplorationRatePercent: 10,
			Gain:                   1.0,
			NodeInfluence: []controlgraph.NodeInfluence{
				{NodeID: "a", Share: 0.50},
				{NodeID: "b", Share: 0.50},
			},
		},
	}
	st := Analyze(samples)
	if st.ControlGraphEntropy < 0.99 {
		t.Fatalf("entropy = %.3f, want near 1", st.ControlGraphEntropy)
	}
	if st.SystemStabilityScore < 0.95 {
		t.Fatalf("stability = %.3f, want stable", st.SystemStabilityScore)
	}
	if st.ConvergenceVelocity != 0 {
		t.Fatalf("convergence velocity = %.3f, want 0", st.ConvergenceVelocity)
	}
}

func TestAnalyzeDetectsOscillatingWindow(t *testing.T) {
	samples := []globalstate.DecisionSample{
		{Action: controlgraph.ActionExplore, ExplorationRatePercent: 12, Gain: 1.1},
		{Action: controlgraph.ActionDampen, ExplorationRatePercent: 3, Gain: 0.6},
		{Action: controlgraph.ActionExplore, ExplorationRatePercent: 12, Gain: 1.1},
		{Action: controlgraph.ActionDampen, ExplorationRatePercent: 3, Gain: 0.6},
	}
	st := Analyze(samples)
	if st.OscillationIndex < 0.9 {
		t.Fatalf("oscillation index = %.3f, want high", st.OscillationIndex)
	}
	if st.SystemStabilityScore >= 0.6 {
		t.Fatalf("stability = %.3f, want unstable", st.SystemStabilityScore)
	}
	if st.ConvergenceVelocity <= 0 {
		t.Fatalf("convergence velocity should detect movement: %+v", st)
	}
}

func TestAnalyzeDetectsCentralizedControlGraph(t *testing.T) {
	st := Analyze([]globalstate.DecisionSample{{
		Action: controlgraph.ActionBalanced,
		NodeInfluence: []controlgraph.NodeInfluence{
			{NodeID: "dominant", Share: 1.0},
		},
	}})
	if st.ControlGraphEntropy != 0 {
		t.Fatalf("single-node entropy = %.3f, want 0", st.ControlGraphEntropy)
	}
}
