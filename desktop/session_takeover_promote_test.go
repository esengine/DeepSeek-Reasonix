package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestMarkLocalTakeoverSpectatorPublishesAndPersistsReclaimState(t *testing.T) {
	app := NewApp()
	tab := &WorkspaceTab{ID: "spectator", Scope: "global", ReadOnly: true}
	app.mu.Lock()
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.mu.Unlock()

	app.markLocalTakeoverSpectator(tab)
	meta := app.tabMeta(tab, true)
	if !tab.Takeover.Spectator || !meta.TakenOver || !meta.ReadOnly {
		t.Fatalf("spectator state = tab=%v meta=%+v", tab.Takeover.Spectator, meta)
	}
	app.mu.Lock()
	_, entries, _, _ := app.saveTabsCollectLocked()
	app.mu.Unlock()
	if len(entries) != 1 || !entries[0].TakeoverSpectator || !entries[0].ReadOnly {
		t.Fatalf("persisted spectator entries = %+v", entries)
	}
}

func TestTakeoverSessionPromotesLocalSpectatorFromFreshDiskState(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "round-trip.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "remote latest"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	sourceLease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	sourceWriterID := agent.SessionWriterID()

	oldCtrl := control.New(control.Options{
		Executor:    agent.New(nil, nil, agent.NewSession("stale"), agent.Options{}, event.Discard),
		SessionDir:  dir,
		SessionPath: path,
	})
	tab := &WorkspaceTab{
		ID: "local-spectator", Scope: "global", SessionPath: path,
		ReadOnly: true, Ctrl: oldCtrl, Ready: true,
	}
	tab.Takeover.Spectator = true
	app := NewApp()
	app.ctx = context.Background()
	tab.sink = &tabEventSink{tabID: tab.ID, app: app, ctx: app.ctx}
	app.mu.Lock()
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	app.advanceSessionRuntimeEpochLocked(tab)
	app.mu.Unlock()
	t.Cleanup(func() {
		if mirror := app.takeoverMirrorForKey(sessionRuntimeKey(path)); mirror != nil {
			mirror.stopAndFinalize(false)
		}
		app.mu.RLock()
		ctrl := tab.Ctrl
		app.mu.RUnlock()
		if ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
		sourceLease.Release()
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/handoff":
			if err := sourceLease.ReleaseForHandoff(sourceWriterID, "forward"); err != nil {
				t.Error(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(takeoverGrant{
				SessionPath: path, MirrorID: "mirror", HandoffID: "forward", ReturnHandoffID: "return",
				SourceWriterID: sourceWriterID, TargetWriterID: agent.SessionWriterID(),
			})
		case "/external/frames":
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
		case "/mirror-end":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	originalFind := takeoverFindTargetForTest
	takeoverFindTargetForTest = func(context.Context, *App, string) (takeoverServeRecord, *http.Client, SessionTakeoverView, error) {
		return takeoverServeRecord{base: srv.URL}, srv.Client(), SessionTakeoverView{Holder: "serve"}, nil
	}
	originalBuild := takeoverBuildLocalSpectatorCandidateForTest
	loadedFreshState := false
	takeoverBuildLocalSpectatorCandidateForTest = func(a *App, candidateTab *WorkspaceTab, source tabRuntimeSnapshot, candidatePath string, loaded *agent.Session) (*sessionRebindCandidate, error) {
		for _, message := range loaded.Snapshot() {
			if message.Role == provider.RoleUser && message.Content == "remote latest" {
				loadedFreshState = true
			}
		}
		ctrl := control.New(control.Options{
			Executor:   agent.New(nil, nil, loaded, agent.Options{}, event.Discard),
			SessionDir: dir, SessionPath: candidatePath,
		})
		return &sessionRebindCandidate{
			app: a, ctrl: ctrl, sink: &tabEventSink{tabID: candidateTab.ID, app: a},
			model: source.model, runtime: source.normalizedRuntime(),
		}, nil
	}
	t.Cleanup(func() {
		takeoverFindTargetForTest = originalFind
		takeoverBuildLocalSpectatorCandidateForTest = originalBuild
	})

	if err := app.TakeoverSession(tab.ID, "wait"); err != nil {
		t.Fatal(err)
	}
	if !loadedFreshState {
		t.Fatal("replacement did not reload the transcript after targeted lease acquisition")
	}
	app.mu.RLock()
	replacement := tab.Ctrl
	readOnly, spectator := tab.ReadOnly, tab.Takeover.Spectator
	app.mu.RUnlock()
	if replacement == nil || replacement == oldCtrl || readOnly || spectator {
		t.Fatalf("promoted tab = ctrl %p old %p readOnly=%v spectator=%v", replacement, oldCtrl, readOnly, spectator)
	}
	if got := tab.sessionLeaseRuntimeKey(); got != sessionRuntimeKey(path) {
		t.Fatalf("promoted lease = %q, want %q", got, sessionRuntimeKey(path))
	}
	if meta := app.tabMeta(tab, true); meta.TakenOver || meta.ReadOnly {
		t.Fatalf("promoted meta remained read-only: %+v", meta)
	}
}

func TestTakeoverSessionBuildFailureKeepsLocalSpectatorAndReturnsLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "failed-round-trip.jsonl")
	session := agent.NewSession("system")
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	sourceLease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	sourceWriterID := agent.SessionWriterID()
	oldCtrl := control.New(control.Options{
		Executor:   agent.New(nil, nil, session, agent.Options{}, event.Discard),
		SessionDir: dir, SessionPath: path,
	})
	tab := &WorkspaceTab{
		ID: "failed-local-spectator", Scope: "global", SessionPath: path,
		ReadOnly: true, Ctrl: oldCtrl, Ready: true,
	}
	tab.Takeover.Spectator = true
	app := NewApp()
	app.ctx = context.Background()
	tab.sink = &tabEventSink{tabID: tab.ID, app: app, ctx: app.ctx}
	app.mu.Lock()
	app.tabs[tab.ID] = tab
	app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	app.advanceSessionRuntimeEpochLocked(tab)
	app.mu.Unlock()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/handoff":
			if err := sourceLease.ReleaseForHandoff(sourceWriterID, "forward"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(takeoverGrant{
				SessionPath: path, MirrorID: "failed-mirror", HandoffID: "forward", ReturnHandoffID: "return",
				SourceWriterID: sourceWriterID, TargetWriterID: agent.SessionWriterID(),
			})
		case "/mirror-end":
			w.WriteHeader(http.StatusNoContent)
		default:
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
		}
	}))
	defer srv.Close()

	originalFind := takeoverFindTargetForTest
	takeoverFindTargetForTest = func(context.Context, *App, string) (takeoverServeRecord, *http.Client, SessionTakeoverView, error) {
		return takeoverServeRecord{base: srv.URL}, srv.Client(), SessionTakeoverView{Holder: "serve"}, nil
	}
	originalBuild := takeoverBuildLocalSpectatorCandidateForTest
	buildErr := errors.New("injected rebuild failure")
	takeoverBuildLocalSpectatorCandidateForTest = func(*App, *WorkspaceTab, tabRuntimeSnapshot, string, *agent.Session) (*sessionRebindCandidate, error) {
		return nil, buildErr
	}
	t.Cleanup(func() {
		takeoverFindTargetForTest = originalFind
		takeoverBuildLocalSpectatorCandidateForTest = originalBuild
		if mirror := app.takeoverMirrorForKey(sessionRuntimeKey(path)); mirror != nil {
			mirror.stopAndFinalize(false)
		}
		oldCtrl.Close()
		sourceLease.Release()
	})

	if err := app.TakeoverSession(tab.ID, "wait"); !errors.Is(err, buildErr) || !strings.Contains(err.Error(), "injected rebuild failure") {
		t.Fatalf("TakeoverSession error = %v, want injected failure", err)
	}
	app.mu.RLock()
	gotCtrl, readOnly, spectator := tab.Ctrl, tab.ReadOnly, tab.Takeover.Spectator
	app.mu.RUnlock()
	if gotCtrl != oldCtrl || !readOnly || !spectator || tab.sessionLeaseRuntimeKey() != "" {
		t.Fatalf("failed promotion changed spectator: ctrl=%p old=%p readOnly=%v spectator=%v lease=%q", gotCtrl, oldCtrl, readOnly, spectator, tab.sessionLeaseRuntimeKey())
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		info, loadErr := agent.LoadSessionLeaseInfo(path)
		if loadErr == nil && info != nil && info.HandoffTo == sourceWriterID && info.HandoffID == "return" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("failed promotion did not publish reverse reservation: info=%+v err=%v", info, loadErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
