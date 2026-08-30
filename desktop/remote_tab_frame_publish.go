package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (a *App) publishRemoteTabFrame(tabID string, gen uint64, sessionPath, kind string, frame json.RawMessage) bool {
	return a.publishRemoteTabFrameForRoute(tabID, nil, nil, gen, sessionPath, false, kind, frame)
}

// publishRemoteTabFrameForRoute holds the tab's route/event boundary from the
// final route validation through frontend publication. A concurrent adoption
// therefore publishes its barrier wholly before or after the validated frame.
func (a *App) publishRemoteTabFrameForRoute(tabID string, expectedTab *remoteTab, expectedClient *http.Client, gen uint64, sessionPath string, requireRehydrating bool, kind string, frame json.RawMessage) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return false
	}
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	return a.publishRemoteTabFrameForRouteLocked(tabID, tab, expectedTab, expectedClient, gen, sessionPath, requireRehydrating, kind, frame)
}

// publishRemoteTabFrameForRouteLocked publishes while tab.routeEventMu is held.
func (a *App) publishRemoteTabFrameForRouteLocked(tabID string, tab, expectedTab *remoteTab, expectedClient *http.Client, gen uint64, sessionPath string, requireRehydrating bool, kind string, frame json.RawMessage) bool {
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	valid := current == tab && current.gen == gen &&
		(expectedTab == nil || current == expectedTab) &&
		(expectedClient == nil || current.client == expectedClient) &&
		(sessionPath == "" || current.routing.currentPath == sessionPath) &&
		(!requireRehydrating || current.routing.rehydratingPath == sessionPath)
	a.remoteTabMu.Unlock()
	if !valid {
		return false
	}
	refreshRuntime := false
	switch kind {
	case "turn_started":
		a.recordRemoteTabTurnStarted(tabID, gen, frame)
		refreshRuntime = true
	case "approval_request", "ask_request":
		a.cacheRemotePendingEvent(tabID, gen, kind, frame)
		refreshRuntime = true
	case "extension_surface":
		refreshRuntime = a.cacheRemotePendingExtensionForm(tabID, gen, frame)
	case "turn_done":
		a.completeRemoteTabTurn(tabID, gen)
	}
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:event", tabID), frame)
	if refreshRuntime {
		a.goRemoteTabSafe("remoteTabRuntimeStatus", func() { _, _ = a.RemoteTabStatus(tabID) })
	}
	if kind == "turn_done" {
		// Capture the durable session name immediately, closing the window
		// where a replacement Serve could otherwise lose a just-finished
		// conversation before the slower generated-title refresh runs.
		a.goRemoteTabSafe("remoteTabTitle", func() {
			_, _ = a.RemoteTabStatus(tabID)
			// The serve generates the session title from the finished
			// conversation; pick it up shortly after the turn settles.
			time.Sleep(1500 * time.Millisecond)
			a.refreshRemoteTabTitle(tabID)
		})
	}
	return true
}
