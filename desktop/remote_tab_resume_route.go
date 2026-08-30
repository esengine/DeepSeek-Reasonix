package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

type remoteTabProvisionalResume struct {
	targetPath      string
	previousPath    string
	pathRevision    uint64
	previousPending map[string]json.RawMessage
	previousRuntime remoteTabRuntimeState
	active          bool
}

func probeRemoteTabFrame(frame string) (kind, path string, current, reset bool) {
	var probe struct {
		Kind           string `json:"kind"`
		SessionPath    string `json:"sessionPath"`
		SessionCurrent bool   `json:"sessionCurrent"`
		SessionReset   bool   `json:"sessionReset"`
	}
	kind = "?"
	if json.Unmarshal([]byte(frame), &probe) == nil && probe.Kind != "" {
		kind = probe.Kind
	}
	return kind, strings.TrimSpace(probe.SessionPath), probe.SessionCurrent, probe.SessionReset
}

func (a *App) beginRemoteTabProvisionalResume(tabID string, tab *remoteTab, client *http.Client, gen uint64, targetPath string) remoteTabProvisionalResume {
	route := remoteTabProvisionalResume{targetPath: strings.TrimSpace(targetPath)}
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen || current.state != "ready" {
		return route
	}
	route.previousPath = current.routing.currentPath
	route.pathRevision = current.routing.pathRevision
	if route.targetPath == route.previousPath {
		return route
	}
	route.previousPending = cloneRemotePendingEvents(current.pendingEvents)
	route.previousRuntime = current.runtime
	route.active = true
	current.routing.currentPath = route.targetPath
	current.routing.pathRevision++
	current.routing.rehydratingPath = route.targetPath
	current.routing.rehydratingFrames = nil
	current.routing.revision++
	resetRemoteTabForegroundRuntimeLocked(current)
	current.runtime.running = current.routing.running[route.targetPath]
	current.runtime.cancellable = current.runtime.running
	return route
}

func (a *App) rollbackRemoteTabProvisionalResume(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume) bool {
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen || current.state != "ready" ||
		current.routing.currentPath != route.targetPath {
		return false
	}
	if !route.active {
		// Re-selecting the already current session creates no rehydration epoch.
		// The path revision still proves whether this failed request owns the
		// visible route or a newer adoption has already superseded it.
		return current.routing.pathRevision == route.pathRevision
	}
	if current.routing.rehydratingPath != route.targetPath {
		return false
	}
	current.routing.currentPath = route.previousPath
	current.routing.pathRevision++
	current.routing.rehydratingPath = ""
	current.routing.rehydratingFrames = nil
	current.routing.revision++
	current.pendingEvents = route.previousPending
	restoredRuntime := route.previousRuntime
	restoredRuntime.revision = max(current.runtime.revision, route.previousRuntime.revision) + 1
	current.runtime = restoredRuntime
	return true
}

// reconcileRemoteTabRejectedResume installs the route Serve reports after an
// ambiguous transport failure. The common unchanged case restores the exact
// preflight snapshot; an externally changed route drops controller-local state
// and publishes the authoritative identity behind a new ready barrier. It
// returns false only when the caller must also restore the pre-open selection.
func (a *App) reconcileRemoteTabRejectedResume(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume, authoritative serveSessionEntry, resumeErr error) bool {
	authoritative.Path = strings.TrimSpace(authoritative.Path)
	if authoritative.Path == route.previousPath {
		if a.rollbackRemoteTabProvisionalResume(tabID, tab, client, gen, route) {
			a.transitionRemoteTabState(tabID, gen, "ready", "ready", resumeErr.Error())
			// Serve stayed on the previous route, so the caller should restore
			// the rest of the pre-open selection snapshot too.
			return false
		}
		// A newer route superseded the failed request while it was being
		// reconciled. Preserve that newer authority.
		return true
	}
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen || current.state != "ready" ||
		current.routing.currentPath != route.targetPath ||
		route.active && current.routing.rehydratingPath != route.targetPath ||
		!route.active && current.routing.pathRevision != route.pathRevision {
		a.remoteTabMu.Unlock()
		return true
	}
	if !adoptRemoteTabSessionPathLocked(current, authoritative.Path) {
		current.routing.rehydratingPath = ""
		current.routing.rehydratingFrames = nil
	}
	current.session.name = strings.TrimSpace(authoritative.Name)
	current.session.path = authoritative.Path
	current.session.newSession = false
	current.session.reset = false
	current.runtime.running = authoritative.Running || current.routing.running[authoritative.Path]
	current.runtime.cancellable = current.runtime.running
	title := strings.TrimSpace(authoritative.Title)
	if title == "" {
		title = strings.TrimSpace(authoritative.Name)
	}
	if title == "" {
		title = remoteWorkspaceName(current.ref.Workspace)
	}
	current.topicTitle = title
	meta := remoteTabMetaLocked(current)
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	a.saveTabsFromRemote()
	a.transitionRemoteTabState(tabID, gen, "ready", "ready", resumeErr.Error())
	// The probed third path is Serve-authoritative. The generic open-selection
	// rollback must not replace it with the preflight route.
	return true
}

