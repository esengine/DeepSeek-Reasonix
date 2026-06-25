package layerguard

import (
	"testing"

	controltypes "reasonix/internal/controlsemantics/types"
)

func TestGuardSignalsBlocksIllegalCrossLayerMutation(t *testing.T) {
	_, err := GuardSignals([]controltypes.TypedSignal{
		controltypes.NewSignal(controltypes.SignalWeight, controltypes.LayerEquilibrium, "gain", "valid"),
		controltypes.NewSignal(controltypes.SignalConstraint, controltypes.LayerControl, "constraint", "invalid"),
	})
	if err == nil {
		t.Fatal("expected guard to block control layer constraint signal")
	}
}

func TestGuardSignalsPreservesValidSignals(t *testing.T) {
	signals := []controltypes.TypedSignal{
		controltypes.NewSignal(controltypes.SignalDecision, controltypes.LayerControl, "balanced", "decision"),
	}
	got, err := GuardSignals(signals)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != controltypes.SignalDecision || got[0].SourceLayer != controltypes.LayerControl {
		t.Fatalf("unexpected guarded signals: %+v", got)
	}
}
