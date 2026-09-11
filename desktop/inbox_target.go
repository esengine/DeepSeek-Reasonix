package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"reasonix/internal/sessioninbox"
)

// InboxTargetView fences follow-ups across both local replacement and remote
// selection changes. It is process-local and never written to the inbox ledger.
type InboxTargetView struct {
	TabID       string `json:"tabId"`
	SessionPath string `json:"sessionPath"`
	Generation  uint64 `json:"generation"`
	Selection   uint64 `json:"selection"`
	Remote      bool   `json:"remote"`
	HostID      string `json:"hostId,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
}

func (a *App) CaptureInboxTarget(tabID, expectedPath string) (InboxTargetView, error) {
	a.runtimeAdmissionMu.RLock()
	defer a.runtimeAdmissionMu.RUnlock()
	return a.captureInboxTarget(tabID, expectedPath)
}

func (a *App) captureInboxTarget(tabID, expectedPath string) (InboxTargetView, error) {
	a.remoteTabMu.Lock()
	remote := a.remoteTabs[tabID]
	if remote != nil {
		defer a.remoteTabMu.Unlock()
		if remote.state != "ready" || remote.client == nil || remote.routing.rehydratingPath != "" || remote.routing.currentPath == "" || remote.routing.currentPath != expectedPath {
			return InboxTargetView{}, fmt.Errorf("inbox target changed")
		}
		return InboxTargetView{TabID: tabID, SessionPath: expectedPath, Generation: remote.gen, Selection: remote.selectionRevision,
			Remote: true, HostID: remote.ref.HostID, Workspace: remote.ref.Workspace}, nil
	}
	a.remoteTabMu.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab.ID == tabID && tab.Ctrl != nil && tab.SessionPath != "" && tab.SessionPath == expectedPath {
			return InboxTargetView{TabID: tabID, SessionPath: expectedPath, Generation: tab.SessionGeneration}, nil
		}
	}
	return InboxTargetView{}, fmt.Errorf("inbox target changed")
}

func (a *App) remoteInboxTarget(target InboxTargetView) (*http.Client, string, error) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[target.TabID]
	if tab == nil || tab.client == nil || tab.state != "ready" || tab.gen != target.Generation || tab.selectionRevision != target.Selection || tab.routing.currentPath != target.SessionPath || tab.routing.rehydratingPath != "" || (target.HostID != "" && (tab.ref.HostID != target.HostID || tab.ref.Workspace != target.Workspace)) {
		return nil, "", fmt.Errorf("inbox target changed")
	}
	return tab.client, tab.base, nil
}

// EnqueueInboxFollowupForTarget is additive; older bindings retain their APIs.
func (a *App) EnqueueInboxFollowupForTarget(target InboxTargetView, display, submit string, invocations []InvocationRequest, key string) (InboxReceiptView, error) {
	if strings.TrimSpace(key) == "" {
		return InboxReceiptView{}, inboxNotSubmitted(fmt.Errorf("idempotency key required"))
	}
	if target.Remote {
		client, base, err := a.remoteInboxTarget(target)
		if err != nil {
			return InboxReceiptView{}, inboxNotSubmitted(err)
		}
		return a.enqueueRemoteFollowupAt(client, base, target.SessionPath, display, submit, invocations, key)
	}
	a.runtimeAdmissionMu.RLock()
	defer a.runtimeAdmissionMu.RUnlock()
	current, err := a.captureInboxTarget(target.TabID, target.SessionPath)
	if err != nil || current != target {
		return InboxReceiptView{}, inboxNotSubmitted(fmt.Errorf("inbox target changed"))
	}
	ctrl, err := a.inboxCtrl(target.TabID)
	if err != nil {
		return InboxReceiptView{}, inboxNotSubmitted(err)
	}
	return a.enqueueInboxWithController(target.TabID, ctrl, sessioninbox.IntentFollowup, display, submit, invocations, key, false, "", target.SessionPath)
}

func inboxNotSubmitted(err error) error {
	return &inboxCodedError{code: "inbox_not_submitted", cause: err}
}

// LookupInboxFollowupForTarget never creates an item, including on a missing
// or expired receipt. Position zero identifies an already removed item.
func (a *App) LookupInboxFollowupForTarget(target InboxTargetView, key string) (InboxReceiptView, error) {
	if target.Remote {
		current, err := a.remoteReceiptTarget(target)
		if err != nil {
			return InboxReceiptView{}, err
		}
		target = current
		client, base, err := a.remoteInboxTarget(target)
		if err != nil {
			return InboxReceiptView{}, err
		}
		ctx, cancel := commandContext(a)
		defer cancel()
		data, err := serveGet(ctx, client, serveURL(base, "/inbox/receipt?key="+url.QueryEscape(key)+"&session="+url.QueryEscape(target.SessionPath)))
		if err != nil {
			return InboxReceiptView{}, err
		}
		var receipt InboxReceiptView
		if err := json.Unmarshal(data, &receipt); err != nil {
			return receipt, err
		}
		currentClient, currentBase, err := a.remoteInboxTarget(target)
		if err != nil || currentClient != client || currentBase != base {
			return InboxReceiptView{}, fmt.Errorf("inbox receipt route changed during read")
		}
		return receipt, nil
	}
	a.runtimeAdmissionMu.RLock()
	defer a.runtimeAdmissionMu.RUnlock()
	owner, err := a.localReceiptTarget(target.SessionPath)
	if err != nil {
		return InboxReceiptView{}, err
	}
	reader, ok := owner.ctrl.(interface {
		LookupInboxReceiptForSession(string, string) (sessioninbox.InboxReceipt, bool, error)
	})
	if !ok {
		return InboxReceiptView{}, fmt.Errorf("inbox receipt lookup unavailable")
	}
	receipt, found, err := reader.LookupInboxReceiptForSession(owner.path, key)
	if err != nil {
		return InboxReceiptView{}, err
	}
	if !found {
		return InboxReceiptView{}, fmt.Errorf("inbox receipt unconfirmed")
	}
	if !a.localReceiptOwnerCurrent(owner) {
		return InboxReceiptView{}, fmt.Errorf("inbox receipt owner changed during read")
	}
	return InboxReceiptView{ItemID: receipt.ItemID, Disposition: string(receipt.Disposition), Position: receipt.Position, Paused: receipt.Paused, Idempotent: receipt.Idempotent}, nil
}
