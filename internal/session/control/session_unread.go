package control

import (
	"log/slog"
	"time"

	"reasonix/internal/state/sessionstore"
)

// MarkSessionViewed records that the person has seen the open session. The
// kernel never infers it: the frontend that knows which pane has the person's
// attention calls this. Only the session sidecar is written.
func (c *Controller) MarkSessionViewed() error {
	return sessionstore.MarkSessionViewed(c.SessionPath(), time.Now())
}

// recordTurnFinished stamps a turn's end on the session sidecar before TurnDone
// is delivered, so a frontend refreshing its list on that event already sees
// the mark. A turn the person cancelled does not leave the session unread.
func (c *Controller) recordTurnFinished(cancelled bool) {
	c.mu.Lock()
	closed := c.gate.closed
	c.mu.Unlock()
	if closed {
		return
	}
	if err := sessionstore.RecordSessionFinished(c.SessionPath(), time.Now(), !cancelled); err != nil {
		slog.Warn("controller: record turn finished", "err", err)
	}
}
