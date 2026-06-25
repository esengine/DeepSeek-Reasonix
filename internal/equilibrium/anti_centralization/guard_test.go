package anticentralization

import (
	"testing"

	controlgraph "reasonix/internal/controlplane/control_graph"
	globalstate "reasonix/internal/equilibrium/global_state"
)

func TestApplyCapsDominantNodeAndEnforcesEntropyFloor(t *testing.T) {
	decision := controlgraph.ControlDecision{
		Action:                 controlgraph.ActionBalanced,
		Confidence:             1,
		ExplorationRatePercent: controlgraph.MinExplorationRatePercent,
		Gain:                   1.2,
		NodeInfluence: []controlgraph.NodeInfluence{
			{NodeID: "dominant", Share: 0.80},
			{NodeID: "other", Share: 0.20},
		},
	}
	policy := globalstate.EquilibriumPolicy{ExplorationRatePercent: controlgraph.MaxExplorationRatePercent}
	st := globalstate.GlobalEquilibriumState{ControlGraphEntropy: 0.30}
	got, adjustments := Apply(decision, policy, st)
	if got.Confidence >= decision.Confidence {
		t.Fatalf("confidence was not penalized: before=%v after=%v", decision.Confidence, got.Confidence)
	}
	if got.Gain > 1.0 {
		t.Fatalf("dominant node should cap gain, got %+v", got)
	}
	if got.ExplorationRatePercent != controlgraph.MaxExplorationRatePercent {
		t.Fatalf("entropy floor should reopen bounded exploration, got %+v", got)
	}
	if len(adjustments) != 2 {
		t.Fatalf("adjustments = %+v, want dominance and entropy guards", adjustments)
	}
}
