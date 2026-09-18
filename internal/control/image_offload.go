package control

import (
	"context"
	"crypto/sha256"
	"fmt"

	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func (c *Controller) bindImageOffloadRecorder() {
	if c == nil || c.executor == nil {
		return
	}
	c.executor.SetImageOffloadRecorder(c.recordImageOffload)
}

func (c *Controller) recordImageOffload(payload provider.ImageOffloadPayload) {
	if c == nil || !c.sessionEventCommitAllowed() {
		return
	}
	store := c.sessionEventStore()
	if store == nil {
		return
	}
	raw, err := payload.Marshal()
	if err != nil || len(payload.Targets) == 0 {
		return
	}
	digest := sha256.Sum256(raw)
	snapshot := store.ExecutionSnapshot()
	c.turnEvents.commitMu.Lock()
	defer c.turnEvents.commitMu.Unlock()
	if _, err := c.appendSessionBatch(context.Background(), store, session.Batch{
		OperationID: fmt.Sprintf("image-offload:%x", digest[:16]),
		TurnID:      snapshot.Projection.TurnID,
		Events: []session.Event{{
			Kind:     session.EventImageOffload,
			Optional: true,
			Payload:  raw,
		}},
	}); err != nil {
		c.failTurnEventLedger(err)
	}
}
