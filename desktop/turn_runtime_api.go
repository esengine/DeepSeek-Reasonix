package main

import (
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/turnevent"
)

// TurnStartView is the synchronous admission receipt for the new Wails turn
// API. Events remain the streaming authority after admission.
type TurnStartView struct {
	TurnID       string                    `json:"turnId"`
	Status       event.TurnStatus          `json:"status"`
	Disposition  control.SubmitDisposition `json:"disposition"`
	RuntimeEpoch string                    `json:"runtimeEpoch,omitempty"`
	SubmissionID string                    `json:"submissionId,omitempty"`
}

// validatePromptIdentity fences a decision to the runtime and turn that
// rendered its card. It is intentionally shared by every decision surface;
// callers must still resolve the prompt on the same controller instance.
func (a *App) validatePromptIdentity(tabID, turnID, runtimeEpoch string) (control.SessionAPI, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return nil, a.workspaceNotReadyErr(tab)
	}
	status := ctrl.RuntimeStatus()
	if strings.TrimSpace(turnID) == "" || status.TurnID != strings.TrimSpace(turnID) {
		return nil, fmt.Errorf("turn %q is not the active turn for tab %q", turnID, tabID)
	}
	if epoch := strings.TrimSpace(runtimeEpoch); epoch != "" && tab != nil && tab.sink != nil && tab.sink.runtimeEpochSnapshot() != epoch {
		return nil, fmt.Errorf("runtime changed while resolving prompt for tab %q", tabID)
	}
	return ctrl, nil
}

// stoppableCtrl resolves and captures the controller a Stop request targets.
// The request is session-scoped and idle cancellation is idempotent.
func (a *App) stoppableCtrl(tabID, turnID string) (control.SessionAPI, error) {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return nil, a.workspaceNotReadyErr(tab)
	}
	status := ctrl.RuntimeStatus()
	if turnID = strings.TrimSpace(turnID); turnID != status.TurnID {
		slog.Info("desktop: stop targeted a stale turn id; interrupting the active turn", "tab", tabID, "requested", turnID, "active", status.TurnID)
	}
	return ctrl, nil
}

// CancelSessionForTab is the protocol-v2 Stop operation. It captures the tab's
// current controller exactly once, so later tab switches cannot retarget it.
func (a *App) CancelSessionForTab(tabID string) (control.CancelReceipt, error) {
	ctrl, err := a.stoppableCtrl(tabID, "")
	if err != nil {
		return control.CancelReceipt{}, err
	}
	if concrete, ok := ctrl.(*control.Controller); ok {
		return concrete.CancelSessionFrom("user_stop"), nil
	}
	if session, ok := ctrl.(interface{ CancelSession() control.CancelReceipt }); ok {
		return session.CancelSession(), nil
	}
	status := ctrl.RuntimeStatus()
	ctrl.Cancel()
	return control.CancelReceipt{SessionRef: ctrl.SessionPath(), HeadID: agent.BranchID(ctrl.SessionPath()), Accepted: true, AlreadyIdle: !status.Running && !status.PendingPrompt}, nil
}

// StartTurnForTab is the turn-id-aware replacement for SubmitToTab. Existing
// Submit entry points remain compatibility wrappers during the protocol cutover.
func (a *App) StartTurnForTab(tabID, input, submissionID string) (TurnStartView, error) {
	if strings.TrimSpace(submissionID) == "" {
		return TurnStartView{}, fmt.Errorf("submissionId is required")
	}
	if _, ctrl := a.tabAndCtrlByID(tabID); ctrl != nil {
		if identified, ok := ctrl.(*control.Controller); ok {
			receipt, found, err := identified.LookupSubmission(control.SubmissionRequest{ID: submissionID, Input: input, Display: input})
			if err != nil {
				return TurnStartView{}, err
			}
			if found {
				return TurnStartView{TurnID: receipt.TurnID, Status: event.TurnQueued, Disposition: control.SubmitTurnStarted, SubmissionID: submissionID}, nil
			}
		}
	}
	result, err := a.submitToTabResult(tabID, input, false, true, submissionID)
	if err != nil {
		return TurnStartView{}, err
	}
	if result.Disposition == control.SubmitManagementHandled {
		return TurnStartView{Disposition: result.Disposition, SubmissionID: submissionID}, nil
	}
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return TurnStartView{}, a.workspaceNotReadyErr(tab)
	}
	turnID := ""
	if admitted, ok := ctrl.(interface{ TurnIDForSubmission(string) string }); ok {
		turnID = admitted.TurnIDForSubmission(submissionID)
	}
	if strings.TrimSpace(turnID) == "" {
		return TurnStartView{}, fmt.Errorf("turn admission did not produce a durable turn id")
	}
	epoch := ""
	if tab != nil && tab.sink != nil {
		epoch = tab.sink.runtimeEpochSnapshot()
	}
	// This is an admission receipt, not a potentially raced runtime snapshot.
	// Ordered events carry every later transition, including a provider that
	// completed before the Wails Promise was delivered.
	return TurnStartView{TurnID: turnID, Status: event.TurnQueued, Disposition: control.SubmitTurnStarted, RuntimeEpoch: epoch, SubmissionID: submissionID}, nil
}

