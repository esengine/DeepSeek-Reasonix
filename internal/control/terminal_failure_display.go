package control

import (
	"context"
	"errors"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/secrets"
	"reasonix/internal/session"
)

// Preparation can fail before Agent.Run writes an interrupted record. Keep
// that cause in the existing optional display format, atomically with turn/end.
// Agent failures already have a canonical local-only notice; never duplicate it.
func (c *Controller) terminalFailureDisplayEvents(e event.Event, projection session.Projection) ([]session.Event, error) {
	if e.Err == nil || e.Cancelled || errors.Is(e.Err, context.Canceled) || e.TurnID == "" {
		return nil, nil
	}
	for _, m := range projection.Messages {
		if c.turnEvents.turnMessageIDs[m.ID] && hasFailureCause(m) {
			return nil, nil
		}
	}
	if p := c.turnEvents.pendingTermination; p != nil && p.TurnID == e.TurnID {
		for _, m := range p.Messages {
			if c.turnEvents.turnMessageIDs[m.ID] && hasFailureCause(m) {
				return nil, nil
			}
		}
	}
	cause := secrets.FailureDetail(e.Err)
	return displayNoticeEvents(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeProviderRequestFailed,
		MessageID: "failure:" + e.TurnID, TurnID: e.TurnID, Text: cause, Detail: cause, Diagnostic: provider.DiagnoseFailure(e.Err)})
}

func hasFailureCause(m provider.Message) bool {
	return m.LocalOnly && m.InterruptedTurn != nil && m.InterruptedTurn.TerminalStatus == "failed" && m.InterruptedTurn.Cause != ""
}
