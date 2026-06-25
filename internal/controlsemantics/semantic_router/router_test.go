package semanticrouter

import (
	"reflect"
	"testing"

	controltypes "reasonix/internal/controlsemantics/types"
)

func TestRouteLayerAssignsLayerDeterministically(t *testing.T) {
	input := []controltypes.TypedSignal{
		controltypes.NewSignal(controltypes.SignalConstraint, "", "oscillation", "constraint"),
		controltypes.NewSignal(controltypes.SignalWeight, "", "gain", "weight"),
	}
	first, err := RouteLayer(controltypes.LayerEquilibrium, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RouteLayer(controltypes.LayerEquilibrium, input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("routing is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
	for _, signal := range first {
		if signal.SourceLayer != controltypes.LayerEquilibrium {
			t.Fatalf("router did not assign equilibrium layer: %+v", signal)
		}
	}
}

func TestRouteRejectsForbiddenTransition(t *testing.T) {
	_, err := Route(controltypes.NewSignal(controltypes.SignalDecision, controltypes.LayerEquilibrium, "override", "invalid"))
	if err == nil {
		t.Fatal("expected equilibrium decision signal to be rejected")
	}
}
