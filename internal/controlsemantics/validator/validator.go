package validator

import (
	"fmt"

	controltypes "reasonix/internal/controlsemantics/types"
)

func ValidateSignal(signal controltypes.TypedSignal) error {
	if signal.Type == "" {
		return fmt.Errorf("control semantics signal type is required")
	}
	if signal.SourceLayer == "" {
		return fmt.Errorf("control semantics source layer is required")
	}
	if !Allowed(signal.SourceLayer, signal.Type) {
		return fmt.Errorf("control semantics violation: layer %q cannot emit %q", signal.SourceLayer, signal.Type)
	}
	return nil
}

func ValidateSignals(signals []controltypes.TypedSignal) error {
	for _, signal := range signals {
		if err := ValidateSignal(signal); err != nil {
			return err
		}
	}
	return nil
}

func Allowed(layer controltypes.SourceLayer, signalType controltypes.SignalType) bool {
	switch layer {
	case controltypes.LayerControl:
		return signalType == controltypes.SignalDecision
	case controltypes.LayerEquilibrium:
		return signalType == controltypes.SignalConstraint || signalType == controltypes.SignalWeight
	case controltypes.LayerTrace:
		return signalType == controltypes.SignalObservation
	case controltypes.LayerExecution:
		return false
	default:
		return false
	}
}
