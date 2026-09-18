package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/attachment"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/session"
	"reasonix/internal/sessioninbox"
)

type ComposerTarget struct {
	Kind       string              `json:"kind"`
	DraftID    string              `json:"draftId,omitempty"`
	TabID      string              `json:"tabId,omitempty"`
	Session    *session.SessionRef `json:"session,omitempty"`
	Generation uint64              `json:"generation,omitempty"`
}

type attachmentTargetState struct {
	// Test hook after I/O and before target revalidation; set before use.
	attachmentIOHook    func()
	attachmentJoinHook  func()
	attachmentTargetsMu sync.Mutex
	attachmentTargets   map[string]attachmentTarget
	attachmentStageOps  map[string]*attachmentStageOperation
}

type attachmentStageOperation struct {
	fingerprint string
	done        chan struct{}
	view        DraftImageView
	err         error
}

func (a *App) releaseAttachmentStageOperations(ownerPrefix string) {
	a.attachmentTargetsMu.Lock()
	defer a.attachmentTargetsMu.Unlock()
	for key, op := range a.attachmentStageOps {
		if strings.HasPrefix(key, ownerPrefix) {
			// In-flight operations retain their result until completion so existing
			// waiters can observe the owner cancellation. Completed receipts can be
			// dropped immediately with their credential family.
			select {
			case <-op.done:
				delete(a.attachmentStageOps, key)
			default:
			}
		}
	}
}

type AttachmentTargetView struct {
	Token        string   `json:"token"`
	Capabilities []string `json:"capabilities"`
}

func (a *App) attachmentContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) attachmentOperationContext(target attachmentTarget) context.Context {
	if target.ctx != nil {
		return target.ctx
	}
	return a.attachmentContext()
}

// CaptureAttachmentTarget must run before browser file reads or hashing.
// The opaque token carries no client-selected filesystem authority.
func (a *App) CaptureAttachmentTarget(composer ComposerTarget) (AttachmentTargetView, error) {
	empty := AttachmentTargetView{Capabilities: []string{}}
	target, err := a.attachmentTargetForComposerTarget(composer)
	if err != nil {
		return empty, err
	}
	if target.draftID == "" {
		if _, ok := target.ctrl.(*control.Controller); !ok {
			return empty, fmt.Errorf("unsupported: attachments-v2")
		}
	}
	var id [24]byte
	if _, err := rand.Read(id[:]); err != nil {
		return empty, err
	}
	token := hex.EncodeToString(id[:])
	a.attachmentTargetsMu.Lock()
	defer a.attachmentTargetsMu.Unlock()
	if a.attachmentTargets == nil {
		a.attachmentTargets = make(map[string]attachmentTarget)
	}
	for key, previous := range a.attachmentTargets {
		if !a.attachmentTargetCurrent(previous) {
			if previous.cancel != nil {
				previous.cancel()
			}
			delete(a.attachmentTargets, key)
		}
	}
	if len(a.attachmentTargets) >= 256 {
		return empty, fmt.Errorf("too many outstanding attachment operations")
	}
	if controller, ok := target.ctrl.(*control.Controller); ok {
		target.ctx, target.cancel = controller.NewAttachmentOperationContext(a.attachmentContext())
	} else {
		target.ctx, target.cancel = context.WithCancel(a.attachmentContext())
	}
	a.attachmentTargets[token] = target
	return AttachmentTargetView{Token: token, Capabilities: []string{"attachments-v2"}}, nil
}

func (a *App) attachmentTargetToken(token string) (attachmentTarget, error) {
	a.attachmentTargetsMu.Lock()
	target, ok := a.attachmentTargets[token]
	a.attachmentTargetsMu.Unlock()
	if !ok || !a.attachmentTargetCurrent(target) {
		return attachmentTarget{}, fmt.Errorf("attachment target changed; please retry")
	}
	return target, nil
}

func (a *App) ReleaseAttachmentTarget(token string) {
	a.attachmentTargetsMu.Lock()
	if target, ok := a.attachmentTargets[token]; ok && target.cancel != nil {
		target.cancel()
	}
	delete(a.attachmentTargets, token)
	a.attachmentTargetsMu.Unlock()
}

