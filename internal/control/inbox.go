package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

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
	// admissionMu serializes competing admission state machines. Snapshot
	// recovery and completion never hold it across Store I/O.
	admissionMu sync.Mutex
	mu          sync.Mutex
	store       *sessioninbox.Store
	// activeItemIDs includes the running follow-up and every accepted steer.
	// TurnDone durable-acks the set so multi-steer rounds leave no orphans.
	activeItemIDs map[string]struct{}
	// activeOwnership mirrors activeItemIDs for lock-free recovery checks while
	// the Store owns its transaction lock. admittingOwnership covers the narrow
	// durable-claim -> active-registration transition.
	activeOwnership    sync.Map
	admittingOwnership sync.Map
	dispatching        bool
	dispatchPending    bool
	// Retry bookkeeping is guarded by mu. Retries are bounded so a persistent
	// disk or materialization failure cannot create a hot background loop.
	dispatchRetryAttempts  int
	dispatchRetryScheduled bool
	// beforePreparedAdmission is a deterministic test hook for the gap between
	// durable preparation and Controller admission. Production leaves it nil.
	beforePreparedAdmission func()
	// beforeCompletionSnapshot exposes the slow snapshot boundary without
	// changing production behavior.
	beforeCompletionSnapshot func()
	// beforeCompletionAck exposes the ownership-to-ack boundary to race tests.
	beforeCompletionAck func()
	// beforeSnapshotRead exposes the final Store snapshot boundary to lock tests.
	beforeSnapshotRead func()
	// afterDispatchScan exposes the empty-scan boundary for lost-wakeup tests.
	afterDispatchScan func(found bool)
	// beforeDispatchSubmit injects a transient owner-level dispatch failure.
	beforeDispatchSubmit func(itemID string) error
	// scheduleDispatchRetry replaces the production timer in deterministic tests.
	scheduleDispatchRetry func(delay time.Duration, retry func())
}

func (s *inboxState) trackActive(id string) {
	if s == nil || id == "" {
		return
	}
	if s.activeItemIDs == nil {
		s.activeItemIDs = make(map[string]struct{})
	}
	s.activeOwnership.Store(id, struct{}{})
	s.activeItemIDs[id] = struct{}{}
}

func (s *inboxState) untrackActive(id string) {
	if s == nil || id == "" {
		return
	}
	if s.activeItemIDs != nil {
		delete(s.activeItemIDs, id)
	}
	s.activeOwnership.Delete(id)
}

func (s *inboxState) untrackActiveSet(ids []string) {
	if s == nil {
		return
	}
	for _, id := range ids {
		if s.activeItemIDs != nil {
			delete(s.activeItemIDs, id)
		}
		s.activeOwnership.Delete(id)
	}
}

func (s *inboxState) clearActive() {
	if s == nil {
		return
	}
	s.activeItemIDs = nil
	s.activeOwnership.Clear()
}

func (s *inboxState) trackAdmission(id string) {
	if s != nil && id != "" {
		s.admittingOwnership.Store(id, struct{}{})
	}
}

func (s *inboxState) untrackAdmission(id string) {
	if s != nil && id != "" {
		s.admittingOwnership.Delete(id)
	}
}

// ownsItem is intentionally lock-free: Store recovery calls it while holding
// its own transaction lock, and no Store -> Controller lock edge is allowed.
func (s *inboxState) ownsItem(id string) bool {
	if s == nil || id == "" {
		return false
	}
	if _, ok := s.admittingOwnership.Load(id); ok {
		return true
	}
	_, ok := s.activeOwnership.Load(id)
	return ok
}

