package main

import (
	"fmt"
	"net/http"
)

// A rejection is one publication transaction: observers of its error must
// already see the restored identity. The route fence also prevents a newer
// selection or authoritative frame from being overwritten by the old failure.
func (a *App) completeRemoteTabResumeFailure(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume, message string) bool {
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen || current.state != "ready" ||
		current.selectionRevision != route.selectionRevision || current.routing.currentPath != route.targetPath ||
		route.active && (current.routing.rehydratingPath != route.targetPath || current.routing.pathRevision != route.pathRevision+1) ||
		!route.active && current.routing.pathRevision != route.pathRevision ||
		route.previousSelection != nil && current.selectionRevision != route.previousSelection.revision {
		a.remoteTabMu.Unlock()
		return true
	}
	if route.previousSelection != nil {
		restoreRemoteTabOpenSelectionLocked(current, route.previousSelection)
	} else if route.active {
		restoreRemoteTabProvisionalRouteLocked(current, route)
	}
	current.err = message
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	if route.previousSelection != nil {
		a.emitRemoteEvent("remote-tab:updated", meta)
		a.saveTabsFromRemote()
	}
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: "ready", Error: message})
	return false
}
