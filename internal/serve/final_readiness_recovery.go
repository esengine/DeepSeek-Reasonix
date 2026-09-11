package serve

import (
	"errors"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func validateSubmitAction(format, action string) error {
	if action != "" && action != control.FinalReadinessRecoveryAction && action != control.ProtocolRecoveryAction {
		return errors.New(`unsupported action (supported: "final_readiness_recovery", "protocol_recovery")`)
	}
	if action != "" && format != "" {
		return errors.New("format is unavailable for recovery actions")
	}
	return nil
}

func submitWithAction(ctrl control.SessionAPI, input, format, action string, recoveryID ...string) {
	if action == control.ProtocolRecoveryAction {
		id := ""
		if len(recoveryID) > 0 {
			id = recoveryID[0]
		}
		if recovery, ok := ctrl.(interface{ SubmitProtocolRecovery(string, string) }); ok {
			recovery.SubmitProtocolRecovery(id, input)
		}
		return
	}
	if action == control.FinalReadinessRecoveryAction {
		ctrl.SubmitFinalReadinessRecovery(input, input)
		return
	}
	ctrl.SubmitHTTPFormat(input, format)
}

func finalReadinessHistoryMessage(m provider.Message) ([]historyMessage, bool) {
	if m.LocalOnly && len(m.ProtocolRecovery) > 0 {
		if r, ok := provider.DecodeProtocolRecovery(m.ProtocolRecovery); ok && r.State == "pending" {
			return []historyMessage{{Role: "protocol_recovery", ProtocolRecovery: &provider.ProtocolRecoveryAction{ID: r.ID}}}, true
		}
		return nil, true
	}
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
