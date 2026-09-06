package cli

import (
	"strings"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// commitTurnPauseNotice renders a controlled-pause TurnDone outcome as one
// informational line. Pauses are not send failures: the composer is already
// free and the user continues with an ordinary message.
func (m *chatTUI) commitTurnPauseNotice(e event.Event) {
	if e.ProtocolRecovery != nil {
		m.commitLine(wrapForViewport("ⓘ "+i18n.M.ProtocolRecoveryLabel+": /recover-context "+e.ProtocolRecovery.ID, m.width, activeCLITheme.info))
	}
	var text string
	switch e.Outcome {
	case event.TurnOutcomeRecoveryPaused:
		text = "⏸ " + i18n.M.RecoveryPaused
	case event.TurnOutcomeCompletionUncertain:
		text = "⏸ " + i18n.M.CompletionUncertain
	case event.TurnOutcomeFinalReadiness:
		text = "ⓘ " + i18n.M.FinalReadinessRecovery
	default:
		if e.Err != nil && e.Err.Error() != "" && !strings.Contains(e.Err.Error(), "context canceled") {
			m.commitLine(wrapForViewport(i18n.M.ErrorPrefix+" "+e.Err.Error(), m.width, activeCLITheme.warn))
		}
		return
	}
	m.commitLine(wrapForViewport(text, m.width, activeCLITheme.info))
}