func (a *App) commitRemoteTabResume(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume, target serveSessionEntry, title string) (TabMeta, bool) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	current := a.remoteTabs[tabID]
	if current != tab || current.client != client || current.gen != gen || current.state != "ready" ||
		current.routing.currentPath != route.targetPath ||
		route.active && current.routing.rehydratingPath != route.targetPath ||
		!route.active && current.routing.pathRevision != route.pathRevision {
		return TabMeta{}, false
	}
	current.topicTitle = title
	current.session.reset = false
	current.session.newSession = false
	current.session.name = strings.TrimSpace(target.Name)
	current.session.path = target.Path
	current.routing.currentPath = target.Path
	// Close the provisional routing epoch so a listing that began while
	// /resume was in flight cannot publish its pre-switch snapshot afterward.
	current.routing.revision++
	current.runtime.revision++
	current.runtime.running = current.runtime.running || target.Running || current.routing.running[target.Path]
	current.runtime.cancellable = current.runtime.cancellable || current.runtime.running
	return remoteTabMetaLocked(current), true
}

// commitAndPublishRemoteTabResume keeps the successful HTTP commit, metadata
// publication, and ready/replay handoff in the same route epoch. Without this
// fence a newer session_changed adoption could publish between those steps and
// then be overwritten by the older resume metadata.
func (a *App) commitAndPublishRemoteTabResume(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume, target serveSessionEntry, title string) bool {
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	meta, committed := a.commitRemoteTabResume(tabID, tab, client, gen, route, target, title)
	if !committed {
		return false
	}
	a.emitRemoteEvent("remote-tab:updated", meta)
	a.saveTabsFromRemote()
	// Frames received while /resume was in flight were held behind the
	// provisional route. Rehydrate the committed session before replaying its
	// retained prompts or later live output.
	a.publishRemoteTabResumeReadyLocked(tabID, tab, client, gen, route)
	return true
}

func (a *App) publishRemoteTabResumeReady(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume) {
	// Keep the ready barrier and the complete buffered drain in one ordered
	// route-publication epoch. A later session adoption waits until every frame
	// from this snapshot is visible, then publishes its own ready barrier.
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.publishRemoteTabResumeReadyLocked(tabID, tab, client, gen, route)
}

// publishRemoteTabResumeReadyLocked publishes while tab.routeEventMu is held.
func (a *App) publishRemoteTabResumeReadyLocked(tabID string, tab *remoteTab, client *http.Client, gen uint64, route remoteTabProvisionalResume) {
	if !a.transitionRemoteTabState(tabID, gen, "ready", "ready", "") {
		return
	}
	for {
		a.remoteTabMu.Lock()
		current := a.remoteTabs[tabID]
		if current != tab || current.client != client || current.gen != gen || current.routing.rehydratingPath != route.targetPath {
			a.remoteTabMu.Unlock()
			return
		}
		frames := current.routing.rehydratingFrames
		current.routing.rehydratingFrames = nil
		if len(frames) == 0 {
			// Clearing the path under the same lock that producers use closes the
			// replay/live race: a later frame either joined this drain or observes
			// the committed live route after every older frame was published.
			current.routing.rehydratingPath = ""
			a.remoteTabMu.Unlock()
			return
		}
		a.remoteTabMu.Unlock()
		for _, frame := range frames {
			kind, path, _, _ := probeRemoteTabFrame(string(frame))
			if path != "" && path != route.targetPath {
				return
			}
			if !a.publishRemoteTabFrameForRouteLocked(tabID, tab, tab, client, gen, route.targetPath, true, kind, frame) {
				return
			}
		}
	}
}

func cloneRemotePendingEvents(src map[string]json.RawMessage) map[string]json.RawMessage {
	if src == nil {
		return nil
	}
	dst := make(map[string]json.RawMessage, len(src))
	for key, frame := range src {
		dst[key] = append(json.RawMessage(nil), frame...)
	}
	return dst
}

func remotePendingEventKey(kind string, frame json.RawMessage) string {
	var probe struct {
		Approval *struct {
			ID string `json:"id"`
		} `json:"approval"`
		Ask *struct {
			ID string `json:"id"`
		} `json:"ask"`
	}
	_ = json.Unmarshal(frame, &probe)
	id := ""
	if probe.Approval != nil {
		id = probe.Approval.ID
	} else if probe.Ask != nil {
		id = probe.Ask.ID
	}
	return kind + ":" + strings.TrimSpace(id)
}

func (a *App) bufferRemoteTabResumeFrame(tabID string, gen uint64, sessionPath, kind string, frame json.RawMessage) bool {
	sessionPath = strings.TrimSpace(sessionPath)
	if sessionPath == "" {
		return false
	}
	key := ""
	switch kind {
	case "approval_request", "ask_request":
		key = remotePendingEventKey(kind, frame)
	case "extension_surface":
		if remotePendingExtensionForm(frame) {
			key = remotePendingExtensionFormKey
		}
	}
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen || tab.routing.rehydratingPath != sessionPath {
		return false
	}
	if key != "" {
		tab.runtime.revision++
		if tab.pendingEvents == nil {
			tab.pendingEvents = make(map[string]json.RawMessage)
		}
		tab.pendingEvents[key] = append(json.RawMessage(nil), frame...)
		tab.runtime.pendingPrompt = true
		tab.runtime.cancellable = true
	}
	// Actionable frames must also cross the same fenced handoff as ordinary
	// output. Snapshot hydration deduplicates prompt IDs against live-buffered
	// events, while this replay closes the window after snapshot capture.
	tab.routing.rehydratingFrames = append(tab.routing.rehydratingFrames, append(json.RawMessage(nil), frame...))
	return true
}
