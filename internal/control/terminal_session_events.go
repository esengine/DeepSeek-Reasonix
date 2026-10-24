package control

import (
	"encoding/json"
	"log/slog"

	"reasonix/internal/event"
	"reasonix/internal/session"
)

func (c *Controller) terminalSessionEvents(e event.Event, projection session.Projection) ([]session.Event, error) {
	var out []session.Event
	if diagnostic, err := c.providerDiagnosticEvent(e); err != nil {
		// Optional evidence must never prevent required lifecycle facts from committing.
		slog.Warn("controller: discarded optional provider diagnostic", "turnId", e.TurnID, "err", err)
	} else if diagnostic != nil {
		out = append(out, *diagnostic)
	}
	if notices, err := c.terminalFailureDisplayEvents(e, projection); err != nil {
		slog.Warn("controller: discarded optional failure display", "turnId", e.TurnID)
	} else {
		out = append(out, notices...)
	}
	interactionState := "unavailable"
	if e.Cancelled || e.Status == event.TurnInterrupted {
		interactionState = "cancelled"
	}
	out = append(out, session.ClosureEvents(projection, interactionState, "turn ended before recording a result")...)
	if e.Recovery != nil && e.Recovery.State == "recovery_required" {
		payload, err := json.Marshal(e.Recovery)
		if err != nil {
			return nil, err
		}
		out = append(out, session.Event{Kind: "runtime/recovery", Payload: payload})
	}
	payload, err := json.Marshal(map[string]any{"status": terminalTurnStatus(e)})
	if err != nil {
		return nil, err
	}
	return append(out, session.Event{Kind: "turn/end", Payload: payload}), nil
}
