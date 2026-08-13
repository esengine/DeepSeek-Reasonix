package main

import (
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/control"
	"reasonix/internal/sessioninbox"
)

const inboxWailsErrorPrefix = "reasonix_error:"

type inboxCodedError struct {
	code  string
	cause error
}

func (e *inboxCodedError) Error() string { return inboxWailsErrorPrefix + e.code }
func (e *inboxCodedError) Unwrap() error { return e.cause }

// inboxWailsError keeps backend errors machine-stable across the Wails boundary.
// The frontend translates known product states at display time; unknown errors
// stay untouched so useful diagnostic details are not discarded.
func inboxWailsError(err error) error {
	if err == nil {
		return nil
	}
	known := []struct {
		target error
		code   string
	}{
		{sessioninbox.ErrCapacityItems, "inbox_capacity_items"},
		{sessioninbox.ErrCapacityBytes, "inbox_capacity_bytes"},
		{sessioninbox.ErrItemTooLarge, "inbox_item_too_large"},
		{sessioninbox.ErrNotFound, "inbox_item_not_found"},
		{sessioninbox.ErrInvalidState, "inbox_invalid_state"},
		{sessioninbox.ErrSchemaReadonly, "inbox_schema_readonly"},
		{sessioninbox.ErrClosed, "inbox_closed"},
		{sessioninbox.ErrEmpty, "inbox_empty"},
		{sessioninbox.ErrPaused, "inbox_paused"},
		{sessioninbox.ErrIdempotencyConflict, "inbox_idempotency_conflict"},
	}
	for _, item := range known {
		if errors.Is(err, item.target) {
			return &inboxCodedError{code: item.code, cause: err}
		}
	}
	switch {
	case err.Error() == "channel session is read-only":
		return &inboxCodedError{code: "channel_read_only", cause: err}
	case err.Error() == "workspace is still starting":
		return &inboxCodedError{code: "workspace_starting", cause: err}
	case strings.HasPrefix(err.Error(), "workspace failed to start:"):
		return &inboxCodedError{code: "workspace_start_failed", cause: err}
	default:
		return err
	}
}

