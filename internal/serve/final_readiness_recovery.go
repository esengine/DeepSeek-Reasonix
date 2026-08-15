package serve

import (
	"errors"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func validateSubmitAction(format, action string) error {
	if action != "" && action != control.FinalReadinessRecoveryAction {
		return errors.New(`unsupported action (supported: "final_readiness_recovery")`)
	}
	if action != "" && format != "" {
		return errors.New("format is unavailable for recovery actions")
	}
	return nil
}

func submitWithAction(ctrl control.SessionAPI, input, format, action string) {
	if action == control.FinalReadinessRecoveryAction {
		ctrl.SubmitFinalReadinessRecovery(input, input)
		return
	}
	ctrl.SubmitHTTPFormat(input, format)
}

func finalReadinessHistoryMessage(m provider.Message) ([]historyMessage, bool) {
	if !m.LocalOnly || m.FinalReadinessRecovery == nil {
		return nil, false
	}
	if !m.FinalReadinessRecovery.Pending {
		return nil, true
	}
	return []historyMessage{{
		Role: "final_readiness", Missing: append([]string(nil), m.FinalReadinessRecovery.Missing...),
	}}, true
}
