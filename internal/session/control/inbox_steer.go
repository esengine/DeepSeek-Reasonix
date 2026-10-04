package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessioninbox"
)

// ErrSteerApplied is the identity of "too late": the running turn has already
// read this guidance, so there is nothing left to take back. A caller has to
// tell it from an ordinary rejected delete, because the answer to the person
// is different - one is "removed", the other is "it is already on its way".
var ErrSteerApplied = errors.New("that guidance has already reached the model")

// DropQueuedSteer takes back guidance a running turn accepted but has not read
// yet. The executor's queue is the only place that can tell that from "already
// on its way", so its answer is what authorizes removing the durable half.
func (c *Controller) DropQueuedSteer(id string) (bool, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	exec := c.executor
	c.mu.Unlock()
	if exec == nil || !exec.DropSteer(id) {
		return false, nil
	}
	c.inbox.untrackActive(id)
	return true, st.CancelAcceptedSteer(id)
}

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

// TrySteerInboxItem persists intent=steer (if needed) and attempts mid-turn
// admission. Rejected steers stay queued as follow-up.
//
// The agent loader only captures the item ID and re-reads the blob on consume
// so large steer bodies do not accumulate in the agent heap.
func (c *Controller) TrySteerInboxItem(id string) (sessioninbox.InboxReceipt, error) {
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
	rotating := c.gate.rotating
	closed := c.gate.closed
	c.mu.Unlock()
	if closed {
		return sessioninbox.InboxReceipt{ItemID: id, Disposition: sessioninbox.DispositionRejectedClosed, Capacity: cap}, nil
	}
	if rotating {
		dispatchAfterUnlock = true
		return sessioninbox.InboxReceipt{ItemID: id, Disposition: sessioninbox.DispositionRejectedRotating, Capacity: cap}, nil
	}
	// Capture only the store pointer + item id. Load body from disk at consume.
	storeRef := st
	itemID := id
	loader := func() (string, error) {
		_, env, err := storeRef.ReadItem(itemID)
		if err != nil {
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
		return firstNonEmptyStr(materialized, text), nil
	}
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
	accepted := !c.gate.closed && !c.gate.rotating && c.gate.running && c.executor != nil && len(env.FrozenImages) == 0 && c.steerItemAs(meta.Origin, id, loader, env.Via)
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

// steerItemAs queues the item under the attribution the manifest recorded, so a
// host-authored item is never delivered as something the user said. Holds c.mu.
func (c *Controller) steerItemAs(origin sessioninbox.PromptOrigin, id string, loader func() (string, error), via *provider.Via) bool {
	if origin.IsHost() {
		return c.executor.SteerHostItem(id, loader)
	}
	return c.executor.SteerItemFrom(id, loader, via)
}

// TrySteer queues mid-turn guidance only when the active agent turn accepts it.
func (c *Controller) TrySteer(text string) bool {
	c.mu.Lock()
	exec := c.executor
	running := c.gate.running
	c.mu.Unlock()
	return running && exec != nil && exec.Steer(text)
}

// Steer is the compatibility path for callers that cannot observe admission.
// Interactive hosts should call TrySteer so a rejected steer remains in their
// draft/queue and can be retried as a regular follow-up.
func (c *Controller) Steer(text string) {
	if c.TrySteer(text) {
		return
	}
	// No active turn accepted the steer: the frontend's runningRef was stale,
	// the turn exited between our running check and the enqueue, or no
	// executor is bound yet. Deliver it as a regular turn instead.
	c.submitSteerFallback(text)
}

// submitSteerFallback records steer text that no active turn accepted as
// unapplied guidance, not as a new task. This compatibility path deliberately
// never opens a provider turn: replaying stale historical guidance as the
// user's current request caused unintended code changes (#7045).
func (c *Controller) submitSteerFallback(text string) admissionResult {
	return c.runGuardedOrPark(func(context.Context) error {
		if c.executor != nil {
			c.executor.RecordUnappliedSteer(text)
		}
		return nil
	})
}

// SteerConsumed returns true when the steer queue is empty after the last consume.
func (c *Controller) SteerConsumed() bool {
	c.mu.Lock()
	exec := c.executor
	c.mu.Unlock()
	if exec != nil {
		return exec.SteerConsumed()
	}
	return true
}

// lockPromptFor acquires the prompt lock, emitting one notice if the wait is
// long enough to look like a hang. It reports false only when ctx ended first;
// the lock is held on true.
func (c *Controller) lockPromptFor(ctx context.Context, kind string) bool {
	acquired := make(chan struct{})
	go func() {
		c.approval.promptMu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return true
	case <-ctx.Done():
	case <-time.After(promptQueueNoticeDelay):
	}
	if ctx.Err() == nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodePromptQueued,
			Text:   "A " + kind + " is waiting for you to answer the prompt ahead of it.",
			Detail: "the assistant asked something while an earlier approval or question was still open; it appears once that one is answered"})
	}
	select {
	case <-acquired:
		return true
	case <-ctx.Done():
		// The lock may still be handed to the goroutine above; release it so the
		// next prompt is not blocked by this abandoned wait.
		go func() {
			<-acquired
			c.approval.promptMu.Unlock()
		}()
		return false
	}
}

func askAnswersHaveSelection(answers []event.AskAnswer) bool {
	for _, answer := range answers {
		if len(answer.Selected) > 0 {
			return true
		}
	}
	return false
}

// onInboxUnappliedSteer keeps accepted-but-unapplied steers for inspection.
func (c *Controller) onInboxUnappliedSteer(itemID string) {
	if itemID == "" {
		return
	}
	st, err := c.ensureInbox()
	if err != nil {
		return
	}
	c.mu.Lock()
	skipped := c.gate.skippedAsk()
	c.mu.Unlock()
	if skipped {
		// The user declined the question, so guidance they queued into it runs
		// next; an item whose body cannot be read back stays held.
		if _, _, readErr := st.ReadItem(itemID); readErr == nil {
			if _, err := st.RequeueUnappliedSteer(itemID); err == nil {
				c.inbox.mu.Lock()
				c.inbox.untrackActive(itemID)
				c.inbox.mu.Unlock()
				return
			}
		}
	}
	_ = st.SetStateCoded(itemID, sessioninbox.StateUncertain, sessioninbox.BlockSteerUnapplied, "steer accepted but unapplied before turn exit")
	_ = st.SetPaused(true)
	c.inbox.mu.Lock()
	c.inbox.untrackActive(itemID)
	c.inbox.mu.Unlock()
	sessioninbox.NoteUncertain()
}
