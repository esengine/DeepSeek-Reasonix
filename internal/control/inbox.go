package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
)

// TurnAdmission is the exported classification of TrySubmitInboxItem /
// TrySteerInboxItem results.
type TurnAdmission string

const (
	AdmissionStarted          TurnAdmission = "started"
	AdmissionSteerAccepted    TurnAdmission = "steer_accepted"
	AdmissionQueuedFollowup   TurnAdmission = "queued_followup"
	AdmissionRejectedBusy     TurnAdmission = "rejected_busy"
	AdmissionRejectedRotating TurnAdmission = "rejected_rotating"
	AdmissionRejectedClosed   TurnAdmission = "rejected_closed"
	AdmissionRejectedCapacity TurnAdmission = "rejected_capacity"
)

// InboxRequest is the frontend-facing enqueue payload.
type InboxRequest struct {
	Intent      sessioninbox.InboxIntent
	Display     string
	Raw         string
	Submit      string
	Format      string
	Source      string
	Idempotency string
	Invocations []InvocationRequest
	Extra       map[string]string
	// FreezeRefs lists workspace-relative paths to freeze at enqueue time.
	FreezeRefs []string
}

// Inbox port on SessionAPI.
type Inbox interface {
	EnqueueInbox(req InboxRequest) (sessioninbox.InboxReceipt, error)
	InboxSnapshot() sessioninbox.InboxSnapshot
	ReadInboxItem(id string) (sessioninbox.InboxItemMeta, sessioninbox.PromptEnvelope, error)
	UpdateInboxItem(id string, display, raw, submit string) (sessioninbox.InboxItemMeta, error)
	AppendInboxItem(id, text, idempotency string, extra map[string]string) (sessioninbox.InboxItemMeta, error)
	DeleteInboxItem(id string) error
	CancelWithInboxItems(ids []string, source string) error
	MoveInboxItem(id string, toIndex int) error
	SetInboxPaused(paused bool) error
	RetryInboxItem(id string) error
	RefreshInboxReferences(id string) error
	TrySubmitInboxItem(id string) (sessioninbox.InboxReceipt, error)
	RunInboxTurn(ctx context.Context, id string) error
	TrySteerInboxItem(id string) (sessioninbox.InboxReceipt, error)
	TryEnqueueAndSteer(req InboxRequest) (sessioninbox.InboxReceipt, error)
	TryEnqueueFollowup(req InboxRequest) (sessioninbox.InboxReceipt, error)
}

// Compile-time port satisfaction.
var _ Inbox = (*Controller)(nil)

// inboxState is controller-owned inbox wiring (disk store + active items).
type inboxState struct {
	mu    sync.Mutex
	store *sessioninbox.Store
	// activeItemIDs includes the running follow-up and every accepted steer.
	// TurnDone durable-acks the set so multi-steer rounds leave no orphans.
	activeItemIDs map[string]struct{}
	dispatching   bool
	// beforePreparedAdmission is a deterministic test hook for the gap between
	// durable preparation and Controller admission. Production leaves it nil.
	beforePreparedAdmission func()
}

func (s *inboxState) trackActive(id string) {
	if s == nil || id == "" {
		return
	}
	if s.activeItemIDs == nil {
		s.activeItemIDs = make(map[string]struct{})
	}
	s.activeItemIDs[id] = struct{}{}
}

func (s *inboxState) untrackActive(id string) {
	if s == nil || s.activeItemIDs == nil || id == "" {
		return
	}
	delete(s.activeItemIDs, id)
}

func (s *inboxState) takeActive() []string {
	if s == nil || len(s.activeItemIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.activeItemIDs))
	for id := range s.activeItemIDs {
		out = append(out, id)
	}
	s.activeItemIDs = nil
	return out
}

func (s *inboxState) clearActive() {
	if s == nil {
		return
	}
	s.activeItemIDs = nil
}

