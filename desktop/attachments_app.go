package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/attachment"
	"reasonix/internal/control"
)

type attachmentTarget struct {
	tab             *WorkspaceTab
	tabID           string
	draftID         string
	draftGeneration uint64
	root            string
	generation      uint64
	ctrl            control.SessionAPI
	ctx             context.Context
	cancel          context.CancelFunc
	ownerIdentity   string
	ownerBound      bool
	runtimeEpoch    string
}

func (a *App) attachmentTargetForComposerTarget(composer ComposerTarget) (attachmentTarget, error) {
	if composer.Kind != "draft" {
		return a.attachmentTargetForTab(composer.TabID)
	}
	record, err := a.draftStore().Get(a.bootContext(), strings.TrimSpace(composer.DraftID))
	if err != nil {
		return attachmentTarget{}, err
	}
	root := record.WorkspaceRoot
	if record.Scope != "project" {
		root = globalWorkspaceRoot()
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return attachmentTarget{}, err
	}
	return attachmentTarget{
		draftID: record.ID, draftGeneration: composer.Generation, root: absRoot,
		ownerIdentity: fmt.Sprintf("draft:%s:%d", record.ID, composer.Generation),
	}, nil
}

func attachmentWorkspaceRoot(tab *WorkspaceTab) (string, error) {
	root := strings.TrimSpace(tab.WorkspaceRoot)
	if root == "" {
		if tab.Scope == "project" {
			return "", fmt.Errorf("attachment workspace is not ready")
		}
		root = globalWorkspaceRoot()
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return absRoot, nil
}

func (a *App) attachmentTargetForTab(tabID string) (attachmentTarget, error) {
	tab, _ := a.tabAndCtrlByID(tabID)
	if tab == nil {
		return attachmentTarget{}, fmt.Errorf("attachment workspace is no longer available")
	}
	a.reconcileTabWithPinnedSessionMeta(tab)
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.tabs[tab.ID] != tab || tab.removed {
		return attachmentTarget{}, fmt.Errorf("attachment workspace is no longer available")
	}
	root, err := attachmentWorkspaceRoot(tab)
	if err != nil {
		return attachmentTarget{}, err
	}
	target := attachmentTarget{
		tab: tab, tabID: tab.ID, root: root, generation: tab.SessionGeneration, ctrl: tab.Ctrl,
		runtimeEpoch: a.runtimeEpochForTabLocked(tab),
	}
	if c, ok := tab.Ctrl.(*control.Controller); ok {
		target.ownerIdentity = c.AttachmentOwnerIdentity()
		target.ownerBound = strings.Trim(target.ownerIdentity, "\x00") != ""
	}
	if !target.ownerBound {
		target.ownerIdentity = fmt.Sprintf("tab:%s:%d:%s", tab.ID, tab.SessionGeneration, target.runtimeEpoch)
	}
	return target, nil
}

func (a *App) attachmentTargetCurrent(target attachmentTarget) bool {
	return a.attachmentTargetInvalidReason(target) == ""
}

func (a *App) attachmentTargetInvalidReason(target attachmentTarget) string {
	if target.tab == nil {
		if target.draftID == "" {
			return "missing owner"
		}
		record, err := a.draftStore().Get(a.bootContext(), target.draftID)
		if err != nil || record.Status != "active" {
			return "draft is no longer active"
		}
		root := record.WorkspaceRoot
		if record.Scope != "project" {
			root = globalWorkspaceRoot()
		}
		absRoot, err := filepath.Abs(root)
		if err != nil || !sameProjectRoot(absRoot, target.root) {
			return "workspace changed"
		}
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	tab := a.tabs[target.tabID]
	if tab != target.tab || tab.removed {
		return "tab closed"
	}
	if tab.SessionGeneration != target.generation {
		return "session generation changed"
	}
	if tab.Ctrl != target.ctrl {
		return "runtime changed"
	}
	if a.runtimeEpochForTabLocked(tab) != target.runtimeEpoch {
		return "runtime epoch changed"
	}
	if c, ok := tab.Ctrl.(*control.Controller); ok && target.ownerBound && c.AttachmentOwnerIdentity() != target.ownerIdentity {
		return "attachment owner changed"
	}
	root, err := attachmentWorkspaceRoot(tab)
	if err != nil || !sameProjectRoot(root, target.root) {
		return "workspace changed"
	}
	return ""
}

func (a *App) finishAttachmentWrite(target attachmentTarget, rel string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if a.attachmentIOHook != nil {
		a.attachmentIOHook()
	}
	if a.attachmentTargetCurrent(target) {
		return rel, nil
	}
	if isAttachmentRefPath(rel) {
		_ = os.Remove(filepath.Join(target.root, filepath.FromSlash(rel)))
	}
	return "", fmt.Errorf("attachment workspace changed while saving; please retry")
}

func isAttachmentRefPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return strings.HasPrefix(path, ".reasonix/attachments/")
}

// SavePastedImage stores a browser clipboard image data URL under the active
// tab's workspace .reasonix/attachments and returns the relative @-reference path.
func (a *App) SavePastedImage(dataURL string) (string, error) {
	target, err := a.attachmentTargetForTab("")
	if err != nil {
		return "", err
	}
	rel, saveErr := control.SaveImageDataURLInRoot(target.root, dataURL)
	return a.finishAttachmentWrite(target, rel, saveErr)
}

func (a *App) SavePastedImageForTab(tabID, dataURL string) (string, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return "", err
	}
	rel, saveErr := control.SaveImageDataURLInRoot(target.root, dataURL)
	return a.finishAttachmentWrite(target, rel, saveErr)
}

