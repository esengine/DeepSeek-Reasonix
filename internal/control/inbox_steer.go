package control

import (
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/sessioninbox"
)

func steerAlreadyAdmitted(state sessioninbox.InboxState) bool {
	switch state {
	case sessioninbox.StateRunning, sessioninbox.StateSteerAccepted, sessioninbox.StateSteerConsumed:
		return true
	default:
		return false
	}
}

func (c *Controller) readSteerCandidate(st *sessioninbox.Store, id string) (sessioninbox.InboxItemMeta, sessioninbox.PromptEnvelope, error) {
	meta, env, err := st.ReadItem(id)
	if err != nil || (meta.State != sessioninbox.StateRunning && meta.State != sessioninbox.StateSteerAccepted && meta.State != sessioninbox.StateSteerConsumed) {
		return meta, env, err
	}
	recovered, err := st.RecoverOrphanedInFlightOwnedBy(c.inbox.ownsItem)
	if err != nil {
		return sessioninbox.InboxItemMeta{}, sessioninbox.PromptEnvelope{}, err
	}
	if recovered > 0 {
		sessioninbox.NoteRecovered(recovered)
	}
	return st.ReadItem(id)
}

func (c *Controller) unlockInboxSteerAdmission(dispatch *bool) {
	c.inbox.admissionMu.Unlock()
	if *dispatch {
		c.maybeDispatchInbox()
	}
}

func inboxSteerLoader(st *sessioninbox.Store, itemID string) func() (string, error) {
	return func() (string, error) {
		_, env, err := st.ReadItem(itemID)
		if err != nil {
			if errors.Is(err, sessioninbox.ErrNotFound) {
				return "", agent.ErrSteerWithdrawn
			}
			return "", err
		}
		text := strings.TrimSpace(env.SubmitText)
		if text == "" {
			text = strings.TrimSpace(env.DisplayText)
		}
		if text == "" {
			return "", fmt.Errorf("inbox item %s has empty body", itemID)
		}
		materialized, images, block, materializeErr := applyInboxReferences(env)
		if materializeErr != nil {
			return "", materializeErr
		}
		if block != "" {
			return "", fmt.Errorf("frozen reference unavailable: %s", block)
		}
		if len(images) > 0 {
			return "", fmt.Errorf("image guidance requires a follow-up turn")
		}
		// This compare-and-transition is the durable hand-off boundary and
		// closes the loader-vs-cancel gap after TrySteerInboxItem returns.
		if err := st.MarkSteerConsumed(itemID); err != nil {
			if errors.Is(err, sessioninbox.ErrNotFound) {
				return "", agent.ErrSteerWithdrawn
			}
			return "", err
		}
		return firstNonEmptyStr(materialized, text), nil
	}
}

// TrySteerInboxItem persists intent=steer (if needed) and attempts mid-turn
// admission. Rejected steers stay queued as follow-up.
//
// The agent loader only captures the item ID and re-reads the blob on consume
// so large steer bodies do not accumulate in the agent heap.
func (c *Controller) TrySteerInboxItem(id string) (sessioninbox.InboxReceipt, error) {
	return c.trySteerInboxItem(id, "")
}

// TrySteerInboxItemForTurn applies an existing durable item only to the exact
// active turn. A stale target falls back to queued-follow-up semantics.
func (c *Controller) TrySteerInboxItemForTurn(turnID, id string) (sessioninbox.InboxReceipt, error) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return sessioninbox.InboxReceipt{}, fmt.Errorf("turnId is required")
	}
	return c.trySteerInboxItem(id, turnID)
}

func (c *Controller) trySteerInboxItem(id, expectedTurnID string) (sessioninbox.InboxReceipt, error) {
	c.inbox.admissionMu.Lock()
	dispatchAfterUnlock := false
	defer c.unlockInboxSteerAdmission(&dispatchAfterUnlock)
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	meta, env, err := c.readSteerCandidate(st, id)
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	// RetryInboxItem may start this item while the frontend holds stale running=true.
	// Treat the follow-up Steer as idempotent: the current turn already owns the
	// durable body, so it must not be applied twice or reported as a false failure.
	if steerAlreadyAdmitted(meta.State) {
		return sessioninbox.InboxReceipt{
			ItemID:      id,
			Disposition: sessioninbox.DispositionSteerAccepted,
			Paused:      st.Snapshot().Paused,
			Capacity:    st.Snapshot().Capacity,
			Idempotent:  true,
		}, nil
	}
	if meta.State != sessioninbox.StateQueued && meta.State != sessioninbox.StateUncertain {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrInvalidState
	}
	snapshot := st.Snapshot()
	if snapshot.Paused {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrPaused
	}
	if meta.State == sessioninbox.StateUncertain {
		if err := st.SetState(id, sessioninbox.StateQueued, ""); err != nil {
			return sessioninbox.InboxReceipt{}, err
		}
	}
	cap := snapshot.Capacity
	c.mu.Lock()
	rotating := c.rotating
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return sessioninbox.InboxReceipt{ItemID: id, Disposition: sessioninbox.DispositionRejectedClosed, Capacity: cap}, nil
	}
	if rotating {
		dispatchAfterUnlock = true
		return sessioninbox.InboxReceipt{ItemID: id, Disposition: sessioninbox.DispositionRejectedRotating, Capacity: cap}, nil
	}
	// Capture only the store pointer + item id. Load body from disk at consume.
	loader := inboxSteerLoader(st, id)
	// Persist the admission boundary before exposing the loader to the agent.
	// Holding c.mu for the short in-memory enqueue serializes active tracking
	// with finishGuardedTurn, so TurnDone cannot overtake an accepted steer.
	c.inbox.trackAdmission(id)
	defer c.inbox.untrackAdmission(id)
	if len(env.FrozenImages) == 0 {
		if err := st.SetState(id, sessioninbox.StateSteerAccepted, ""); err != nil {
			return sessioninbox.InboxReceipt{}, err
		}
	}
	c.mu.Lock()
	turnMatches := true
	if expectedTurnID != "" {
		turnMatches = false
		if ledger := c.turnEventLedger(); ledger != nil {
			turnMatches = ledger.ActiveTurnID() == expectedTurnID
		}
	}
	accepted := turnMatches && !c.closed && !c.rotating && c.running && c.executor != nil && len(env.FrozenImages) == 0 && c.executor.SteerItem(id, loader)
	if accepted {
		c.inbox.mu.Lock()
		c.inbox.trackActive(id)
		c.inbox.mu.Unlock()
	}
	c.mu.Unlock()
	if accepted {
		sessioninbox.NoteSteerAccepted()
		return sessioninbox.InboxReceipt{
			ItemID:      id,
			Disposition: sessioninbox.DispositionSteerAccepted,
			Paused:      st.Snapshot().Paused,
			Capacity:    cap,
		}, nil
	}
	// Rejected: keep as follow-up.
	if len(env.FrozenImages) == 0 {
		if err := st.SetState(id, sessioninbox.StateQueued, ""); err != nil {
			_ = st.ForcePause(true, 1)
			return sessioninbox.InboxReceipt{}, err
		}
	}
	if err := st.ConvertIntent(id, sessioninbox.IntentFollowup); err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	sessioninbox.NoteSteerRejected()
	dispatchAfterUnlock = true
	return sessioninbox.InboxReceipt{
		ItemID:      id,
		Disposition: sessioninbox.DispositionQueuedFollowup,
		Paused:      st.Snapshot().Paused,
		Capacity:    cap,
	}, nil
}
