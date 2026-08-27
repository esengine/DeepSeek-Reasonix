package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type remoteTabSessionRouting struct {
	currentPath       string
	rehydratingPath   string
	rehydratingFrames []json.RawMessage
	running           map[string]bool
	revision          uint64
	// pathRevision changes only when foreground identity changes. Unlike
	// revision, background running/listing refreshes do not advance it.
	pathRevision uint64
}

func lockRemoteTabRoute(tab *remoteTab) func() {
	if tab == nil {
		return func() {}
	}
	tab.routeEventMu.Lock()
	return tab.routeEventMu.Unlock
}

// enterRemoteSession is the compatibility wrapper used by bridge tests.
func enterRemoteSession(ctx context.Context, client *http.Client, base string, opts RemoteTabOpenOptions) error {
	_, err := enterRemoteSessionTarget(ctx, client, base, opts)
	return err
}

// preflightRemoteSessionTarget resolves the foreground identity without
// mutating Serve. Attach uses it to publish routing before the event pump can
// observe an immediate replay from a detached controller.
func preflightRemoteSessionTarget(ctx context.Context, client *http.Client, base string, opts RemoteTabOpenOptions) (serveSessionEntry, error) {
	name := strings.TrimSpace(opts.SessionName)
	if path := strings.TrimSpace(opts.SessionPath); path != "" {
		return serveSessionEntry{Name: name, Path: path, Title: strings.TrimSpace(opts.SessionTitle), Current: true}, nil
	}
	if name == "" {
		return serveCurrentSession(ctx, client, base)
	}
	sessions, err := serveSessions(ctx, client, base)
	if err != nil {
		return serveSessionEntry{}, err
	}
	for _, session := range sessions {
		if session.Name == name {
			session.Current = true
			return session, nil
		}
	}
	return serveSessionEntry{}, fmt.Errorf("remote session %q not found", name)
}

func enterRemoteSessionTarget(ctx context.Context, client *http.Client, base string, opts RemoteTabOpenOptions) (serveSessionEntry, error) {
	name := strings.TrimSpace(opts.SessionName)
	if opts.NewSession {
		path, err := servePostSessionPath(ctx, client, serveURL(base, "/new"), nil)
		if err != nil {
			return serveSessionEntry{}, err
		}
		return serveSessionEntry{Path: path, Current: true}, nil
	}
	if sessionPath := strings.TrimSpace(opts.SessionPath); sessionPath != "" {
		body, err := json.Marshal(map[string]string{"path": sessionPath})
		if err != nil {
			return serveSessionEntry{}, err
		}
		if err := servePost(ctx, client, serveURL(base, "/resume"), body); err != nil {
			return serveSessionEntry{}, err
		}
		return serveSessionEntry{Name: name, Path: sessionPath, Title: strings.TrimSpace(opts.SessionTitle), Current: true}, nil
	}
	// Focus-only attaches retain the current session; only explicit NewSession
	// may abandon it.
	if name == "" {
		current, _ := serveCurrentSession(ctx, client, base)
		return current, nil
	}
	sessions, err := serveSessions(ctx, client, base)
	if err != nil {
		return serveSessionEntry{}, err
	}
	for _, session := range sessions {
		if session.Name != name {
			continue
		}
		body, err := json.Marshal(map[string]string{"path": session.Path})
		if err != nil {
			return serveSessionEntry{}, err
		}
		if err := servePost(ctx, client, serveURL(base, "/resume"), body); err != nil {
			return serveSessionEntry{}, err
		}
		session.Current = true
		return session, nil
	}
	return serveSessionEntry{}, fmt.Errorf("remote session %q not found", name)
}

func serveCurrentSession(ctx context.Context, client *http.Client, base string) (serveSessionEntry, error) {
	sessions, err := serveSessions(ctx, client, base)
	if err != nil {
		return serveSessionEntry{}, err
	}
	for _, session := range sessions {
		if session.Current {
			return session, nil
		}
	}
	return serveSessionEntry{}, nil
}

