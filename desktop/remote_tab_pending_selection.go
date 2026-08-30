package main

import (
	"strings"
)

// remoteTabPendingOpenSelection retains the newest session-selection intent
// while an existing shell is still attaching. The local identity must not
// advance until Serve can accept the matching /resume or /new request.
type remoteTabPendingOpenSelection struct {
	name              string
	path              string
	title             string
	newSession        bool
	reuseBlank        bool
	revision          uint64
	deferred          bool
	identityCommitted bool
	previous          *remoteTabOpenSelection
}

func newRemoteTabPendingOpenSelection(opts RemoteTabOpenOptions) *remoteTabPendingOpenSelection {
	return &remoteTabPendingOpenSelection{
		name: strings.TrimSpace(opts.SessionName), path: strings.TrimSpace(opts.SessionPath),
		title: strings.TrimSpace(opts.SessionTitle), newSession: opts.NewSession,
	}
}

// consumeQueuedRemoteTabOpenSelectionLocked retires the ready-tab handoff once
// its resume goroutine passes the revision fence. selectionMu keeps newer
// registrations out until this request and any rollback finish. Caller holds
// remoteTabMu.
func consumeQueuedRemoteTabOpenSelectionLocked(tab *remoteTab, revision uint64) {
	if revision == 0 || tab.pendingSelection == nil || tab.pendingSelection.deferred || tab.pendingSelection.revision != revision {
		return
	}
	tab.pendingSelection = nil
}

func requeueRemoteTabOpenSelectionLocked(tab *remoteTab, selection *remoteTabPendingOpenSelection) {
	pending := tab.pendingSelection
	if pending != nil && (pending.revision == 0 || pending.revision > selection.revision) {
		return
	}
	tab.pendingSelection = selection
}

// applyPendingRemoteTabOpenSelection commits only the newest deferred intent
// once the shell reaches ready, then uses the normal transition path.
func (a *App) applyPendingRemoteTabOpenSelection(tabID string) {
	a.tabSelectionMu.Lock()
	defer a.tabSelectionMu.Unlock()
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return
	}
	tab.routeEventMu.Lock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.state != "ready" || current.pendingSelection == nil || !current.pendingSelection.deferred {
		a.remoteTabMu.Unlock()
		tab.routeEventMu.Unlock()
		return
	}
	selection := current.pendingSelection
	current.pendingSelection = nil
	if selection.revision == 0 {
		current.selectionRevision++
		selection.revision = current.selectionRevision
	} else if current.selectionRevision != selection.revision {
		a.remoteTabMu.Unlock()
		tab.routeEventMu.Unlock()
		return
	}
	if selection.newSession {
		// Recheck the authoritative state at application time. The tab may have
		// earned content while the shell was reconnecting, so reuseBlank from
		// registration is only a snapshot and cannot discard this newer intent.
		reuseCurrentBlank := current.session.reset
		a.remoteTabMu.Unlock()
		tab.routeEventMu.Unlock()
		if reuseCurrentBlank {
			return
		}
		if err := a.resetRemoteTabSession(tabID); err != nil {
			a.emitRemoteTabState(tabID, "ready", err.Error())
		}
		return
	}
	previous := selection.previous
	identityChanged := !selection.identityCommitted
	if previous == nil {
		previous = &remoteTabOpenSelection{
			session: current.session, topicTitle: current.topicTitle,
			currentPath: current.routing.currentPath,
			pending:     cloneRemotePendingEvents(current.pendingEvents), runtime: current.runtime,
			revision: selection.revision,
		}
	}
	if identityChanged {
		current.session.newSession = false
		current.session.name = selection.name
		current.session.path = selection.path
		if selection.title != "" {
			current.topicTitle = selection.title
		}
		if selection.path != "" {
			commitRemoteTabAttachRoute(current, selection.path, false)
		}
	}
	selection.deferred = false
	selection.identityCommitted = true
	selection.previous = previous
	current.pendingSelection = selection
	current.err = ""
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	tab.routeEventMu.Unlock()

	if identityChanged {
		a.emitRemoteEvent("remote-tab:updated", meta)
		a.saveTabsFromRemote()
	}
	a.resumeRemoteTabOpenAsync(tabID, selection.name, selection.path, selection.title, previous)
}

func (a *App) resumeRemoteTabOpenAsync(tabID, name, sessionPath, sessionTitle string, selection *remoteTabOpenSelection) {
	revision := uint64(0)
	if selection != nil {
		if selection.revision == 0 {
			a.remoteTabMu.Lock()
			if current := a.remoteTabs[tabID]; current != nil {
				selection.revision = current.selectionRevision
			}
			a.remoteTabMu.Unlock()
		}
		revision = selection.revision
	}
	a.goRemoteTabSafe("remoteTabResume", func() {
		a.remoteTabMu.Lock()
		tab := a.remoteTabs[tabID]
		a.remoteTabMu.Unlock()
		if tab == nil {
			return
		}
		func() {
			tab.selectionMu.Lock()
			defer tab.selectionMu.Unlock()
			handled := a.resumeRemoteTabSessionPathForOpenSelection(tabID, name, sessionPath, sessionTitle, revision, selection)
			if !handled {
				a.restoreRejectedRemoteTabOpenSelection(tabID, selection)
			}
		}()
		a.applyPendingRemoteTabOpenSelection(tabID)
	})
}

func (a *App) restoreRejectedRemoteTabOpenSelection(tabID string, previous *remoteTabOpenSelection) {
	if previous == nil {
		return
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return
	}
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.selectionRevision != previous.revision || current.state != "ready" || strings.TrimSpace(current.err) == "" {
		a.remoteTabMu.Unlock()
		return
	}
	restoreRemoteTabOpenSelectionLocked(current, previous)
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	a.saveTabsFromRemote()
}

func restoreRemoteTabOpenSelectionLocked(current *remoteTab, previous *remoteTabOpenSelection) {
	current.session = previous.session
	current.topicTitle = previous.topicTitle
	current.routing.currentPath = previous.currentPath
	current.routing.pathRevision++
	current.routing.revision++
	current.routing.rehydratingPath = ""
	current.routing.rehydratingFrames = nil
	current.pendingEvents = cloneRemotePendingEvents(previous.pending)
	restoredRuntime := previous.runtime
	restoredRuntime.revision = max(current.runtime.revision, previous.runtime.revision) + 1
	current.runtime = restoredRuntime
}