func (c *Controller) ensureInbox() (*sessioninbox.Store, error) {
	path := c.SessionPath()
	if path == "" {
		return nil, fmt.Errorf("inbox requires a persisted session path")
	}
	c.inbox.mu.Lock()
	defer c.inbox.mu.Unlock()
	if c.inbox.store != nil && c.inbox.store.SessionPath() == path {
		return c.inbox.store, nil
	}
	if c.inbox.store != nil {
		c.inbox.store.Close()
		c.inbox.store = nil
	}
	st, err := sessioninbox.Open(path, sessioninbox.Limits{})
	if err != nil {
		return nil, err
	}
	c.inbox.store = st
	snap := st.Snapshot()
	if snap.Recovered && snap.RecoveredN > 0 {
		c.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Code:  "inbox_recovered",
			Text:  fmt.Sprintf("Recovered %d pending instruction(s). Inbox is paused — review with /queue before resuming.", snap.RecoveredN),
		})
		sessioninbox.NoteRecovered(snap.RecoveredN)
	}
	return st, nil
}

// rebindInbox opens the inbox for the current session path. Safe across
// NewSession/Resume/SetSessionPath; does not copy items on fork.
func (c *Controller) rebindInbox() {
	path := c.SessionPath()
	c.inbox.mu.Lock()
	defer c.inbox.mu.Unlock()
	if c.inbox.store != nil {
		if path != "" && c.inbox.store.SessionPath() == path {
			return
		}
		// Pause the old session's queue so it is not auto-run if reopened.
		_ = c.inbox.store.SetPaused(true)
		c.inbox.store.Close()
		c.inbox.store = nil
		c.inbox.clearActive()
	}
	if path == "" {
		return
	}
	st, err := sessioninbox.Open(path, sessioninbox.Limits{})
	if err != nil {
		slog.Warn("controller: open session inbox", "err", err, "path", path)
		return
	}
	c.inbox.store = st
	snap := st.Snapshot()
	if snap.Recovered && snap.RecoveredN > 0 {
		// Emit after unlock via deferred sink call would race; emit here.
		go func(n int) {
			c.sink.Emit(event.Event{
				Kind:  event.Notice,
				Level: event.LevelWarn,
				Code:  "inbox_recovered",
				Text:  fmt.Sprintf("Recovered %d pending instruction(s). Inbox is paused — review with /queue before resuming.", n),
			})
		}(snap.RecoveredN)
		sessioninbox.NoteRecovered(snap.RecoveredN)
	}
}

func (c *Controller) pauseInboxOnRotate() {
	c.inbox.mu.Lock()
	st := c.inbox.store
	c.inbox.mu.Unlock()
	if st != nil {
		_ = st.SetPaused(true)
	}
}

// EnqueueInbox durably queues an instruction. Only returns a receipt after
// blob+manifest commit. Does not auto-start a turn (call TrySubmit / dispatcher).
func (c *Controller) EnqueueInbox(req InboxRequest) (sessioninbox.InboxReceipt, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	submit := strings.TrimSpace(firstNonEmptyStr(req.Submit, req.Raw))
	if submit == "" && len(req.Invocations) == 0 {
		submit = strings.TrimSpace(req.Display)
	}
	if submit == "" && len(req.Invocations) == 0 {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrEmpty
	}
	display := firstNonEmptyStr(req.Display, submit)
	raw := firstNonEmptyStr(req.Raw, submit)
	env := sessioninbox.PromptEnvelope{
		DisplayText:  display,
		RawText:      raw,
		SubmitText:   submit,
		Format:       req.Format,
		Source:       req.Source,
		Idempotency:  req.Idempotency,
		ExplicitRefs: append([]string(nil), req.FreezeRefs...),
		Invocations:  sessionInboxInvocations(req.Invocations),
		Extra:        maps.Clone(req.Extra),
	}
	env.FrozenRefBlock, env.FrozenImages, env.ReferenceErrors = c.freezeInboxReferences(context.Background(), submit, req.FreezeRefs)
	intent := req.Intent
	if intent != sessioninbox.IntentSteer {
		intent = sessioninbox.IntentFollowup
	}
	rec, err := st.Enqueue(sessioninbox.EnqueueRequest{
		Intent:      intent,
		Envelope:    env,
		Source:      req.Source,
		Idempotency: req.Idempotency,
		SessionID:   c.parentSessionID(),
	})
	if err != nil {
		if errors.Is(err, sessioninbox.ErrCapacityItems) || errors.Is(err, sessioninbox.ErrCapacityBytes) || errors.Is(err, sessioninbox.ErrItemTooLarge) {
			sessioninbox.NoteCapacityReject()
		} else {
			sessioninbox.NoteTxFail()
		}
		return sessioninbox.InboxReceipt{}, err
	}
	if !rec.Idempotent && len(env.ReferenceErrors) > 0 {
		reason := strings.Join(env.ReferenceErrors, "; ")
		if stateErr := st.SetState(rec.ItemID, sessioninbox.StateBlocked, reason); stateErr != nil {
			return sessioninbox.InboxReceipt{}, stateErr
		}
		if pauseErr := st.SetPaused(true); pauseErr != nil {
			return sessioninbox.InboxReceipt{}, pauseErr
		}
		rec.Paused = true
	}
	sessioninbox.NoteEnqueue(int64(len(env.SubmitText)))
	return rec, nil
}

