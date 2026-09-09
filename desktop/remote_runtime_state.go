package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/event"
)

type remoteRuntimeSync struct {
	mu          sync.Mutex
	pending     bool
	unsupported map[string]string
}

var remoteRuntimeDiagnostics struct {
	stale, conflicts, failures atomic.Uint64
}

func validRuntimeState(state event.RuntimeStateSnapshot) bool {
	return state.SchemaVersion == 1 && state.RuntimeEpoch != "" && state.Revision > 0 && state.BackgroundJobs >= 0 &&
		(state.Phase == "idle" || state.Phase == "executing" || state.Phase == "finishing" || state.Phase == "closed")
}

// acceptRemoteRuntimeStateLocked shares source ordering for GET and SSE. Host
// generation and route validation remain the caller's responsibility.
func acceptRemoteRuntimeStateLocked(tab *remoteTab, path string, next event.RuntimeStateSnapshot, authoritative bool) bool {
	if !validRuntimeState(next) {
		return false
	}
	if path == "" {
		path = tab.routing.currentPath
	}
	if tab.runtimeStates == nil {
		tab.runtimeStates = map[string]event.RuntimeStateSnapshot{}
	}
	previous, found := tab.runtimeStates[path]
	if found && previous.RuntimeEpoch != next.RuntimeEpoch && !authoritative {
		return false
	}
	if !found && !authoritative {
		return false
	}
	if found && previous.RuntimeEpoch == next.RuntimeEpoch {
		if next.Revision < previous.Revision {
			slog.Debug("remote runtime stale snapshot discarded", "source", "serve", "revision", next.Revision, "stale", remoteRuntimeDiagnostics.stale.Add(1))
			return false
		}
		if next.Revision == previous.Revision {
			if previous != next {
				slog.Warn("remote runtime snapshot version conflict", "source", "serve", "revision", next.Revision, "conflicts", remoteRuntimeDiagnostics.conflicts.Add(1))
				return false
			}
			if !authoritative || tab.runtimeUnknown[path] == 0 {
				return false
			}
		}
	}
	tab.runtimeStates[path] = next
	tab.runtime.syncFailed = false
	delete(tab.runtimeUnknown, path)
	delete(tab.runtimeConflicts, path)
	if path == tab.routing.currentPath {
		tab.runtime.snapshot = next
		tab.runtime.running, tab.runtime.pendingPrompt = next.Running, next.PendingPrompt
		tab.runtime.backgroundJobs, tab.runtime.cancellable, tab.runtime.cancelRequested = next.BackgroundJobs, next.Cancellable, next.CancelRequested
	}
	if tab.routing.running == nil {
		tab.routing.running = map[string]bool{}
	}
	tab.routing.running[path] = next.ActiveWork()
	tab.runtime.revision++
	return true
}

func (a *App) acceptRemoteRuntimeFrame(tabID string, gen uint64, path string, frame json.RawMessage) {
	var payload struct {
		State json.RawMessage `json:"runtimeState"`
	}
	if json.Unmarshal(frame, &payload) != nil || len(payload.State) == 0 {
		return
	}
	state, decodeErr := decodeRemoteRuntimeState(payload.State)
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	a.remoteTabMu.Unlock()
	if tab == nil {
		return
	}
	tab.routeEventMu.Lock()
	defer tab.routeEventMu.Unlock()
	a.remoteTabMu.Lock()
	if a.remoteTabs[tabID] != tab || tab.gen != gen {
		a.remoteTabMu.Unlock()
		return
	}
	if path == "" {
		path = tab.routing.currentPath
	}
	if decodeErr != nil {
		markRemoteRuntimeUnknownLocked(tab, path)
		a.remoteTabMu.Unlock()
		a.emitRuntimeStateChanged()
		a.goRemoteTabSafe("remoteRuntimeSync", func() { _, _ = a.SyncRuntimeState() })
		return
	}
	previous, found := tab.runtimeStates[path]
	resync := !found || previous.RuntimeEpoch != state.RuntimeEpoch || (previous.Revision == state.Revision && previous != state)
	if resync {
		if tab.runtimeConflicts[path] == state {
			resync = false
		} else {
			if tab.runtimeConflicts == nil {
				tab.runtimeConflicts = map[string]event.RuntimeStateSnapshot{}
			}
			tab.runtimeConflicts[path] = state
		}
	}
	changed := acceptRemoteRuntimeStateLocked(tab, path, state, false)
	meta := remoteTabMetaLocked(tab)
	a.remoteTabMu.Unlock()
	if changed {
		a.emitRemoteEvent("remote-tab:updated", meta)
		a.emitRuntimeStateChanged()
	} else if resync {
		a.goRemoteTabSafe("remoteRuntimeSync", func() { _, _ = a.SyncRuntimeState() })
	}
}