// SaveClipboardImage reads the native OS clipboard image under the active tab's
// workspace .reasonix/attachments and returns the relative @-reference path.
func (a *App) SaveClipboardImage() (string, error) {
	target, err := a.attachmentTargetForTab("")
	if err != nil {
		return "", err
	}
	rel, saveErr := control.SaveClipboardImageInRoot(target.root)
	return a.finishAttachmentWrite(target, rel, saveErr)
}

func (a *App) SaveClipboardImageForTab(tabID string) (string, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return "", err
	}
	rel, saveErr := control.SaveClipboardImageInRoot(target.root)
	return a.finishAttachmentWrite(target, rel, saveErr)
}

// SavePastedFile stores a dropped non-image file (the browser exposes its bytes
// as a data URL but not a real path) under the active tab's workspace
// .reasonix/attachments and returns the relative @-reference path.
func (a *App) SavePastedFile(name, dataURL string) (string, error) {
	target, err := a.attachmentTargetForTab("")
	if err != nil {
		return "", err
	}
	rel, saveErr := control.SaveAttachmentDataURLInRoot(target.root, name, dataURL)
	return a.finishAttachmentWrite(target, rel, saveErr)
}

func (a *App) SavePastedFileForTab(tabID, name, dataURL string) (string, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return "", err
	}
	rel, saveErr := control.SaveAttachmentDataURLInRoot(target.root, name, dataURL)
	return a.finishAttachmentWrite(target, rel, saveErr)
}

// AttachmentDataURL returns a safe data URL for a stored image attachment.
func (a *App) AttachmentDataURL(path string) (string, error) {
	target, err := a.attachmentTargetForTab("")
	if err != nil {
		return "", err
	}
	return a.attachmentDataURLForTarget(target, path)
}

func (a *App) AttachmentDataURLForTab(tabID, path string) (string, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return "", err
	}
	return a.attachmentDataURLForTarget(target, path)
}

func (a *App) attachmentDataURLForTarget(target attachmentTarget, path string) (string, error) {
	dataURL, readErr := control.ImageDataURLInRoot(target.root, path)
	if readErr != nil {
		return "", readErr
	}
	if a.attachmentIOHook != nil {
		a.attachmentIOHook()
	}
	if !a.attachmentTargetCurrent(target) {
		return "", fmt.Errorf("attachment workspace changed while reading; please retry")
	}
	return dataURL, nil
}