func (c *Controller) InboxSnapshot() sessioninbox.InboxSnapshot {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxSnapshot{}
	}
	return st.Snapshot()
}

func (c *Controller) ReadInboxItem(id string) (sessioninbox.InboxItemMeta, sessioninbox.PromptEnvelope, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxItemMeta{}, sessioninbox.PromptEnvelope{}, err
	}
	return st.ReadItem(id)
}

func (c *Controller) UpdateInboxItem(id, display, raw, submit string) (sessioninbox.InboxItemMeta, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxItemMeta{}, err
	}
	submit = strings.TrimSpace(firstNonEmptyStr(submit, raw, display))
	display = firstNonEmptyStr(display, submit)
	raw = firstNonEmptyStr(raw, submit)
	_, previous, err := st.ReadItem(id)
	if err != nil {
		return sessioninbox.InboxItemMeta{}, err
	}
	env := sessioninbox.PromptEnvelope{
		DisplayText:  display,
		RawText:      raw,
		SubmitText:   submit,
		Format:       previous.Format,
		Source:       previous.Source,
		ExplicitRefs: append([]string(nil), previous.ExplicitRefs...),
		Invocation:   previous.Invocation,
		Invocations:  append([]sessioninbox.StructuredInvocation(nil), previous.Invocations...),
		Attachments:  append([]string(nil), previous.Attachments...),
		Extra:        maps.Clone(previous.Extra),
	}
	env.FrozenRefBlock, env.FrozenImages, env.ReferenceErrors = c.freezeInboxReferences(context.Background(), submit, env.ExplicitRefs)
	updated, err := st.UpdateItem(id, env)
	if err != nil {
		return sessioninbox.InboxItemMeta{}, err
	}
	if len(env.ReferenceErrors) > 0 {
		reason := strings.Join(env.ReferenceErrors, "; ")
		if err := st.SetState(id, sessioninbox.StateBlocked, reason); err != nil {
			return sessioninbox.InboxItemMeta{}, err
		}
		_ = st.SetPaused(true)
		updated.State = sessioninbox.StateBlocked
		updated.BlockReason = reason
	}
	return updated, nil
}

