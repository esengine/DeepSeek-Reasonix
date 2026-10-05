package control

import (
	"log/slog"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// keepSupersededVersion saves the conversation a rewind is about to cut, so the
// cut changes what the session continues from without destroying what was
// said. The version is a record, not a precondition: a rewind that cannot
// keep one still does what the user asked.
func (c *Controller) keepSupersededVersion(msgs []provider.Message, boundary int) {
	path := c.SessionPath()
	if boundary >= len(msgs) || path == "" || c.sessionDir == "" {
		return
	}
	if _, err := sessionstore.SaveSupersededVersion(path, msgs); err != nil {
		slog.Warn("rewind: keep the superseded version", "path", path, "err", err)
	}
}