type remoteRuntimeTarget struct {
	id             string
	gen, selection uint64
	client         *http.Client
	states         map[string]event.RuntimeStateSnapshot
	unknown        map[string]uint64
}
type remoteRuntimeConnection struct {
	id, key, base string
	client        *http.Client
	targets       []remoteRuntimeTarget
}

// SyncRuntimeState reconciles each Serve connection once, regardless of how
// many tabs it owns. It is separate from the memory-only snapshot getter.
func (a *App) SyncRuntimeState() (RuntimeStateProjection, error) {
	syncer := &a.remoteRuntimeSync
	syncer.mu.Lock()
	if syncer.pending {
		syncer.mu.Unlock()
		return a.GetRuntimeStateSnapshot(), nil
	}
	syncer.pending = true
	if syncer.unsupported == nil {
		syncer.unsupported = map[string]string{}
	}
	syncer.mu.Unlock()
	defer func() { syncer.mu.Lock(); syncer.pending = false; syncer.mu.Unlock() }()
	connections := map[string]remoteRuntimeConnection{}
	a.remoteTabMu.Lock()
	for id, tab := range a.remoteTabs {
		if tab.client == nil || tab.state != "ready" {
			continue
		}
		key := fmt.Sprintf("%s\x00%s\x00%s", tab.ref.HostID, tab.ref.Workspace, tab.base)
		conn := connections[key]
		conn.id, conn.key, conn.base, conn.client = id, key, tab.base, tab.client
		states := map[string]event.RuntimeStateSnapshot{}
		maps.Copy(states, tab.runtimeStates)
		conn.targets = append(conn.targets, remoteRuntimeTarget{id, tab.gen, tab.selectionRevision, tab.client, states, maps.Clone(tab.runtimeUnknown)})
		connections[key] = conn
	}
	a.remoteTabMu.Unlock()
	var firstErr error

	for _, conn := range connections {
		if err := a.syncRemoteRuntimeConnection(conn); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		slog.Warn("remote runtime synchronization failed", "source", "serve", "reason", "reconcile", "connections", len(connections), "failures", remoteRuntimeDiagnostics.failures.Add(1))
	}
	a.emitRuntimeStateChanged()
	return a.GetRuntimeStateSnapshot(), firstErr
}

func (a *App) markRemoteRuntimeSyncFailed(targets []remoteRuntimeTarget, failed bool) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	for _, target := range targets {
		if tab := a.remoteTabs[target.id]; tab != nil && tab.gen == target.gen && tab.client == target.client && tab.selectionRevision == target.selection {
			tab.runtime.syncFailed = failed
			if failed {
				markRemoteRuntimeUnknownLocked(tab, tab.routing.currentPath)
				for path := range tab.runtimeStates {
					markRemoteRuntimeUnknownLocked(tab, path)
				}
			}
		}
	}
}