// AppendInboxItem atomically merges collect-mode text and binds the inbound
// platform message ID as an idempotency alias for the existing durable item.
func (c *Controller) AppendInboxItem(id, text, idempotency string, extra map[string]string) (sessioninbox.InboxItemMeta, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxItemMeta{}, err
	}
	_, previous, err := st.ReadItem(id)
	if err != nil {
		return sessioninbox.InboxItemMeta{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return sessioninbox.InboxItemMeta{}, sessioninbox.ErrEmpty
	}
	merged := strings.TrimSpace(previous.SubmitText)
	if merged != "" {
		merged += "\n" + text
	} else {
		merged = text
	}
	env := previous
	env.DisplayText = merged
	env.RawText = merged
	env.SubmitText = merged
	if len(extra) > 0 {
		env.Extra = maps.Clone(extra)
	}
	env.FrozenRefBlock, env.FrozenImages, env.ReferenceErrors = c.freezeInboxReferences(context.Background(), merged, env.ExplicitRefs)
	aliasEnv := sessioninbox.PromptEnvelope{
		DisplayText: text,
		RawText:     text,
		SubmitText:  text,
		Source:      previous.Source,
		Extra:       maps.Clone(extra),
	}
	updated, err := st.UpdateItemWithIdempotency(id, env, idempotency, aliasEnv)
	if err != nil {
		return sessioninbox.InboxItemMeta{}, err
	}
	if len(env.ReferenceErrors) > 0 {
		reason := strings.Join(env.ReferenceErrors, "; ")
		if err := st.SetState(id, sessioninbox.StateBlocked, reason); err != nil {
			return sessioninbox.InboxItemMeta{}, err
		}
		_ = st.SetPaused(true)
		updated.State = sessioninbox.StateBlocked
		updated.BlockReason = reason
	}
	return updated, nil
}

func (c *Controller) DeleteInboxItem(id string) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	return st.DeleteItem(id)
}

// CancelWithInboxItems stops the active turn and discards only the durable
// pending items explicitly owned by the cancelling frontend. Admission is
// paused around the batch deletion so TurnDone cannot race a cancelled item
// into a new provider turn. Unrelated inbox items remain intact.
func (c *Controller) CancelWithInboxItems(ids []string, source string) error {
	st, err := c.ensureInbox()
	if err != nil {
		c.Cancel()
		return err
	}
	wasPaused := st.Snapshot().Paused
	if err := st.SetPaused(true); err != nil {
		c.Cancel()
		return err
	}
	if err := st.DiscardPendingItemsOwned(ids, strings.TrimSpace(source)); err != nil {
		// Keep the inbox paused for inspection if an item already crossed the
		// admission boundary. Cancellation still stops that in-flight turn.
		c.Cancel()
		return err
	}
	c.Cancel()
	if !wasPaused {
		if err := st.SetPaused(false); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) MoveInboxItem(id string, toIndex int) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	return st.MoveItem(id, toIndex)
}

func (c *Controller) SetInboxPaused(paused bool) error {
	return c.setInboxPaused(paused, true)
}

// SetInboxPausedPassive changes pause state without starting a background turn.
// Blocking transports such as Bot own their render sink and drain explicitly.
func (c *Controller) SetInboxPausedPassive(paused bool) error {
	return c.setInboxPaused(paused, false)
}

func (c *Controller) setInboxPaused(paused, dispatch bool) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	if err := st.SetPaused(paused); err != nil {
		return err
	}
	if paused {
		sessioninbox.NotePaused()
	} else if dispatch {
		// On resume, try to dispatch if idle.
		c.maybeDispatchInbox()
	}
	return nil
}

func (c *Controller) RetryInboxItem(id string) error {
	return c.retryInboxItem(id, true)
}

// RetryInboxItemPassive requeues an item without detached background dispatch.
func (c *Controller) RetryInboxItemPassive(id string) error {
	return c.retryInboxItem(id, false)
}

func (c *Controller) retryInboxItem(id string, dispatch bool) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	if err := st.RetryItem(id); err != nil {
		return err
	}
	if dispatch {
		c.maybeDispatchInbox()
	}
	return nil
}

func (c *Controller) RefreshInboxReferences(id string) error {
	st, err := c.ensureInbox()
	if err != nil {
		return err
	}
	meta, env, err := st.ReadItem(id)
	if err != nil {
		return err
	}
	_ = meta
	env.Refs = nil
	env.FrozenRefBlock, env.FrozenImages, env.ReferenceErrors = c.freezeInboxReferences(context.Background(), env.SubmitText, env.ExplicitRefs)
	_, err = st.UpdateItem(id, env)
	if err == nil && len(env.ReferenceErrors) > 0 {
		reason := strings.Join(env.ReferenceErrors, "; ")
		err = st.SetState(id, sessioninbox.StateBlocked, reason)
		_ = st.SetPaused(true)
	}
	return err
}