// DroppedItem is one OS-dropped file resolved into a composer context entry.
type DroppedItem struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	IsDir       bool   `json:"isDir,omitempty"`
	DisplayPath string `json:"displayPath,omitempty"`
	PreviewURL  string `json:"previewUrl,omitempty"`
}

const attachmentsCapabilityV1 = "attachments-v1"

type DraftImageView struct {
	DraftID     string `json:"draftId"`
	Path        string `json:"path,omitempty"`
	DisplayName string `json:"displayName"`
	MIME        string `json:"mime"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bytes       int64  `json:"bytes"`
}

func (a *App) StageImageForTab(tabID, operationID, displayName, mime, dataURL string) (DraftImageView, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return DraftImageView{}, err
	}
	return a.stageImageForTarget(target, displayName, mime, dataURL)
}

func (a *App) stageImageForTarget(target attachmentTarget, displayName, mime, dataURL string) (DraftImageView, error) {
	identified, ok := target.ctrl.(*control.Controller)
	if !ok {
		return DraftImageView{}, fmt.Errorf("unsupported: %s", attachmentsCapabilityV1)
	}
	draft, err := identified.StageImage(a.attachmentOperationContext(target), displayName, mime, dataURL)
	if a.attachmentIOHook != nil {
		a.attachmentIOHook()
	}
	if !a.attachmentTargetCurrent(target) {
		identified.ReleaseDraftImage(draft.ID)
		return DraftImageView{}, fmt.Errorf("attachment workspace changed while saving; please retry")
	}
	if err != nil {
		return DraftImageView{}, err
	}
	return draftImageView(draft), nil
}

func (a *App) ReadDraftImageForTab(tabID, draftID string) (string, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return "", err
	}
	return a.readDraftImageForTarget(target, draftID)
}

func (a *App) readDraftImageForTarget(target attachmentTarget, draftID string) (string, error) {
	identified, ok := target.ctrl.(*control.Controller)
	if !ok {
		return "", fmt.Errorf("unsupported: %s", attachmentsCapabilityV1)
	}
	draft, raw, err := identified.ReadDraftImage(a.attachmentOperationContext(target), draftID)
	if a.attachmentIOHook != nil {
		a.attachmentIOHook()
	}
	if !a.attachmentTargetCurrent(target) {
		return "", fmt.Errorf("attachment workspace changed while reading; please retry")
	}
	if err != nil {
		return "", err
	}
	return attachment.DataURL(draft.MIME, raw), nil
}

func (a *App) ReadSessionAttachmentForTab(tabID, digest string, offset int64) (SessionHistoryContentChunk, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return SessionHistoryContentChunk{}, err
	}
	ctx := a.attachmentContext()
	var cancel context.CancelFunc
	if controller, ok := target.ctrl.(*control.Controller); ok {
		ctx, cancel = controller.NewAttachmentOperationContext(ctx)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	target.ctx = ctx
	if err := ctx.Err(); err != nil {
		return SessionHistoryContentChunk{}, err
	}
	query, sessionRef, err := a.canonicalSessionQuery(tabID)
	if err != nil {
		return SessionHistoryContentChunk{}, err
	}
	if !a.attachmentTargetCurrent(target) {
		return SessionHistoryContentChunk{}, fmt.Errorf("attachment workspace changed while reading; please retry")
	}
	data, total, err := query.ReadSessionAttachment(ctx, sessionRef, digest, offset, sessionHistoryContentChunkBytes)
	if a.attachmentIOHook != nil {
		a.attachmentIOHook()
	}
	if !a.attachmentTargetCurrent(target) {
		return SessionHistoryContentChunk{}, fmt.Errorf("attachment workspace changed while reading; please retry")
	}
	if err != nil {
		return SessionHistoryContentChunk{}, err
	}
	next := offset + int64(len(data))
	return SessionHistoryContentChunk{
		Data:       base64.StdEncoding.EncodeToString(data),
		NextOffset: next,
		Done:       next >= total,
	}, nil
}

func (a *App) ReleaseDraftImageForTab(tabID, draftID string) error {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return err
	}
	identified, ok := target.ctrl.(*control.Controller)
	if !ok {
		return fmt.Errorf("unsupported: %s", attachmentsCapabilityV1)
	}
	identified.ReleaseDraftImage(draftID)
	return nil
}

func draftImageView(draft attachment.DraftCredential) DraftImageView {
	return DraftImageView{
		DraftID:     draft.ID,
		DisplayName: draft.DisplayName,
		MIME:        draft.MIME,
		Width:       draft.Width,
		Height:      draft.Height,
		Bytes:       draft.Bytes,
	}
}

func (a *App) AttachDropped(path string) (DroppedItem, error) {
	return a.attachDroppedForTab("", path)
}

func (a *App) AttachDroppedForTab(tabID, path string) (DroppedItem, error) {
	return a.attachDroppedForTab(tabID, path)
}

func (a *App) attachDroppedForTab(tabID, path string) (DroppedItem, error) {
	target, err := a.attachmentTargetForTab(tabID)
	if err != nil {
		return DroppedItem{}, err
	}
	return a.attachDroppedForTarget(target, path)
}

func (a *App) attachDroppedForTarget(target attachmentTarget, path string) (DroppedItem, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return DroppedItem{}, err
	}
	if isImageExt(path) {
		if rel, saveErr := control.SaveImageFileInRoot(target.root, path); saveErr == nil {
			rel, saveErr = a.finishAttachmentWrite(target, rel, nil)
			if saveErr != nil {
				return DroppedItem{}, saveErr
			}
			preview, previewErr := a.attachmentDataURLForTarget(target, rel)
			if previewErr != nil {
				_ = os.Remove(filepath.Join(target.root, filepath.FromSlash(rel)))
				return DroppedItem{}, previewErr
			}
			return DroppedItem{Kind: "attachment", Path: rel, PreviewURL: preview}, nil
		} else {
			return DroppedItem{}, saveErr
		}
	}
	if rel, ok := workspaceRelativeIn(path, target.root); ok {
		if !a.attachmentTargetCurrent(target) {
			return DroppedItem{}, fmt.Errorf("attachment workspace changed while reading; please retry")
		}
		return DroppedItem{Kind: "workspace", Path: rel, IsDir: info.IsDir()}, nil
	}
	if info.IsDir() {
		if target.tab == nil || target.ctrl == nil {
			return DroppedItem{}, fmt.Errorf("workspace is not ready")
		}
		if err := a.ensureTabControllerWorkspace(target.tab); err != nil {
			return DroppedItem{}, err
		}
		refreshed, err := a.attachmentTargetForTab(target.tabID)
		if err != nil || refreshed.tab != target.tab || refreshed.generation != target.generation ||
			!sameProjectRoot(refreshed.root, target.root) {
			return DroppedItem{}, fmt.Errorf("attachment workspace is no longer available")
		}
		target = refreshed
		ctrl := target.ctrl
		if ctrl == nil || !a.attachmentTargetCurrent(target) {
			return DroppedItem{}, fmt.Errorf("attachment workspace is no longer available")
		}
		token, displayPath, err := ctrl.RegisterExternalFolderRef(path)
		if err != nil {
			return DroppedItem{}, err
		}
		return DroppedItem{Kind: "workspace", Path: token, IsDir: true, DisplayPath: displayPath}, nil
	}
	rel, err := control.SaveAttachmentFileInRoot(target.root, path)
	if err != nil {
		return DroppedItem{}, err
	}
	rel, err = a.finishAttachmentWrite(target, rel, nil)
	if err != nil {
		return DroppedItem{}, err
	}
	return DroppedItem{Kind: "attachment", Path: rel}, nil
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

func workspaceRelativeIn(path, workspaceRoot string) (string, bool) {
	root := workspaceRoot
	if !filepath.IsAbs(root) {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", false
		}
		root = abs
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
