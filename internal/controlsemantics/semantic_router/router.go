package semanticrouter

import (
	layerguard "reasonix/internal/controlsemantics/layer_guard"
	controltypes "reasonix/internal/controlsemantics/types"
)

func Route(signal controltypes.TypedSignal) (controltypes.TypedSignal, error) {
	return layerguard.GuardSignal(signal)
}

func RouteAll(signals []controltypes.TypedSignal) ([]controltypes.TypedSignal, error) {
	return layerguard.GuardSignals(signals)
}

func RouteLayer(layer controltypes.SourceLayer, signals []controltypes.TypedSignal) ([]controltypes.TypedSignal, error) {
	routed := make([]controltypes.TypedSignal, 0, len(signals))
	for _, signal := range signals {
		signal.SourceLayer = layer
		guarded, err := Route(signal)
		if err != nil {
			return nil, err
		}
		routed = append(routed, guarded)
	}
	return routed, nil
}
