package main

import (
	"fmt"

	"reasonix/internal/control"
)

// A receipt belongs to a durable session, not the UI tab which originally
// submitted it. Rebinding is read-only; enqueue still uses the original fence.
func (a *App) remoteReceiptTarget(original InboxTargetView) (InboxTargetView, error) {
	if original.HostID == "" || original.Workspace == "" {
		return original, nil // old callers cannot safely rebind without identity
	}
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	var chosen *remoteTab
	for _, tab := range a.remoteTabs {
		if tab == nil || tab.ref.HostID != original.HostID || tab.ref.Workspace != original.Workspace ||
			tab.routing.currentPath != original.SessionPath || tab.state != "ready" ||
			tab.client == nil || tab.routing.rehydratingPath != "" {
			continue
		}
		if chosen == nil || tab.id < chosen.id || tab.id == original.TabID {
			chosen = tab
		}
		if tab.id == original.TabID {
			break
		}
	}
	if chosen == nil {
		return InboxTargetView{}, fmt.Errorf("inbox receipt session unavailable")
	}
	return InboxTargetView{TabID: chosen.id, SessionPath: original.SessionPath, Generation: chosen.gen,
		Selection: chosen.selectionRevision, Remote: true, HostID: chosen.ref.HostID, Workspace: chosen.ref.Workspace}, nil
}

type localReceiptOwner struct {
	tab        *WorkspaceTab
	ctrl       control.SessionAPI
	path       string
	generation uint64
}

func (a *App) localReceiptTarget(path string) (localReceiptOwner, error) {
	key := sessionRuntimeKey(path)
	if key == "" {
		return localReceiptOwner{}, fmt.Errorf("inbox receipt session unavailable")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	var owner localReceiptOwner
	for _, tab := range a.runtimeTabsLocked() {
		if tab.Ctrl == nil || tab.ReadOnly || sessionRuntimeKey(tab.SessionPath) != key {
			continue
		}
		if owner.ctrl != nil && owner.ctrl != tab.Ctrl {
			return localReceiptOwner{}, fmt.Errorf("inbox receipt owner is ambiguous")
		}
		owner = localReceiptOwner{tab: tab, ctrl: tab.Ctrl, path: tab.SessionPath, generation: tab.SessionGeneration}
	}
	if owner.ctrl == nil {
		return owner, fmt.Errorf("inbox receipt session unavailable")
	}
	return owner, nil
}

func (a *App) localReceiptOwnerCurrent(owner localReceiptOwner) bool {
	if sessionRuntimeKey(owner.ctrl.SessionPath()) != sessionRuntimeKey(owner.path) {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.runtimeTabsLocked() {
		if tab == owner.tab && tab.Ctrl == owner.ctrl && !tab.ReadOnly && tab.SessionGeneration == owner.generation &&
			sessionRuntimeKey(tab.SessionPath) == sessionRuntimeKey(owner.path) {
			return true
		}
	}
	return false
}
