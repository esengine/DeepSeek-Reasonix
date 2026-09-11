package main

import (
	"context"
	"fmt"
	"net/http"
)

// Route events, terminal state, explicit close and generation replacement
// share one publication order. Never wait for this fence while holding
// remoteTabMu; callers recheck the captured tab after acquiring both locks.
func (a *App) lockRemoteTabPublication(tabID string) *remoteTab {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab != nil {
		tab.routeEventMu.Lock()
	}
	return tab
}

func (a *App) retireRemoteTabGeneration(tabID string, gen uint64) {
	tab := a.lockRemoteTabPublication(tabID)
	if tab == nil {
		return
	}
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return
	}
	cancel := tab.cancel
	tab.gen++
	tab.attachedGen = 0
	tab.cancel = nil
	tab.client = nil
	tab.base = ""
	tab.token = ""
	a.remoteTabMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// reconnectRemoteTabGeneration retires a dead pump and atomically parks its
// tab in reconnecting. The bool reports whether this pump should start the
// retry loop; a pump opened by an existing retry loop leaves retries to its
// caller so two loops cannot race each other.
func (a *App) reconnectRemoteTabGeneration(tabID string, gen uint64) bool {
	tab := a.lockRemoteTabPublication(tabID)
	if tab == nil {
		return false
	}
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	startRetry := tab.state != "reconnecting"
	cancel := tab.cancel
	tab.gen++
	tab.attachedGen = 0
	tab.cancel = nil
	tab.client = nil
	tab.base = ""
	tab.token = ""
	tab.state = "reconnecting"
	tab.err = ""
	a.remoteTabMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: "reconnecting"})
	return startRetry
}

func (a *App) emitRemoteTabStateForGeneration(tabID string, gen uint64, state, errMsg string) bool {
	tab := a.lockRemoteTabPublication(tabID)
	if tab == nil {
		return false
	}
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	tab.state = state
	tab.err = errMsg
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errMsg})
	return true
}

func (a *App) transitionRemoteTabState(tabID string, gen uint64, from, state, errMsg string) bool {
	tab := a.lockRemoteTabPublication(tabID)
	if tab == nil {
		return false
	}
	defer tab.routeEventMu.Unlock()
	return a.transitionRemoteTabStateLocked(tab, gen, from, state, errMsg)
}

func (a *App) transitionRemoteTabStateLocked(tab *remoteTab, gen uint64, from, state, errMsg string) bool {
	tabID := tab.id
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.gen != gen || tab.state != from {
		a.remoteTabMu.Unlock()
		return false
	}
	tab.state = state
	tab.err = errMsg
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errMsg})
	return true
}

func (a *App) emitRemoteTabState(tabID, state, errMsg string) {
	tab := a.lockRemoteTabPublication(tabID)
	if tab == nil {
		return
	}
	defer tab.routeEventMu.Unlock()
	a.emitRemoteTabStateLocked(tab, state, errMsg)
}

func (a *App) emitRemoteTabStateLocked(tab *remoteTab, state, errMsg string) {
	tabID := tab.id
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab {
		a.remoteTabMu.Unlock()
		return
	}
	tab.state = state
	tab.err = errMsg
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errMsg})
}

func (a *App) installRemoteTabAttachPump(ctx context.Context, tabID string, tab *remoteTab, client *http.Client, base, token, targetPath string, installRoute bool) (context.Context, uint64, uint64, error) {
	tab.routeEventMu.Lock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab {
		a.remoteTabMu.Unlock()
		tab.routeEventMu.Unlock()
		return nil, 0, 0, fmt.Errorf("remote tab %q closed during bootstrap", tabID)
	}
	// Retire any pump installed by a concurrent reconnect so exactly one
	// generation owns the event stream.
	tab.gen++
	if tab.cancel != nil {
		tab.cancel()
	}
	tab.client = client
	tab.base = base
	tab.token = token
	if installRoute {
		commitRemoteTabAttachRoute(tab, targetPath, false)
	}
	attachPathRevision := tab.routing.pathRevision
	gen := tab.gen
	pumpCtx, cancelPump := context.WithCancel(ctx)
	tab.cancel = cancelPump
	a.remoteTabMu.Unlock()
	tab.routeEventMu.Unlock()

	return pumpCtx, gen, attachPathRevision, nil
}
