package control

import (
	"context"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
)

// checkpointToolTranscript persists execution evidence before another tool may
// start. Listing projections and other UI sidecars remain on the normal autosave.
func (c *Controller) checkpointToolTranscript() error {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	path := c.SessionPath()
	if path == "" || c.executor == nil {
		return nil
	}
	session := c.executor.Session()
	if session == nil || !session.HasContent() {
		return nil
	}
	_, err := c.extensionSessionStrategy(context.Background(), extension.PointSessionSave, dispatch.PhaseSave, path)
	if err != nil {
		return err
	}
	err, _ = persistSessionSnapshotMode(session, path, false, true)
	return err
}