func (a *App) StageImageForTarget(token, operationID, displayName, mime, dataURL string) (DraftImageView, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return DraftImageView{}, err
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return DraftImageView{}, fmt.Errorf("attachment operationId is required")
	}
	fingerprint, err := stageImageFingerprint(displayName, mime, dataURL)
	if err != nil {
		return DraftImageView{}, err
	}
	key := target.ownerIdentity + "\x00" + operationID
	a.attachmentTargetsMu.Lock()
	if a.attachmentStageOps == nil {
		a.attachmentStageOps = make(map[string]*attachmentStageOperation)
	}
	if existing := a.attachmentStageOps[key]; existing != nil {
		if existing.fingerprint != fingerprint {
			a.attachmentTargetsMu.Unlock()
			return DraftImageView{}, fmt.Errorf("attachment operation conflicts with different input")
		}
		done := existing.done
		a.attachmentTargetsMu.Unlock()
		if a.attachmentJoinHook != nil {
			a.attachmentJoinHook()
		}
		select {
		case <-done:
			return existing.view, existing.err
		case <-a.attachmentOperationContext(target).Done():
			return DraftImageView{}, a.attachmentOperationContext(target).Err()
		}
	}
	count := 0
	for existingKey := range a.attachmentStageOps {
		if strings.HasPrefix(existingKey, target.ownerIdentity+"\x00") {
			count++
		}
	}
	if count >= 256 {
		a.attachmentTargetsMu.Unlock()
		return DraftImageView{}, fmt.Errorf("too many outstanding attachment operations")
	}
	op := &attachmentStageOperation{fingerprint: fingerprint, done: make(chan struct{})}
	a.attachmentStageOps[key] = op
	a.attachmentTargetsMu.Unlock()

	if target.draftID != "" {
		rel, stageErr := control.SaveImageDataURLInRoot(target.root, dataURL)
		if stageErr == nil {
			op.view = DraftImageView{Path: rel, DisplayName: attachment.NormalizeDisplayName(displayName), MIME: strings.ToLower(strings.TrimSpace(mime))}
		}
		op.err = stageErr
	} else {
		op.view, op.err = a.stageImageForTarget(target, displayName, mime, dataURL)
	}
	invalidReason := a.attachmentTargetInvalidReason(target)
	ownerCurrent := invalidReason == ""
	if op.err == nil && !ownerCurrent {
		op.view = DraftImageView{}
		op.err = fmt.Errorf("attachment target changed (%s); please retry", invalidReason)
	}
	a.attachmentTargetsMu.Lock()
	close(op.done)
	if !ownerCurrent {
		delete(a.attachmentStageOps, key)
	}
	a.attachmentTargetsMu.Unlock()
	return op.view, op.err
}

func stageImageFingerprint(displayName, mime, dataURL string) (string, error) {
	const marker = ";base64,"
	before, encoded, ok := strings.Cut(dataURL, marker)
	if !ok || !strings.HasPrefix(before, "data:") {
		return "", fmt.Errorf("unsupported pasted image")
	}
	raw, err := decodeBase64(encoded)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write(raw)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(strings.ToLower(strings.TrimSpace(mime))))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(attachment.NormalizeDisplayName(displayName)))
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (a *App) ReadDraftImageForTarget(token, draftID string) (string, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return "", err
	}
	if target.draftID != "" {
		return control.ImageDataURLInRoot(target.root, draftID)
	}
	return a.readDraftImageForTarget(target, draftID)
}

func (a *App) SavePastedFileForTarget(token, name, dataURL string) (string, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return "", err
	}
	rel, err := control.SaveAttachmentDataURLInRoot(target.root, name, dataURL)
	return a.finishAttachmentWrite(target, rel, err)
}

func (a *App) SaveClipboardImageForTarget(token string) (string, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return "", err
	}
	rel, err := control.SaveClipboardImageInRoot(target.root)
	return a.finishAttachmentWrite(target, rel, err)
}

func (a *App) AttachmentDataURLForTarget(token, path string) (string, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return "", err
	}
	return a.attachmentDataURLForTarget(target, path)
}

func (a *App) AttachDroppedForTarget(token, path string) (DroppedItem, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return DroppedItem{}, err
	}
	return a.attachDroppedForTarget(target, path)
}

func (a *App) RebindDraftImageForTarget(token, draftID string) (DraftImageView, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return DraftImageView{}, err
	}
	c, ok := target.ctrl.(*control.Controller)
	if !ok {
		return DraftImageView{}, fmt.Errorf("unsupported: draft attachment rebind")
	}
	draft, err := c.RebindDraftImage(a.attachmentOperationContext(target), draftID)
	if err != nil {
		return DraftImageView{}, err
	}
	if !a.attachmentTargetCurrent(target) {
		return DraftImageView{}, fmt.Errorf("attachment target changed; please retry")
	}
	return draftImageView(draft), nil
}

