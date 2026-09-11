package acp

import "reasonix/internal/control"

// currentQualityFloor reads the floor from the live controller; sessions
// without a controller report the standard default.
func (s *acpSession) currentQualityFloor() string {
	ctrl := s.currentCtrl()
	if ctrl == nil {
		return control.QualityFloorStandard
	}
	return ctrl.QualityFloor()
}

// withQualityFloorConfig publishes the session quality floor as a select
// option. Light vocabulary is absent: standard is where it folds.
func withQualityFloorConfig(state SessionConfigState, floor string) SessionConfigState {
	if floor != control.QualityFloorDelivery {
		floor = control.QualityFloorStandard
	}
	option := SessionConfigOption{
		ID:           "quality_floor",
		Name:         "Quality Floor",
		Category:     "work_mode",
		Type:         "select",
		CurrentValue: floor,
		Options: []SessionConfigSelectOption{
			{Value: control.QualityFloorStandard, Name: "Standard", Description: "Adaptive verification and review follow task risk"},
			{Value: control.QualityFloorDelivery, Name: "Delivery", Description: "Session-sticky delivery gates: full verification, review receipts, project checks"},
		},
	}
	for i := range state.ConfigOptions {
		if normalizeConfigID(state.ConfigOptions[i].ID) == option.ID {
			state.ConfigOptions[i] = option
			return state
		}
	}
	state.ConfigOptions = append(state.ConfigOptions, option)
	return state
}