// InboxItemView is the Wails-facing metadata row (never full body).
type InboxItemView struct {
	ID          string `json:"id"`
	Intent      string `json:"intent"`
	State       string `json:"state"`
	Preview     string `json:"preview"`
	ByteSize    int64  `json:"byteSize"`
	Source      string `json:"source,omitempty"`
	BlockReason string `json:"blockReason,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	Position    int    `json:"position"`
}

// InboxSnapshotView is the Wails-facing queue snapshot.
type InboxSnapshotView struct {
	Revision    int64           `json:"revision"`
	Paused      bool            `json:"paused"`
	Recovered   bool            `json:"recovered"`
	RecoveredN  int             `json:"recoveredCount,omitempty"`
	SessionPath string          `json:"sessionPath,omitempty"`
	Items       []InboxItemView `json:"items"`
	ItemsCount  int             `json:"itemsCount"`
	Bytes       int64           `json:"bytes"`
	MaxItems    int             `json:"maxItems"`
	MaxBytes    int64           `json:"maxBytes"`
}

// InboxReceiptView is returned after durable enqueue/steer.
type InboxReceiptView struct {
	ItemID      string `json:"itemId"`
	Disposition string `json:"disposition"`
	Position    int    `json:"position"`
	Paused      bool   `json:"paused"`
	Idempotent  bool   `json:"idempotent,omitempty"`
	Error       string `json:"error,omitempty"`
}

// InboxEnvelopeView is the full body for the editor (fetched by id only).
type InboxEnvelopeView struct {
	ID          string `json:"id"`
	DisplayText string `json:"displayText"`
	RawText     string `json:"rawText"`
	SubmitText  string `json:"submitText"`
}

func inboxSnapshotView(snap sessioninbox.InboxSnapshot) InboxSnapshotView {
	items := make([]InboxItemView, 0, len(snap.Items))
	for i, it := range snap.Items {
		items = append(items, InboxItemView{
			ID:          it.ID,
			Intent:      string(it.Intent),
			State:       string(it.State),
			Preview:     it.Preview,
			ByteSize:    it.ByteSize,
			Source:      it.Source,
			BlockReason: it.BlockReason,
			CreatedAt:   it.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Position:    i + 1,
		})
	}
	return InboxSnapshotView{
		Revision:    snap.Revision,
		Paused:      snap.Paused,
		Recovered:   snap.Recovered,
		RecoveredN:  snap.RecoveredN,
		SessionPath: snap.SessionPath,
		Items:       items,
		ItemsCount:  len(items),
		Bytes:       snap.Capacity.Bytes,
		MaxItems:    snap.Capacity.MaxItems,
		MaxBytes:    snap.Capacity.MaxBytes,
	}
}

func (a *App) inboxCtrl(tabID string) (control.SessionAPI, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if a.tabIsReadOnly(tab) {
		return nil, inboxWailsError(readOnlyChannelErr())
	}
	if ctrl == nil {
		return nil, inboxWailsError(a.workspaceNotReadyErr(tab))
	}
	return ctrl, nil
}

// InboxSnapshot returns durable inbox metadata for a tab (no bodies).
func (a *App) InboxSnapshot(tabID string) (InboxSnapshotView, error) {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return InboxSnapshotView{}, err
	}
	return inboxSnapshotView(ctrl.InboxSnapshot()), nil
}

// EnqueueInboxFollowup durably queues a follow-up for the tab.
func (a *App) EnqueueInboxFollowup(tabID, display, submit, idempotency string) (InboxReceiptView, error) {
	return a.enqueueInbox(tabID, sessioninbox.IntentFollowup, display, submit, nil, idempotency, false)
}

// EnqueueInboxFollowupWithInvocations preserves rich-composer Skill/Subagent
// entities in the durable envelope instead of degrading them to slash text.
func (a *App) EnqueueInboxFollowupWithInvocations(tabID, display, submit string, invocations []InvocationRequest, idempotency string) (InboxReceiptView, error) {
	return a.enqueueInbox(tabID, sessioninbox.IntentFollowup, display, submit, invocations, idempotency, false)
}

// EnqueueInboxSteer durably queues and attempts mid-turn steer.
func (a *App) EnqueueInboxSteer(tabID, display, submit, idempotency string) (InboxReceiptView, error) {
	return a.enqueueInbox(tabID, sessioninbox.IntentSteer, display, submit, nil, idempotency, true)
}

// SteerInboxItem attempts to apply an existing durable queue item to the
// current turn. It never creates a second entry for the same instruction.
func (a *App) SteerInboxItem(tabID, itemID string) (InboxReceiptView, error) {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return InboxReceiptView{}, err
	}
	rec, err := ctrl.TrySteerInboxItem(strings.TrimSpace(itemID))
	if err != nil {
		err = inboxWailsError(err)
		return InboxReceiptView{Error: err.Error()}, err
	}
	a.emitInboxChanged(tabID)
	return InboxReceiptView{
		ItemID:      rec.ItemID,
		Disposition: string(rec.Disposition),
		Position:    rec.Position,
		Paused:      rec.Paused,
		Idempotent:  rec.Idempotent,
	}, nil
}

// CancelTabWithInboxItems cancels the turn and atomically discards only the
// durable pending items currently shown by that tab's Composer.
func (a *App) CancelTabWithInboxItems(tabID string, itemIDs []string) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if err := ctrl.CancelWithInboxItems(itemIDs, "desktop"); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

func (a *App) enqueueInbox(tabID string, intent sessioninbox.InboxIntent, display, submit string, invocations []InvocationRequest, idempotency string, trySteer bool) (InboxReceiptView, error) {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return InboxReceiptView{}, err
	}
	if ensurer, ok := ctrl.(interface{ EnsureSessionPath() }); ok {
		ensurer.EnsureSessionPath()
	}
	submit = strings.TrimSpace(submit)
	display = strings.TrimSpace(display)
	if submit == "" && len(invocations) == 0 {
		submit = display
	}
	if display == "" {
		display = submit
	}
	req := control.InboxRequest{
		Intent:      intent,
		Display:     display,
		Raw:         submit,
		Submit:      submit,
		Source:      "desktop",
		Idempotency: strings.TrimSpace(idempotency),
		Invocations: controlInvocationRequests(invocations),
	}
	var rec sessioninbox.InboxReceipt
	if trySteer {
		rec, err = ctrl.TryEnqueueAndSteer(req)
	} else {
		rec, err = ctrl.TryEnqueueFollowup(req)
	}
	if err != nil {
		err = inboxWailsError(err)
		return InboxReceiptView{Error: err.Error()}, err
	}
	a.emitInboxChanged(tabID)
	return InboxReceiptView{
		ItemID:      rec.ItemID,
		Disposition: string(rec.Disposition),
		Position:    rec.Position,
		Paused:      rec.Paused,
		Idempotent:  rec.Idempotent,
	}, nil
}

// ReadInboxItem returns the full envelope for editing.
func (a *App) ReadInboxItem(tabID, id string) (InboxEnvelopeView, error) {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return InboxEnvelopeView{}, err
	}
	meta, env, err := ctrl.ReadInboxItem(id)
	if err != nil {
		return InboxEnvelopeView{}, inboxWailsError(err)
	}
	return InboxEnvelopeView{
		ID:          meta.ID,
		DisplayText: env.DisplayText,
		RawText:     env.RawText,
		SubmitText:  env.SubmitText,
	}, nil
}

// UpdateInboxItem rewrites a durable entry and re-freezes refs.
func (a *App) UpdateInboxItem(tabID, id, display, submit string) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if _, err := ctrl.UpdateInboxItem(id, display, submit, submit); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

// DeleteInboxItem removes a durable entry.
func (a *App) DeleteInboxItem(tabID, id string) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if err := ctrl.DeleteInboxItem(id); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

// MoveInboxItem reorders (toIndex is 0-based).
func (a *App) MoveInboxItem(tabID, id string, toIndex int) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if err := ctrl.MoveInboxItem(id, toIndex); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

// SetInboxPaused pauses or resumes dispatch.
func (a *App) SetInboxPaused(tabID string, paused bool) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if err := ctrl.SetInboxPaused(paused); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

// RetryInboxItem resets uncertain/blocked items to queued.
func (a *App) RetryInboxItem(tabID, id string) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if err := ctrl.RetryInboxItem(id); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

// RefreshInboxReferences re-freezes @-refs for an item.
func (a *App) RefreshInboxItem(tabID, id string) error {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return err
	}
	if err := ctrl.RefreshInboxReferences(id); err != nil {
		return inboxWailsError(err)
	}
	a.emitInboxChanged(tabID)
	return nil
}

// SteerForTab still works for compatibility; prefer EnqueueInboxSteer so the
// guidance is durable before admission.
func (a *App) emitInboxChanged(tabID string) {
	if a == nil || a.ctx == nil {
		return
	}
	runtimeEventsEmitFallback(a.ctx, "InboxChanged", map[string]string{"tabId": tabID})
}

// ClearSessionConfirm checks for a non-empty inbox before clear.
func (a *App) InboxHasItems(tabID string) (bool, error) {
	ctrl, err := a.inboxCtrl(tabID)
	if err != nil {
		return false, err
	}
	return len(ctrl.InboxSnapshot().Items) > 0, nil
}

// FormatInboxRecoveryNotice builds the recovery banner text.
func FormatInboxRecoveryNotice(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("Recovered %d pending instruction(s). Inbox is paused — review before resuming.", n)
}