func (a *App) syncRemoteRuntimeConnection(conn remoteRuntimeConnection) error {
	syncer := &a.remoteRuntimeSync
	sort.Slice(conn.targets, func(i, j int) bool { return conn.targets[i].id < conn.targets[j].id })
	first := conn.targets[0]
	capabilityKey := fmt.Sprintf("%s\x00%s\x00%d\x00%p", conn.key, first.id, first.gen, first.client)
	syncer.mu.Lock()
	unsupported := syncer.unsupported[conn.key] == capabilityKey
	syncer.mu.Unlock()
	if unsupported {
		_, err := a.RemoteTabStatus(conn.id)
		a.markRemoteRuntimeSyncFailed(conn.targets, err != nil)
		return err
	}
	ctx, cancel := context.WithTimeout(a.bootContext(), 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serveURL(conn.base, "/runtime-states"), nil)
	if err != nil {
		cancel()
		a.markRemoteRuntimeSyncFailed(conn.targets, true)
		return err
	}
	resp, err := conn.client.Do(req)
	if err != nil {
		cancel()
		a.markRemoteRuntimeSyncFailed(conn.targets, true)
		return err
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		resp.Body.Close()
		cancel()
		syncer.mu.Lock()
		syncer.unsupported[conn.key] = capabilityKey
		syncer.mu.Unlock()
		_, err := a.RemoteTabStatus(conn.id)
		a.markRemoteRuntimeSyncFailed(conn.targets, err != nil)
		return err
	}
	var payload struct {
		SchemaVersion int `json:"schemaVersion"`
		Sessions      []struct {
			SessionPath string          `json:"sessionPath"`
			State       json.RawMessage `json:"state"`
		} `json:"sessions"`
	}
	err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(&payload)
	resp.Body.Close()
	cancel()
	if err != nil || resp.StatusCode != http.StatusOK || payload.SchemaVersion != 1 || payload.Sessions == nil {
		a.markRemoteRuntimeSyncFailed(conn.targets, true)
		return fmt.Errorf("runtime state synchronization failed")
	}
	states := make(map[string]event.RuntimeStateSnapshot, len(payload.Sessions))
	for _, session := range payload.Sessions {
		state, decodeErr := decodeRemoteRuntimeState(session.State)
		_, duplicate := states[session.SessionPath]
		if decodeErr != nil || session.SessionPath == "" || duplicate {
			a.markRemoteRuntimeSyncFailed(conn.targets, true)
			return fmt.Errorf("invalid runtime state synchronization payload")
		}
		states[session.SessionPath] = state
	}
	a.applyRemoteRuntimeSnapshot(conn, states)
	return nil
}

func (a *App) applyRemoteRuntimeSnapshot(conn remoteRuntimeConnection, states map[string]event.RuntimeStateSnapshot) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	for _, target := range conn.targets {
		tab := a.remoteTabs[target.id]
		if tab == nil || tab.base != conn.base || tab.gen != target.gen || tab.client != target.client || tab.selectionRevision != target.selection || tab.routing.rehydratingPath != "" {
			continue
		}
		tab.runtime.syncFailed = false
		seen := map[string]bool{}
		for path, state := range states {
			seen[path] = true
			// A failure or malformed frame observed after this GET started
			// requires a fresh request, even when the cached facts are unchanged.
			if tab.runtimeUnknown[path] != target.unknown[path] {
				continue
			}
			previous := tab.runtimeStates[path]
			// A GET can establish an epoch only if no newer source update
			// has changed this binding while it was in flight.
			if previous.RuntimeEpoch != state.RuntimeEpoch && previous != target.states[path] {
				continue
			}
			acceptRemoteRuntimeStateLocked(tab, path, state, true)
		}
		if !seen[tab.routing.currentPath] && tab.runtimeStates[tab.routing.currentPath] == target.states[tab.routing.currentPath] {
			markRemoteRuntimeUnknownLocked(tab, tab.routing.currentPath)
		}
		for path, state := range tab.runtimeStates {
			if !seen[path] && path != tab.routing.currentPath && state == target.states[path] && tab.runtimeUnknown[path] == target.unknown[path] {
				delete(tab.runtimeStates, path)
				delete(tab.runtimeUnknown, path)
				delete(tab.routing.running, path)
			}
		}
	}
}

func markRemoteRuntimeUnknownLocked(tab *remoteTab, path string) {
	if tab.runtimeUnknown == nil {
		tab.runtimeUnknown = map[string]uint64{}
	}
	tab.runtime.revision++
	tab.runtimeUnknown[path] = tab.runtime.revision
}
