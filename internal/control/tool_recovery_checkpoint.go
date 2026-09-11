package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func (c *Controller) stampToolRecoveryEvent(e event.Event) error {
	if e.Kind != event.ToolStarted {
		return nil
	}
	return c.stampToolRecoveryCheckpoint(e.Tool.AttemptID)
}

func (c *Controller) stampToolRecoveryCheckpoint(attempt string) error {
	if c.executor == nil || attempt == "" {
		return nil
	}
	for _, record := range c.executor.PendingToolRecovery() {
		if record.Identity.AttemptID != attempt || record.ReadOnly {
			continue
		}
		c.checkpoints.mu.Lock()
		store := c.checkpoints.store
		c.checkpoints.mu.Unlock()
		if store == nil {
			return nil
		}
		payload, err := json.Marshal(provider.ModelMessages(c.executor.Session().Snapshot()))
		if err != nil {
			return err
		}
		digest := sha256.Sum256(payload)
		return store.BindRecoveryIdentity(checkpoint.RecoveryIdentity{Action: record.Identity, TranscriptDigest: hex.EncodeToString(digest[:])})
	}
	return nil
}