// installRemoteTabAttachRoute fences listings both before a target is installed
// and after Serve commits it, preventing either stale epoch from restoring the
// former current session.
func installRemoteTabAttachRoute(tab *remoteTab, path string) {
	path = strings.TrimSpace(path)
	if tab.routing.currentPath != path {
		tab.routing.currentPath = path
		tab.routing.pathRevision++
	}
	tab.routing.rehydratingPath = ""
	tab.routing.rehydratingFrames = nil
	tab.session.path = tab.routing.currentPath
	tab.routing.revision++
}

func commitRemoteTabAttachRoute(tab *remoteTab, path string, reset bool) {
	path = strings.TrimSpace(path)
	if reset || tab.routing.currentPath != path {
		resetRemoteTabForegroundRuntimeLocked(tab)
	}
	installRemoteTabAttachRoute(tab, path)
}

// routeRemoteTabFrame tracks background runtime without leaking its frames
// into the foreground reducer. Untagged frames remain legacy-compatible.
func (a *App) routeRemoteTabFrame(tabID string, gen uint64, sessionPath, kind string) bool {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	if tab.routing.running == nil {
		tab.routing.running = map[string]bool{}
	}
	changed := false
	if sessionPath != "" {
		switch kind {
		case "turn_started":
			tab.routing.revision++
			changed = !tab.routing.running[sessionPath]
			tab.routing.running[sessionPath] = true
		case "turn_done":
			tab.routing.revision++
			changed = tab.routing.running[sessionPath]
			tab.routing.running[sessionPath] = false
		}
	}
	foreground := sessionPath == "" || tab.routing.currentPath != "" && sessionPath == tab.routing.currentPath
	_, knownBackground := tab.routing.running[sessionPath]
	// A detached turn can finish while background jobs remain. Its later notice
	// makes /sessions authoritative again, so refresh project-tree rows without
	// forwarding that background notice to the foreground reducer.
	backgroundChanged := !foreground && (changed || kind == "turn_done" || kind == "notice" && knownBackground)
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	if backgroundChanged {
		a.emitRemoteEvent("remote-tab:updated", meta)
	}
	return foreground
}

// adoptRemoteTabFrameCurrent consumes Serve's publication-time foreground
// marker. Unlike the running cache, this marker follows switches initiated by
// other HTTP clients and slash/recovery path changes while a tab stays open.
func (a *App) adoptRemoteTabFrameCurrent(tabID string, gen uint64, sessionPath string, reset bool) {
	if sessionPath == "" {
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
	resetTitle := ""
	if reset {
		resetTitle = a.localizedDefaultTopicTitle()
	}
	a.remoteTabMu.Lock()
	current := a.remoteTabs[tabID]
	if current != tab || current.gen != gen || !adoptRemoteTabSessionPathLocked(current, sessionPath) {
		a.remoteTabMu.Unlock()
		return
	}
	if reset {
		current.session.name = ""
		current.session.newSession = true
		current.session.reset = true
		current.topicTitle = resetTitle
	} else {
		// The routing marker has no title. Stop displaying the previous
		// session's title while the authoritative /sessions row is fetched.
		current.topicTitle = remoteWorkspaceName(current.ref.Workspace)
	}
	meta := remoteTabMetaLocked(current)
	ready := current.state == "ready"
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:updated", meta)
	if !reset {
		a.goRemoteTabSafe("remoteTabAdoptedTitle", func() { a.refreshRemoteTabTitle(tabID) })
	}
	if ready {
		// The frontend treats ready -> ready as a new surface generation. Emit it
		// before the triggering frame is forwarded so that frame is buffered until
		// the newly current session snapshot replaces the old transcript.
		a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: "ready"})
	}
}