func (a *App) ReleaseDraftImageForTarget(token, draftID string) error {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return err
	}
	if c, ok := target.ctrl.(*control.Controller); ok {
		c.ReleaseDraftImage(draftID)
	}
	a.attachmentTargetsMu.Lock()
	for key, op := range a.attachmentStageOps {
		if strings.HasPrefix(key, target.ownerIdentity+"\x00") && op.view.DraftID == draftID {
			delete(a.attachmentStageOps, key)
		}
	}
	a.attachmentTargetsMu.Unlock()
	return nil
}

// StartTurnForAttachmentTarget shares controller admission for all structured
// sources. beginTabTurn holds the runtime replacement barrier during admission.
func (a *App) StartTurnForAttachmentTarget(token, submissionID string, req control.SubmissionRequest) (TurnStartView, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return TurnStartView{}, err
	}
	if submissionID == "" {
		return TurnStartView{}, fmt.Errorf("submissionId is required")
	}
	req.ID = submissionID
	c, ok := target.ctrl.(*control.Controller)
	if !ok {
		return TurnStartView{}, fmt.Errorf("attachment target is not a session")
	}
	if receipt, found, err := c.LookupSubmissionContext(a.attachmentOperationContext(target), req); found || err != nil {
		return TurnStartView{TurnID: receipt.TurnID, SubmissionID: submissionID, Status: event.TurnQueued, Disposition: control.SubmitTurnStarted}, err
	}
	prepared, err := c.PrepareSubmission(a.attachmentOperationContext(target), req)
	if err != nil {
		return TurnStartView{}, err
	}
	admission, ctrl, err := a.beginTabTurn(target.tabID, true, submissionID)
	if err != nil {
		return TurnStartView{}, err
	}
	defer admission.abort()
	if ctrl != target.ctrl || !a.attachmentTargetCurrent(target) {
		return TurnStartView{}, fmt.Errorf("attachment target changed; please retry")
	}
	setup := func() error {
		if req.Goal != "" {
			if err := syncTabGoalToController(ctrl, req.Goal); err != nil {
				return err
			}
			a.mu.Lock()
			target.tab.goal = req.Goal
			target.tab.toolApprovalMode = normalizeToolApprovalMode(req.ToolApprovalMode)
			target.tab.mode = tabModeFromAxes(false, target.tab.toolApprovalMode == control.ToolApprovalYolo)
			a.saveTabsLocked()
			a.mu.Unlock()
			ctrl.SetPlanMode(false)
			applyTabToolApprovalModeToController(ctrl, normalizeToolApprovalMode(req.ToolApprovalMode))
		}
		return a.ensureTabTopicIndexedForUserTurn(target.tab)
	}
	receipt, err := c.SubmitPreparedWithSetup(a.attachmentOperationContext(target), prepared, setup)
	if err != nil {
		return TurnStartView{}, err
	}
	admission.finish(ctrl)
	return TurnStartView{TurnID: receipt.TurnID, SubmissionID: submissionID, Status: event.TurnQueued, Disposition: control.SubmitTurnStarted}, nil
}

func (a *App) EnqueueForAttachmentTarget(token, submissionID, input, display string, invocations []control.InvocationRequest, attachments []control.SubmissionAttachment) (InboxReceiptView, error) {
	target, err := a.attachmentTargetToken(token)
	if err != nil {
		return InboxReceiptView{}, err
	}
	a.runtimeAdmissionMu.RLock()
	defer a.runtimeAdmissionMu.RUnlock()
	if !a.attachmentTargetCurrent(target) {
		return InboxReceiptView{}, fmt.Errorf("attachment target changed; please retry")
	}
	c, ok := target.ctrl.(*control.Controller)
	if !ok {
		return InboxReceiptView{}, fmt.Errorf("attachment target is not a session")
	}
	receipt, err := c.TryEnqueueFollowupContext(a.attachmentOperationContext(target), control.InboxRequest{Intent: sessioninbox.IntentFollowup, Display: display, Raw: input, Submit: input, Source: "desktop", Idempotency: submissionID, Invocations: invocations, Attachments: attachments})
	if err != nil {
		return InboxReceiptView{}, err
	}
	a.emitInboxChanged(target.tabID)
	return InboxReceiptView{ItemID: receipt.ItemID, Paused: receipt.Paused}, nil
}
