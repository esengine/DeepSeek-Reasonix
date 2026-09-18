package main

import (
	"reflect"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

type RuntimeSessionState struct {
	TabID             string                     `json:"tabId"`
	Scope             string                     `json:"scope"`
	WorkspaceRoot     string                     `json:"workspaceRoot"`
	TopicID           string                     `json:"topicId"`
	SessionID         string                     `json:"sessionId,omitempty"`
	SessionPath       string                     `json:"sessionPath"`
	SessionGeneration uint64                     `json:"sessionGeneration"`
	Open              bool                       `json:"open"`
	Remote            bool                       `json:"remote"`
	HostID            string                     `json:"hostId,omitempty"`
	Freshness         string                     `json:"freshness"`
	State             event.RuntimeStateSnapshot `json:"state"`
}

type RuntimeStateProjection struct {
	Epoch    string                `json:"epoch"`
	Revision uint64                `json:"revision"`
	Sessions []RuntimeSessionState `json:"sessions"`
	Topics   []ProjectRuntimeTopic `json:"topics"`
}

type desktopRuntimeProjection struct {
	mu       sync.Mutex
	snapshot RuntimeStateProjection
}

type localRuntimeBindingKey struct {
	key  string
	open bool
}

type localRuntimeBinding struct {
	tab     *WorkspaceTab
	view    RuntimeSessionState
	ctrl    control.SessionAPI
	catalog catalogRuntimeSnapshot
}

// localRuntimeBindingsLocked copies identity and display metadata as one
// binding. App.mu must be held; controller methods must stay outside that lock.
func (a *App) localRuntimeBindingsLocked() map[localRuntimeBindingKey]localRuntimeBinding {
	bindings := make(map[localRuntimeBindingKey]localRuntimeBinding, len(a.tabs)+len(a.detachedSessions))
	collect := func(key string, tab *WorkspaceTab, open bool) {
		if tab == nil {
			return
		}
		bindings[localRuntimeBindingKey{key, open}] = localRuntimeBinding{tab: tab, ctrl: tab.Ctrl,
			view: RuntimeSessionState{TabID: tab.ID, Scope: tab.Scope, WorkspaceRoot: tab.WorkspaceRoot,
				TopicID: tab.TopicID, SessionID: tab.SessionID, SessionPath: tab.SessionPath, SessionGeneration: tab.SessionGeneration, Open: open, Freshness: "synced"},
			catalog: catalogRuntimeSnapshot{tabID: tab.ID, scope: tab.Scope, workspaceRoot: tab.WorkspaceRoot, topicID: tab.TopicID, sessionPath: tab.SessionPath,
				activity: tab.ActivityStatus, topicTitle: tab.TopicTitle, topicTitleSource: tab.topicTitleSource, open: open}}
		if tab.SessionID != "" {
			binding := bindings[localRuntimeBindingKey{key, open}]
			binding.catalog.sessionPath = sessionRoute(tab.SessionID)
			bindings[localRuntimeBindingKey{key, open}] = binding
		}
	}
	for key, tab := range a.tabs {
		collect(key, tab, true)
	}
	for key, tab := range a.detachedSessions {
		collect(key, tab, false)
	}
	return bindings
}

func (a *App) sampleLocalRuntimeBindings() []localRuntimeBinding {
	for {
		a.mu.RLock()
		bindings := a.localRuntimeBindingsLocked()
		a.mu.RUnlock()
		states := make(map[localRuntimeBindingKey]event.RuntimeStateSnapshot, len(bindings))
		for key, binding := range bindings {
			states[key] = controllerRuntimeState(binding.ctrl)
		}
		// A controller can rotate its session, be replaced, or move between
		// open and detached while sampled. Retry the binding set so its state
		// cannot be published under the previous session identity.
		a.mu.RLock()
		current := a.localRuntimeBindingsLocked()
		valid := len(current) == len(bindings)
		for key, binding := range bindings {
			if !sameLocalRuntimeBinding(current[key], binding) {
				valid = false
				break
			}
		}
		a.mu.RUnlock()
		if !valid {
			continue
		}
		result := make([]localRuntimeBinding, 0, len(bindings))
		for key, binding := range bindings {
			binding.view.State = states[key]
			result = append(result, binding)
		}
		return result
	}
}

// sameLocalRuntimeBinding compares the immutable identity and display fields
// copied while App.mu was held. reflect.DeepEqual is deliberately unsuitable
// here: following tab or controller pointers recursively reads their live
// mutex/atomic state and races the controller's runtime-state publisher.
func sameLocalRuntimeBinding(current, sampled localRuntimeBinding) bool {
	return current.tab == sampled.tab &&
		sameSessionAPI(current.ctrl, sampled.ctrl) &&
		current.view.TabID == sampled.view.TabID &&
		current.view.Scope == sampled.view.Scope &&
		current.view.WorkspaceRoot == sampled.view.WorkspaceRoot &&
		current.view.TopicID == sampled.view.TopicID &&
		current.view.SessionID == sampled.view.SessionID &&
		current.view.SessionPath == sampled.view.SessionPath &&
		current.view.SessionGeneration == sampled.view.SessionGeneration &&
		current.view.Open == sampled.view.Open &&
		current.view.Remote == sampled.view.Remote &&
		current.view.HostID == sampled.view.HostID &&
		current.view.Freshness == sampled.view.Freshness &&
		current.catalog.scope == sampled.catalog.scope &&
		current.catalog.workspaceRoot == sampled.catalog.workspaceRoot &&
		current.catalog.topicID == sampled.catalog.topicID &&
		current.catalog.sessionPath == sampled.catalog.sessionPath &&
		current.catalog.activity == sampled.catalog.activity &&
		current.catalog.topicTitle == sampled.catalog.topicTitle &&
		current.catalog.topicTitleSource == sampled.catalog.topicTitleSource &&
		current.catalog.open == sampled.catalog.open
}

func sameSessionAPI(current, sampled control.SessionAPI) bool {
	if current == nil || sampled == nil {
		return current == nil && sampled == nil
	}
	currentValue, sampledValue := reflect.ValueOf(current), reflect.ValueOf(sampled)
	if currentValue.Type() != sampledValue.Type() {
		return false
	}
	if currentValue.Type().Comparable() {
		return currentValue.Interface() == sampledValue.Interface()
	}
	return false
}

func controllerRuntimeState(ctrl control.SessionAPI) event.RuntimeStateSnapshot {
	if reader, ok := ctrl.(control.RuntimeStateReader); ok {
		return reader.RuntimeStateSnapshot()
	}
	if ctrl == nil {
		return event.RuntimeStateSnapshot{Phase: "idle"}
	}
	legacy := ctrl.RuntimeStatus()
	phase := "idle"
	if legacy.Running {
		phase = "executing"
	}
	return event.RuntimeStateSnapshot{Phase: phase, Running: legacy.Running, PendingPrompt: legacy.PendingPrompt,
		BackgroundJobs: legacy.BackgroundJobs, Cancellable: legacy.Cancellable, CancelRequested: legacy.CancelRequested,
		TurnID: legacy.TurnID, TurnStatus: legacy.Status, TurnEventSeq: legacy.TurnEventSeq}
}

func runtimeDisplayStatus(state event.RuntimeStateSnapshot, result string) string {
	switch {
	case state.Phase == "finishing":
		return "finishing"
	case state.CancelRequested:
		return "cancelling"
	case state.PendingPrompt:
		return topicStatusWaitingConfirmation
	case state.Phase == "executing":
		if state.Activity == "streaming" {
			return topicStatusStreaming
		}
		return topicStatusThinking
	case state.BackgroundJobs > 0:
		return topicStatusBackgroundJob
	}
	if result == topicStatusError || result == topicStatusPaused || result == topicStatusAwaitingDelivery {
		return result
	}
	return ""
}

func catalogControllerStatus(ctrl control.SessionAPI, activity string) (string, bool) {
	state := controllerRuntimeState(ctrl)
	return catalogStateStatus(state, activity)
}

func catalogStateStatus(state event.RuntimeStateSnapshot, activity string) (string, bool) {
	if state.SchemaVersion == 1 {
		return runtimeDisplayStatus(state, activity), state.ActiveWork()
	}
	legacy := control.RuntimeStatus{Running: state.Running, PendingPrompt: state.PendingPrompt, BackgroundJobs: state.BackgroundJobs}
	status := catalogRuntimeStatus(activity, legacy)
	return status, status != "" || state.ActiveWork()
}

// GetRuntimeStateSnapshot reads committed controller snapshots after copying
// bindings off App.mu. Controller and remote-tab sampling stay outside the
// projection mutex: archive and runtime-state callbacks also enter here, and
// holding that mutex across a controller read deadlocks a running turn.
func (a *App) GetRuntimeStateSnapshot() RuntimeStateProjection {
	bindings := a.sampleLocalRuntimeBindings()
	remote := a.sampleRemoteRuntimeSessions()
	r := &a.runtimeStateProjection
	r.mu.Lock()
	defer r.mu.Unlock()
	next := RuntimeStateProjection{Epoch: r.snapshot.Epoch, Sessions: []RuntimeSessionState{}}
	catalog := []catalogRuntimeSnapshot{}
	if next.Epoch == "" {
		next.Epoch = newSessionRuntimeID("projection")
	}
	for _, binding := range bindings {
		view := binding.view
		next.Sessions = append(next.Sessions, view)
		if binding.catalog.topicID != "" {
			entry := binding.catalog
			entry.state = &view.State
			catalog = append(catalog, entry)
		}
	}
	next.Topics = a.projectTreeRuntimeTopics(catalog)
	next.Sessions = append(next.Sessions, remote...)
	sort.Slice(next.Sessions, func(i, j int) bool {
		if next.Sessions[i].TabID == next.Sessions[j].TabID {
			return next.Sessions[i].SessionPath < next.Sessions[j].SessionPath
		}
		return next.Sessions[i].TabID < next.Sessions[j].TabID
	})
	next.Revision = r.snapshot.Revision
	if !reflect.DeepEqual(next, r.snapshot) {
		next.Revision++
		r.snapshot = next
	}
	result := r.snapshot
	result.Sessions = append([]RuntimeSessionState{}, result.Sessions...)
	result.Topics = cloneRuntimeTopics(result.Topics)
	return result
}

func (a *App) sampleRemoteRuntimeSessions() []RuntimeSessionState {
	if a == nil {
		return nil
	}
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	sessions := make([]RuntimeSessionState, 0)
	for _, tab := range a.remoteTabs {
		freshness := "synced"
		if tab.state != "ready" || tab.session.takenOver || tab.runtime.syncFailed || tab.runtimeUnknown[tab.routing.currentPath] != 0 {
			freshness = "unknown"
		}
		state := tab.runtimeStates[tab.routing.currentPath]
		if state.SchemaVersion == 0 {
			state = event.RuntimeStateSnapshot{Phase: "idle", Running: tab.runtime.running, PendingPrompt: tab.runtime.pendingPrompt,
				BackgroundJobs: tab.runtime.backgroundJobs, Cancellable: tab.runtime.cancellable, CancelRequested: tab.runtime.cancelRequested}
			if state.Running {
				state.Phase = "executing"
			}
		}
		sessions = append(sessions, RuntimeSessionState{TabID: tab.id, Scope: "remote", HostID: tab.ref.HostID, WorkspaceRoot: tab.ref.Workspace,
			SessionID: remoteRuntimeSessionID(tab.routing.currentPath, tab.session.sessionID, state), SessionPath: tab.routing.currentPath,
			Open: true, Remote: true, Freshness: freshness, State: state})
		for path, background := range tab.runtimeStates {
			if path == tab.routing.currentPath {
				continue
			}
			freshness := "synced"
			// Foreground ownership changes do not invalidate another session,
			// but a failed observation of this background route still does.
			if tab.runtimeUnknown[path] != 0 {
				freshness = "unknown"
			}
			sessions = append(sessions, RuntimeSessionState{TabID: tab.id, Scope: "remote", HostID: tab.ref.HostID, WorkspaceRoot: tab.ref.Workspace,
				SessionID: remoteRuntimeSessionID(path, "", background), SessionPath: path, Remote: true, Freshness: freshness, State: background})
		}
	}
	return sessions
}

func remoteRuntimeSessionID(route, fallback string, state event.RuntimeStateSnapshot) string {
	if id := strings.TrimSpace(state.SessionID); id != "" {
		return id
	}
	if id, ok := parseSessionRoute(route); ok {
		return id
	}
	return strings.TrimSpace(fallback)
}

func (a *App) emitRuntimeStateChanged() {
	if a != nil {
		a.emitRuntimeEvent("runtime-state:changed", a.GetRuntimeStateSnapshot())
	}
}

func (s *tabEventSink) RuntimeStateChanged(snapshot event.RuntimeStateSnapshot) {
	id, app := s.binding()
	if app == nil {
		return
	}
	app.mu.RLock()
	tab := app.tabByEventSinkIDLocked(id)
	var ctrl control.SessionAPI
	if tab != nil {
		ctrl = tab.Ctrl
	}
	app.mu.RUnlock()
	if ctrl == nil {
		return
	}
	current := controllerRuntimeState(ctrl)
	if current.RuntimeEpoch != snapshot.RuntimeEpoch || current.Revision > snapshot.Revision {
		return
	}
	app.emitProjectTreeRuntimeChangedWithLegacy()
}