func (a *App) StartTurnForTabWithDrafts(tabID, input, submissionID string, draftIDs []string) (TurnStartView, error) {
	if strings.TrimSpace(submissionID) == "" {
		return TurnStartView{}, fmt.Errorf("submissionId is required")
	}
	req := control.SubmissionRequest{ID: submissionID, Input: input, Display: input, DraftIDs: append([]string(nil), draftIDs...)}
	if _, ctrl := a.tabAndCtrlByID(tabID); ctrl != nil {
		if identified, ok := ctrl.(*control.Controller); ok {
			receipt, found, err := identified.LookupSubmission(req)
			if err != nil {
				return TurnStartView{}, err
			}
			if found {
				return TurnStartView{TurnID: receipt.TurnID, Status: event.TurnQueued, Disposition: control.SubmitTurnStarted, SubmissionID: submissionID}, nil
			}
		}
	}
	admission, ctrl, err := a.beginTabTurn(tabID, true, submissionID)
	if err != nil {
		return TurnStartView{}, a.submissionAdmissionError(tabID, req, err)
	}
	defer admission.abort()
	if err := a.ensureTabTopicIndexedForUserTurn(admission.tab); err != nil {
		return TurnStartView{}, err
	}
	identified, ok := ctrl.(*control.Controller)
	if !ok {
		return TurnStartView{}, fmt.Errorf("unsupported: attachments-v1")
	}
	if _, err := identified.SubmitIdentified(req); err != nil {
		return TurnStartView{}, inboxBridgeError(err)
	}
	admission.finish(ctrl)
	turnID := identified.TurnIDForSubmission(submissionID)
	if strings.TrimSpace(turnID) == "" {
		return TurnStartView{}, fmt.Errorf("turn admission did not produce a durable turn id")
	}
	epoch := ""
	if admission.tab != nil && admission.tab.sink != nil {
		epoch = admission.tab.sink.runtimeEpochSnapshot()
	}
	return TurnStartView{TurnID: turnID, Status: event.TurnQueued, Disposition: control.SubmitTurnStarted, RuntimeEpoch: epoch, SubmissionID: submissionID}, nil
}

// InterruptTurnForTab stops the tab's active work. Stop is a session-level
// request: a turn id from a stale button still interrupts whatever is running
// now, because an unstoppable turn is worse than stopping its replacement.
func (a *App) InterruptTurnForTab(tabID, turnID string) error {
	ctrl, err := a.stoppableCtrl(tabID, turnID)
	if err != nil {
		return err
	}
	if session, ok := ctrl.(interface{ CancelSession() control.CancelReceipt }); ok {
		session.CancelSession()
	} else {
		ctrl.Cancel()
	}
	return nil
}

// InterruptTurnWithInboxItemsForTab is the receipt-capable Stop used by the
// Composer when it also discards queued follow-ups.
func (a *App) InterruptTurnWithInboxItemsForTab(tabID, turnID string, itemIDs []string) (InboxCancelResultView, error) {
	view := InboxCancelResultView{DiscardedItemIDs: []string{}}
	ctrl, err := a.stoppableCtrl(tabID, turnID)
	if err != nil {
		return view, err
	}
	result, err := ctrl.CancelWithInboxItemsResult(itemIDs, "desktop")
	if err != nil {
		return view, inboxBridgeError(err)
	}
	view.DiscardedItemIDs = append(view.DiscardedItemIDs, result.DiscardedItemIDs...)
	view.Warning = result.Warning
	a.emitInboxChanged(tabID)
	return view, nil
}

