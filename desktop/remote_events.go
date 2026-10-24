package main

import (
	"fmt"
	"strings"
)

// RemoteTabHydrationState is a local publication snapshot, with no Serve I/O.
// Taking the route publication lock joins mount-time hydration to a ready
// barrier even when its frontend event is still queued for delivery.
func (a *App) RemoteTabHydrationState(tabID string) (RemoteTabStateView, error) {
	tab := a.lockRemoteTabPublication(tabID)
	if tab == nil {
		return RemoteTabStateView{}, fmt.Errorf("remote tab %q not found", tabID)
	}
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if a.remoteTabs[tabID] != tab {
		return RemoteTabStateView{}, fmt.Errorf("remote tab %q was replaced", tabID)
	}
	return RemoteTabStateView{State: tab.state, Error: tab.err, HydrationRevision: tab.publication.hydrationRevision}, nil
}

// emitRemoteEvent bridges a kernel callback to the frontend through the async
// emitter so a slow webview never blocks the kernel.
func (a *App) emitRemoteEvent(name string, payload any) {
	if view, ok := payload.(RemoteTabStateView); ok && strings.HasPrefix(name, "remote-tab:") && strings.HasSuffix(name, ":state") {
		tabID := strings.TrimSuffix(strings.TrimPrefix(name, "remote-tab:"), ":state")
		a.remoteTabMu.Lock()
		if tab := a.remoteTabs[tabID]; tab != nil {
			tab.publication.hydrationRevision++
			view.HydrationRevision = tab.publication.hydrationRevision
		}
		a.remoteTabMu.Unlock()
		payload = view
	}
	if a.remoteEventHook != nil {
		a.remoteEventHook(name, payload)
	}
	if strings.HasPrefix(name, "remote-tab:") && strings.HasSuffix(name, ":state") {
		a.emitRuntimeStateChanged()
	}
	ctx := a.bootContext()
	if ctx == nil {
		return
	}
	a.runtimeEvents.Emit(ctx, name, payload)
}
