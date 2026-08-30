package main

import "reasonix/internal/control"

// stubSessionAPI supplies port defaults for the test controllers that only
// override the few methods their scenario exercises. Embedding the bare
// control.SessionAPI interface leaves it nil, so every un-overridden call
// panics; production readers call the port directly and must not carry a
// recover to absorb that.
type stubSessionAPI struct {
	control.SessionAPI
}

func (stubSessionAPI) QualityFloor() string         { return control.QualityFloorStandard }
func (stubSessionAPI) SetQualityFloor(string) error { return nil }