// AnswerPromptForTab resolves an Ask only when it belongs to the exact active
// turn. Controller-side prompt ids remain independently idempotent.
func (a *App) AnswerPromptForTab(tabID, turnID, promptID string, answers []QuestionAnswer) error {
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return a.workspaceNotReadyErr(tab)
	}
	status := ctrl.RuntimeStatus()
	if strings.TrimSpace(turnID) == "" || status.TurnID != strings.TrimSpace(turnID) {
		return fmt.Errorf("turn %q is not the active turn for tab %q", turnID, tabID)
	}
	// Resolve on the controller instance that passed the turn-id fence. Calling
	// the legacy app wrapper here would re-resolve the tab and could deliver a
	// late answer to a replacement controller after a runtime rebuild.
	out := make([]event.AskAnswer, len(answers))
	for i, answer := range answers {
		out[i] = event.AskAnswer{QuestionID: answer.QuestionID, Selected: answer.Selected}
	}
	if checked, ok := ctrl.(interface {
		AnswerQuestionChecked(string, []event.AskAnswer) error
	}); ok {
		return checked.AnswerQuestionChecked(promptID, out)
	}
	ctrl.AnswerQuestion(promptID, out)
	return nil
}

type turnEventReader interface {
	TurnEventReplay(after uint64) (turnevent.ReplayView, error)
}

type TurnEventReplayView struct {
	Events             []turnevent.Envelope `json:"events"`
	FloorSequence      uint64               `json:"floorSeq"`
	LatestSequence     uint64               `json:"latestSeq"`
	NextAfterSequence  uint64               `json:"nextAfterSeq"`
	HasMore            bool                 `json:"hasMore"`
	ResetRequired      bool                 `json:"resetRequired"`
	TranscriptRevision int64                `json:"transcriptRevision,omitempty"`
	TranscriptDigest   string               `json:"transcriptDigest,omitempty"`
	HeadID             string               `json:"headId,omitempty"`
	LeafMessageID      string               `json:"leafMessageId,omitempty"`
	RuntimeEpoch       string               `json:"runtimeEpoch,omitempty"`
}

// TurnEventsForTab supplies the durable suffix used to repair sequence gaps or
// rebuild after a runtime epoch change.
func (a *App) TurnEventsForTab(tabID string, afterSeq uint64) (TurnEventReplayView, error) {
	empty := TurnEventReplayView{Events: []turnevent.Envelope{}}
	tab, ctrl := a.tabAndCtrlByID(tabID)
	if ctrl == nil {
		return empty, a.workspaceNotReadyErr(tab)
	}
	reader, ok := ctrl.(turnEventReader)
	if !ok {
		return empty, fmt.Errorf("turn event replay is unavailable")
	}
	// Re-check the controller under the app lock before sampling the epoch.
	// This prevents pairing an old controller with a replacement runtime after
	// a session rebind races tabAndCtrlByID.
	epoch := ""
	a.mu.RLock()
	bound := tab != nil && a.tabs[tabID] == tab && tab.Ctrl == ctrl
	if bound && tab.sink != nil {
		epoch = tab.sink.runtimeEpochSnapshot()
	}
	a.mu.RUnlock()
	if !bound {
		return empty, fmt.Errorf("runtime changed while binding turn event replay")
	}
	replay, err := reader.TurnEventReplay(afterSeq)
	if replay.Events == nil {
		replay.Events = []turnevent.Envelope{}
	}
	return TurnEventReplayView{
		Events: replay.Events, FloorSequence: replay.FloorSequence,
		LatestSequence: replay.LatestSequence, NextAfterSequence: replay.NextAfterSequence,
		HasMore: replay.HasMore, ResetRequired: replay.ResetRequired,
		TranscriptRevision: replay.TranscriptRevision, TranscriptDigest: replay.TranscriptDigest,
		HeadID: replay.HeadID, LeafMessageID: replay.LeafMessageID,
		RuntimeEpoch: epoch,
	}, err
}

var _ control.SessionAPI = (*control.Controller)(nil)