func (s *inboxState) activeIDs() []string {
	if s == nil || len(s.activeItemIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.activeItemIDs))
	for id := range s.activeItemIDs {
		out = append(out, id)
	}
	return out
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
		// Pending work must remain inspectable if this session is reopened.
		_ = c.inbox.store.PauseIfPending()
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
		_ = st.PauseIfPending()
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
	if recovered, recoverErr := st.RecoverOrphanedInFlightOwnedBy(c.inbox.ownsItem); recoverErr != nil {
		slog.Warn("controller: recover orphaned inbox items", "err", recoverErr)
	} else if recovered > 0 {
		sessioninbox.NoteRecovered(recovered)
	}
	c.inbox.mu.Lock()
	beforeSnapshotRead := c.inbox.beforeSnapshotRead
	c.inbox.mu.Unlock()
	if beforeSnapshotRead != nil {
		beforeSnapshotRead()
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
	if _, recoverErr := st.RecoverOrphanedInFlightOwnedBy(c.inbox.ownsItem); recoverErr != nil {
		slog.Warn("controller: recover inbox item before delete", "err", recoverErr, "id", id)
	}
	err = st.DeleteItem(id)
	if err == nil || errors.Is(err, sessioninbox.ErrNotFound) {
		return nil
	}
	if !errors.Is(err, sessioninbox.ErrInvalidState) {
		return err
	}
	meta, _, readErr := st.ReadItem(id)
	if readErr != nil {
		if errors.Is(readErr, sessioninbox.ErrNotFound) {
			return nil
		}
		return err
	}
	// An accepted-but-unconsumed steer is still waiting from the user's point
	// of view. Roll it back to queued so the same delete path can remove it.
	if meta.State != sessioninbox.StateSteerAccepted {
		return err
	}
	if setErr := st.SetState(id, sessioninbox.StateQueued, ""); setErr != nil {
		return err
	}
	if delErr := st.DeleteItem(id); delErr != nil && !errors.Is(delErr, sessioninbox.ErrNotFound) {
		return delErr
	}
	return nil
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

// TrySubmitInboxItem admits a queued item as a new turn when the session is idle.
func (c *Controller) TrySubmitInboxItem(id string) (sessioninbox.InboxReceipt, error) {
	c.inbox.admissionMu.Lock()
	defer c.inbox.admissionMu.Unlock()
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
	c.inbox.trackAdmission(id)
	defer c.inbox.untrackAdmission(id)
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
	// Keep these IDs published as live ownership while SnapshotActivity runs.
	// Inbox recovery can therefore proceed without waiting on extension hooks,
	// transcript I/O, or the session file lock and will preserve this turn.
	ids := c.inbox.activeIDs()
	st := c.inbox.store
	beforeSnapshot := c.inbox.beforeCompletionSnapshot
	beforeAck := c.inbox.beforeCompletionAck
	c.inbox.mu.Unlock()
	if st == nil || len(ids) == 0 {
		return
	}
	if beforeSnapshot != nil {
		beforeSnapshot()
	}
	// Transcript snapshot is the durable receipt boundary for the whole set.
	if err := c.SnapshotActivity(); err != nil {
		slog.Warn("controller: inbox turn snapshot", "err", err)
		for _, id := range ids {
			_ = st.SetState(id, sessioninbox.StateUncertain, "turn completed but transcript snapshot failed")
		}
		_ = st.SetPaused(true)
		c.inbox.mu.Lock()
		c.inbox.untrackActiveSet(ids)
		c.inbox.mu.Unlock()
		sessioninbox.NoteUncertain()
		return
	}
	// Keep ownership published through every durable acknowledgement. Recovery
	// can run concurrently, sees these IDs as live without a Controller lock,
	// and ownership is removed only after dequeue or uncertain state is durable.
	if beforeAck != nil {
		beforeAck()
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
	c.inbox.mu.Lock()
	c.inbox.untrackActiveSet(ids)
	c.inbox.mu.Unlock()
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

// TryEnqueueAndSteer is a convenience for frontends: durable steer then TrySteer.
func (c *Controller) TryEnqueueAndSteer(req InboxRequest) (sessioninbox.InboxReceipt, error) {
	req.Intent = sessioninbox.IntentSteer
	rec, err := c.EnqueueInbox(req)
	if err != nil {
		return rec, err
	}
	steered, err := c.TrySteerInboxItem(rec.ItemID)
	if errors.Is(err, sessioninbox.ErrPaused) {
		rec.Disposition = sessioninbox.DispositionQueuedFollowup
		rec.Paused = true
		return rec, nil
	}
	if err != nil {
		return rec, err
	}
	return steered, nil
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