// resetRemoteTabForegroundRuntimeLocked drops controller-local prompts and
// runtime state before a fresh session becomes visible. Caller holds
// remoteTabMu.
func resetRemoteTabForegroundRuntimeLocked(tab *remoteTab) {
	tab.pendingEvents = nil
	tab.runtime.revision++
	tab.runtime.running = false
	tab.runtime.turnStartedAt = 0
	tab.runtime.backgroundJobs = 0
	tab.runtime.pendingPrompt = false
	tab.runtime.cancelRequested = false
	tab.runtime.cancellable = false
}

// adoptRemoteTabSessionPathLocked moves foreground-only state to a new session.
// Actionable events belong to the previous controller and must never be replayed
// into the newly hydrated surface. Caller holds remoteTabMu.
func adoptRemoteTabSessionPathLocked(tab *remoteTab, sessionPath string) bool {
	sessionPath = strings.TrimSpace(sessionPath)
	if tab == nil || sessionPath == "" || tab.routing.currentPath == sessionPath {
		return false
	}
	running := tab.routing.running[sessionPath]
	resetRemoteTabForegroundRuntimeLocked(tab)
	tab.routing.currentPath = sessionPath
	tab.routing.pathRevision++
	tab.routing.rehydratingPath = ""
	tab.routing.rehydratingFrames = nil
	tab.routing.revision++
	tab.session.path = sessionPath
	tab.session.newSession = false
	tab.session.reset = false
	tab.runtime.running = running
	tab.runtime.cancellable = running
	return true
}

// remoteTabFramePathUnknown distinguishes a possible foreground recovery or
// slash-command rotation from a path already observed as background work.
func (a *App) remoteTabFramePathUnknown(tabID string, gen uint64, sessionPath, kind string) bool {
	if sessionPath == "" {
		return false
	}
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen || sessionPath == tab.routing.currentPath {
		return false
	}
	_, knownBackground := tab.routing.running[sessionPath]
	if !knownBackground {
		return true
	}
	// Transitional frames are sparse and must remain compatible with a Serve
	// that did not attach sessionCurrent. Revalidate them after a background
	// cache hit; ordinary output is covered by the per-frame marker above.
	switch kind {
	case "turn_started", "approval_request", "ask_request":
		return true
	default:
		return false
	}
}

func (a *App) reconcileRemoteTabFramePath(tabID string, gen uint64, sessionPath string) bool {
	if _, err := a.RemoteTabStatus(tabID); err != nil {
		return false
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return false
	}
	if tab.routing.currentPath == sessionPath {
		a.remoteTabMu.Unlock()
		return true
	}
	// The authoritative status confirmed another foreground path. Remember
	// this tag as background so a lossy detached stream does not synchronously
	// fetch /status for every later token or notice from the same session.
	if tab.routing.running == nil {
		tab.routing.running = map[string]bool{}
	}
	var refresh *TabMeta
	if _, known := tab.routing.running[sessionPath]; !known {
		tab.routing.running[sessionPath] = false
		tab.routing.revision++
		meta := remoteTabMetaLocked(tab)
		refresh = &meta
	}
	a.remoteTabMu.Unlock()
	if refresh != nil {
		a.emitRemoteEvent("remote-tab:updated", *refresh)
	}
	return false
}

func (a *App) routeRemoteTabFrameReconciled(tabID string, gen uint64, sessionPath, kind string) bool {
	pathUnknown := a.remoteTabFramePathUnknown(tabID, gen, sessionPath, kind)
	if a.routeRemoteTabFrame(tabID, gen, sessionPath, kind) {
		return true
	}
	return pathUnknown && a.reconcileRemoteTabFramePath(tabID, gen, sessionPath) &&
		a.routeRemoteTabFrame(tabID, gen, sessionPath, kind)
}

func (a *App) routeRemoteTabWireFrame(tabID string, gen uint64, sessionPath, kind string, current, reset bool) bool {
	if current {
		a.adoptRemoteTabFrameCurrent(tabID, gen, sessionPath, reset)
	}
	return a.routeRemoteTabFrameReconciled(tabID, gen, sessionPath, kind)
}
