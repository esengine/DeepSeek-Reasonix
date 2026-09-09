package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
)

// takeoverAfterAdoptGrantHookForTest pauses a direct local-session adoption
// after Serve rotates the mirror generation but before Desktop publishes it.
// Production leaves it nil.
var takeoverAfterAdoptGrantHookForTest func()

var discoverLocalTakeoverServesForAdopt = discoverLocalTakeoverServes

func (a *App) adoptSessionFromLocalServe(tabID, sessionPath string) {
	if a.adoptSessionFromLocalServeOnce(tabID, sessionPath) {
		return
	}
	// The announce can race a restart, token rotation, or transient discovery.
	// Retry once after backoff while the tab still shows the session; otherwise
	// the remote side can see the foreign lease but cannot reclaim it cleanly.
	time.AfterFunc(serveProbeBackoffWindow, func() {
		if tab := a.tabByID(tabID); tab != nil && !tab.ReadOnly &&
			strings.TrimSpace(tab.currentSessionPath()) == strings.TrimSpace(sessionPath) {
			a.adoptSessionFromLocalServeOnce(tabID, sessionPath)
		}
	})
}

type takeoverAdoptFence struct {
	tab      *WorkspaceTab
	sink     *tabEventSink
	epoch    string
	revision uint64
}

func (a *App) beginTakeoverAdopt(tabID, sessionPath, key string) (takeoverAdoptFence, bool) {
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	fence := takeoverAdoptFence{tab: tab}
	valid := tab != nil && !tab.ReadOnly && sessionRuntimeKey(tab.currentSessionPath()) == key && tab.sink != nil
	if valid {
		fence.sink = tab.sink
		fence.epoch = a.runtimeEpochForTabLocked(tab)
	}
	a.mu.RUnlock()
	if !valid {
		return takeoverAdoptFence{}, false
	}
	a.takeoverMu.Lock()
	defer a.takeoverMu.Unlock()
	if a.takeoverMirrors[key] != nil {
		return takeoverAdoptFence{}, false
	}
	if a.takeoverAdoptRevisions == nil {
		a.takeoverAdoptRevisions = map[string]uint64{}
	}
	fence.revision = a.takeoverAdoptRevisions[key] + 1
	a.takeoverAdoptRevisions[key] = fence.revision
	return fence, true
}

func newTakeoverMirror(app *App, key, tabID, sessionPath string, sink *tabEventSink, record takeoverServeRecord, client *http.Client, grant takeoverGrant) *takeoverMirror {
	return &takeoverMirror{
		app: app, key: key, tabID: tabID, sessionPath: sessionPath, sink: sink,
		record: record, client: client, grant: grant, bindingRevision: 1,
		stop: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1),
	}
}

// commitTakeoverAdopt publishes an adoption only while the initiating tab,
// runtime epoch, session path, sink, and adoption revision are all current.
// runtimeRebuildMu closes the final validation-to-attach gap against session
// switches; an older overlapping /adopt response loses to the latest revision.
func (a *App) commitTakeoverAdopt(fence takeoverAdoptFence, key, tabID, sessionPath string, record takeoverServeRecord, client *http.Client, grant takeoverGrant) bool {
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	valid := tab != nil && tab == fence.tab && !tab.ReadOnly && tab.sink == fence.sink &&
		a.runtimeEpochForTabLocked(tab) == fence.epoch && sessionRuntimeKey(tab.currentSessionPath()) == key
	a.mu.RUnlock()
	if !valid {
		return false
	}
	m := newTakeoverMirror(a, key, tabID, sessionPath, fence.sink, record, client, grant)
	a.takeoverMu.Lock()
	currentRevision := a.takeoverAdoptRevisions[key]
	if currentRevision != fence.revision || a.takeoverMirrors[key] != nil {
		a.takeoverMu.Unlock()
		return false
	}
	if a.takeoverMirrors == nil {
		a.takeoverMirrors = map[string]*takeoverMirror{}
	}
	a.takeoverMirrors[key] = m
	delete(a.takeoverAdoptRevisions, key)
	a.takeoverMu.Unlock()
	fence.sink.setTakeoverMirror(m)
	go m.run(client, record)
	return true
}

// adoptSessionFromLocalServeOnce announces a directly-opened local session to
// a resident serve. It reports false when no serve could be told, so the
// caller can retry.
func (a *App) adoptSessionFromLocalServeOnce(tabID, sessionPath string) bool {
	key := sessionRuntimeKey(sessionPath)
	if key == "" {
		return true
	}
	fence, attempt := a.beginTakeoverAdopt(tabID, sessionPath, key)
	if !attempt {
		return true
	}
	defer func() {
		a.takeoverMu.Lock()
		if a.takeoverAdoptRevisions[key] == fence.revision && a.takeoverMirrors[key] == nil {
			delete(a.takeoverAdoptRevisions, key)
		}
		a.takeoverMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, serve := range discoverLocalTakeoverServesForAdopt() {
		workspaceDir := config.ProjectSessionDir(serve.state.Workspace)
		if workspaceDir == "" || !pathWithinDir(sessionPath, workspaceDir) {
			continue
		}
		client, err := takeoverClient(ctx, serve)
		if err != nil {
			continue
		}
		view, err := takeoverOwnership(ctx, client, serve.base, sessionPath)
		if err != nil {
			continue
		}
		if view.Holder == "serve" || view.Holder == "external" {
			return true
		}
		body, err := json.Marshal(map[string]string{"sessionPath": sessionPath, "writerId": agent.SessionWriterID()})
		if err != nil {
			continue
		}
		resp, err := serveDo(ctx, client, http.MethodPost, serveURL(serve.base, "/adopt"), body)
		if err != nil {
			continue
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var grant takeoverGrant
		if json.Unmarshal(respBody, &grant) != nil || grant.MirrorID == "" || grant.ReturnHandoffID == "" || grant.SourceWriterID == "" ||
			grant.TargetWriterID != agent.SessionWriterID() || sessionRuntimeKey(grant.SessionPath) != key {
			continue
		}
		if hook := takeoverAfterAdoptGrantHookForTest; hook != nil {
			hook()
		}
		if !a.commitTakeoverAdopt(fence, key, tabID, sessionPath, serve, client, grant) {
			a.endFailedTakeover(serve, client, grant)
			return true
		}
		slog.Info("desktop: local session adopted by serve for remote spectating",
			"tab", tabID, "session", sessionPath, "serve", serve.base)
		return true
	}
	return false
}
