package layerguard

import (
	controltypes "reasonix/internal/controlsemantics/types"
	"reasonix/internal/controlsemantics/validator"
)

func GuardSignal(signal controltypes.TypedSignal) (controltypes.TypedSignal, error) {
	if err := validator.ValidateSignal(signal); err != nil {
		return controltypes.TypedSignal{}, err
	}
	return signal, nil
}

func GuardSignals(signals []controltypes.TypedSignal) ([]controltypes.TypedSignal, error) {
	out := make([]controltypes.TypedSignal, 0, len(signals))
	for _, signal := range signals {
		guarded, err := GuardSignal(signal)
		if err != nil {
			return nil, err
		}
		out = append(out, guarded)
	}
	return out, nil
}