// TrySteerInboxItem persists intent=steer (if needed) and attempts mid-turn
// admission. Rejected steers stay queued as follow-up.
//
// The agent loader only captures the item ID and re-reads the blob on consume
// so large steer bodies do not accumulate in the agent heap.
func (c *Controller) TrySteerInboxItem(id string) (sessioninbox.InboxReceipt, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	meta, env, err := st.ReadItem(id)
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	if meta.State != sessioninbox.StateQueued && meta.State != sessioninbox.StateSteerAccepted {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrInvalidState
	}
	if meta.State == sessioninbox.StateSteerAccepted {
		return sessioninbox.InboxReceipt{
			ItemID:      id,
			Disposition: sessioninbox.DispositionSteerAccepted,
			Paused:      st.Snapshot().Paused,
			Capacity:    st.Snapshot().Capacity,
			Idempotent:  true,
		}, nil
	}
	snapshot := st.Snapshot()
	if snapshot.Paused {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrPaused
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
	if len(env.FrozenImages) == 0 {
		if err := st.SetState(id, sessioninbox.StateSteerAccepted, ""); err != nil {
			return sessioninbox.InboxReceipt{}, err
		}
	}
	c.mu.Lock()
	accepted := !c.closed && !c.rotating && c.running && c.executor != nil && len(env.FrozenImages) == 0 && c.executor.SteerItem(id, loader)
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
	return sessioninbox.InboxReceipt{
		ItemID:      id,
		Disposition: sessioninbox.DispositionQueuedFollowup,
		Paused:      st.Snapshot().Paused,
		Capacity:    cap,
	}, nil
}

// TrySubmitInboxItem admits a queued item as a new turn when the session is idle.
func (c *Controller) TrySubmitInboxItem(id string) (sessioninbox.InboxReceipt, error) {
	st, err := c.ensureInbox()
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	meta, env, err := st.ReadItem(id)
	if err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	if meta.State != sessioninbox.StateQueued {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrInvalidState
	}
	if st.Snapshot().Paused {
		return sessioninbox.InboxReceipt{}, sessioninbox.ErrPaused
	}
	run, block, materializeErr := c.prepareInboxRun(env)
	if materializeErr != nil {
		return sessioninbox.InboxReceipt{}, materializeErr
	}
	if block != "" {
		_ = st.SetState(id, sessioninbox.StateBlocked, block)
		_ = st.SetPaused(true)
		return sessioninbox.InboxReceipt{}, fmt.Errorf("%w: %s", sessioninbox.ErrInvalidState, block)
	}
	// Persist the in-flight state before admission. Active tracking is installed
	// only after Controller admission is reserved and before the turn can finish.
	if err := st.ClaimItem(id); err != nil {
		return sessioninbox.InboxReceipt{}, err
	}
	c.inbox.mu.Lock()
	beforeAdmission := c.inbox.beforePreparedAdmission
	c.inbox.mu.Unlock()
	if beforeAdmission != nil {
		beforeAdmission()
	}
	// Start the classified envelope directly. Submit would parse @tokens again
	// and mix live workspace bytes with the enqueue-time snapshot.
	result := c.submitPreparedInboxTurn(id, run)
	if result != turnStarted {
		if err := st.SetState(id, sessioninbox.StateQueued, ""); err != nil {
			_ = st.ForcePause(true, 1)
			return sessioninbox.InboxReceipt{}, err
		}
		return c.receiptForAdmissionResult(id, st, result), nil
	}
	return sessioninbox.InboxReceipt{
		ItemID:      id,
		Disposition: sessioninbox.DispositionStarted,
		Capacity:    st.Snapshot().Capacity,
	}, nil
}

func (c *Controller) receiptForAdmissionResult(id string, st *sessioninbox.Store, result admissionResult) sessioninbox.InboxReceipt {
	disposition := sessioninbox.DispositionRejectedBusy
	switch result {
	case turnDroppedClosed:
		disposition = sessioninbox.DispositionRejectedClosed
	case turnDroppedRotating:
		disposition = sessioninbox.DispositionRejectedRotating
	}
	return sessioninbox.InboxReceipt{ItemID: id, Disposition: disposition, Capacity: st.Snapshot().Capacity}
}

// onInboxTurnDone acknowledges durable completion of every active inbox item
// (running follow-up + all steers accepted this turn). Dispatch of the next
// item is deferred until the finishing window closes so admission is not
// rejected as busy.
func (c *Controller) onInboxTurnDone() {
	c.inbox.mu.Lock()
	ids := c.inbox.takeActive()
	st := c.inbox.store
	c.inbox.mu.Unlock()
	if st == nil || len(ids) == 0 {
		return
	}
	// Transcript snapshot is the durable receipt boundary for the whole set.
	if err := c.SnapshotActivity(); err != nil {
		slog.Warn("controller: inbox turn snapshot", "err", err)
		for _, id := range ids {
			_ = st.SetState(id, sessioninbox.StateUncertain, "turn completed but transcript snapshot failed")
		}
		_ = st.SetPaused(true)
		sessioninbox.NoteUncertain()
		return
	}
	ackFailed := false
	for _, id := range ids {
		if err := st.AckDequeue(id); err != nil {
			if errors.Is(err, sessioninbox.ErrNotFound) {
				continue
			}
			slog.Warn("controller: inbox ack dequeue", "err", err, "id", id)
			_ = st.SetState(id, sessioninbox.StateUncertain, "turn completed but inbox acknowledgement failed")
			ackFailed = true
		}
	}
	if ackFailed {
		_ = st.SetPaused(true)
		sessioninbox.NoteUncertain()
	}
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
	_ = st.SetState(itemID, sessioninbox.StateUncertain, "steer accepted but unapplied before turn exit")
	_ = st.SetPaused(true)
	c.inbox.mu.Lock()
	c.inbox.untrackActive(itemID)
	c.inbox.mu.Unlock()
	sessioninbox.NoteUncertain()
}

// onInboxSteerConsumed marks steer_accepted → steer_consumed.
func (c *Controller) onInboxSteerConsumed(itemID string) {
	if itemID == "" {
		return
	}
	st, err := c.ensureInbox()
	if err != nil {
		return
	}
	_ = st.SetState(itemID, sessioninbox.StateSteerConsumed, "")
}

// maybeDispatchInbox admits the next FIFO item when idle, not paused, and no
// approval/ask UI is open.
func (c *Controller) maybeDispatchInbox() {
	c.inbox.mu.Lock()
	if c.inbox.dispatching {
		c.inbox.mu.Unlock()
		return
	}
	c.inbox.dispatching = true
	c.inbox.mu.Unlock()
	defer func() {
		c.inbox.mu.Lock()
		c.inbox.dispatching = false
		c.inbox.mu.Unlock()
	}()

	if c.PendingPrompt() {
		return
	}
	c.mu.Lock()
	busy := c.running || c.finishing || c.rotating || c.closed
	c.mu.Unlock()
	if busy {
		return
	}
	st, err := c.ensureInbox()
	if err != nil {
		return
	}
	meta, ok := st.NextQueued()
	if !ok {
		return
	}
	_, _ = c.TrySubmitInboxItem(meta.ID)
}

// TryEnqueueAndSteer is a convenience for frontends: durable steer then TrySteer.
func (c *Controller) TryEnqueueAndSteer(req InboxRequest) (sessioninbox.InboxReceipt, error) {
	req.Intent = sessioninbox.IntentSteer
	rec, err := c.EnqueueInbox(req)
	if err != nil {
		return rec, err
	}
	return c.TrySteerInboxItem(rec.ItemID)
}

// TryEnqueueFollowup durably queues a follow-up and may dispatch if idle.
func (c *Controller) TryEnqueueFollowup(req InboxRequest) (sessioninbox.InboxReceipt, error) {
	req.Intent = sessioninbox.IntentFollowup
	rec, err := c.EnqueueInbox(req)
	if err != nil {
		return rec, err
	}
	if !c.Running() {
		c.maybeDispatchInbox()
	}
	return rec, nil
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
