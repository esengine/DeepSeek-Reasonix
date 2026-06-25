package validator

import (
	"testing"

	controltypes "reasonix/internal/controlsemantics/types"
)

func TestValidateSignalAllowsStrictLayerTypes(t *testing.T) {
	allowed := []controltypes.TypedSignal{
		controltypes.NewSignal(controltypes.SignalDecision, controltypes.LayerControl, "balanced", "control decision"),
		controltypes.NewSignal(controltypes.SignalConstraint, controltypes.LayerEquilibrium, "dampen", "equilibrium constraint"),
		controltypes.NewSignal(controltypes.SignalWeight, controltypes.LayerEquilibrium, "gain", "equilibrium weight"),
		controltypes.NewSignal(controltypes.SignalObservation, controltypes.LayerTrace, "outcome", "trace observation"),
	}
	for _, signal := range allowed {
		if err := ValidateSignal(signal); err != nil {
			t.Fatalf("expected signal to be valid: %+v err=%v", signal, err)
		}
	}
}

func TestValidateSignalRejectsCrossLayerSignals(t *testing.T) {
	rejected := []controltypes.TypedSignal{
		controltypes.NewSignal(controltypes.SignalDecision, controltypes.LayerEquilibrium, "override", "equilibrium cannot decide"),
		controltypes.NewSignal(controltypes.SignalConstraint, controltypes.LayerControl, "constraint", "control cannot constrain"),
		controltypes.NewSignal(controltypes.SignalWeight, controltypes.LayerExecution, "policy", "execution cannot affect policy"),
		controltypes.NewSignal(controltypes.SignalDecision, controltypes.LayerTrace, "decision", "trace cannot decide"),
	}
	for _, signal := range rejected {
		if err := ValidateSignal(signal); err == nil {
			t.Fatalf("expected signal to be rejected: %+v", signal)
		}
	}
}
