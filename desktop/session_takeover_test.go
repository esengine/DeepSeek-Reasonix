package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/remote/bootstrap"
)

func TestTakeoverOwnershipEncodesOpaqueSessionPath(t *testing.T) {
	want := `C:\Users\测试 User\sessions\a&b%20.jsonl`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("session"); got != want {
			t.Errorf("session query = %q, want %q", got, want)
		}
		_ = json.NewEncoder(w).Encode(SessionTakeoverView{Holder: "serve"})
	}))
	defer srv.Close()
	if _, err := takeoverOwnership(context.Background(), srv.Client(), srv.URL, want); err != nil {
		t.Fatal(err)
	}
}

func TestTakeoverGrantCannotCommitAfterTabRuntimeChanges(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := filepath.Join(t.TempDir(), "taken-over.jsonl")
	source, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	sourceWriter := agent.SessionWriterID()
	var sourceReleased atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/handoff":
			if !sourceReleased.CompareAndSwap(false, true) {
				t.Error("handoff called more than once")
				w.WriteHeader(http.StatusConflict)
				return
			}
			if err := source.ReleaseForHandoff(sourceWriter, "forward"); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(takeoverGrant{
				SessionPath: path, MirrorID: "mirror", HandoffID: "forward", ReturnHandoffID: "return",
				SourceWriterID: sourceWriter, TargetWriterID: sourceWriter,
			})
		case "/mirror-end":
			returned, err := agent.TryAcquireSessionLeaseWithHandoff(path, sourceWriter, "forward")
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusConflict)
				return
			}
			returned.Release()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "takeover-tab")
	tab.SessionPath = path
	tab.StartupErrLeaseHeld = true
	tab.StartupErr = (&sessionLeaseBusyError{}).Error()
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.mu.Lock()
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	app.mu.Unlock()

	originalFind := takeoverFindTargetForTest
	takeoverFindTargetForTest = func(context.Context, *App, string) (takeoverServeRecord, *http.Client, SessionTakeoverView, error) {
		return takeoverServeRecord{base: srv.URL}, srv.Client(), SessionTakeoverView{Holder: "serve"}, nil
	}
	grantSeen := make(chan struct{})
	runtimeChanged := make(chan struct{})
	originalHook := takeoverAfterGrantHookForTest
	takeoverAfterGrantHookForTest = func() {
		close(grantSeen)
		<-runtimeChanged
	}
	t.Cleanup(func() {
		takeoverFindTargetForTest = originalFind
		takeoverAfterGrantHookForTest = originalHook
	})

	newPath := filepath.Join(t.TempDir(), "replacement.jsonl")
	replacement := control.New(control.Options{
		Executor:    agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard),
		SessionPath: newPath,
		Sink:        event.Discard,
	})
	defer replacement.Close()
	done := make(chan error, 1)
	go func() { done <- app.TakeoverSession(tab.ID, "wait") }()
	<-grantSeen
	app.runtimeRebuildMu.Lock()
	app.mu.Lock()
	tab.SessionPath = newPath
	tab.Ctrl = replacement
	tab.StartupErrLeaseHeld = false
	app.mu.Unlock()
	app.runtimeRebuildMu.Unlock()
	close(runtimeChanged)
	if err := <-done; err == nil {
		t.Fatal("stale takeover unexpectedly committed")
	}
	if tab.Ctrl != replacement || tab.currentSessionPath() != newPath {
		t.Fatalf("replacement runtime was overwritten: ctrl=%p path=%q", tab.Ctrl, tab.currentSessionPath())
	}
	if got := tab.sessionLeaseRuntimeKey(); got != "" {
		t.Fatalf("stale takeover installed lease %q", got)
	}
	if mirror := app.takeoverMirrorForKey(sessionRuntimeKey(path)); mirror != nil {
		t.Fatal("stale takeover installed a mirror")
	}
}

func TestDirectAdoptGrantCannotAttachAfterTabRuntimeChanges(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := t.TempDir()
	sessionDir := config.ProjectSessionDir(workspace)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "adopted.jsonl")
	mirrorEnded := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/ownership":
			_ = json.NewEncoder(w).Encode(SessionTakeoverView{Holder: "free"})
		case "/adopt":
			_ = json.NewEncoder(w).Encode(takeoverGrant{
				SessionPath: path, MirrorID: "stale-mirror", ReturnHandoffID: "stale-return",
				SourceWriterID: "serve-writer", TargetWriterID: agent.SessionWriterID(),
			})
		case "/mirror-end":
			mirrorEnded <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "adopt-tab")
	tab.SessionPath = path
	oldSink := &tabEventSink{tabID: tab.ID, app: app}
	tab.sink = oldSink
	app.mu.Lock()
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	app.advanceSessionRuntimeEpochLocked(tab)
	app.mu.Unlock()

	originalDiscover := discoverLocalTakeoverServesForAdopt
	discoverLocalTakeoverServesForAdopt = func() []takeoverServeRecord {
		return []takeoverServeRecord{{base: srv.URL, token: "fresh", state: bootstrap.ServeState{Workspace: workspace}}}
	}
	grantSeen := make(chan struct{})
	runtimeChanged := make(chan struct{})
	originalHook := takeoverAfterAdoptGrantHookForTest
	takeoverAfterAdoptGrantHookForTest = func() {
		close(grantSeen)
		<-runtimeChanged
	}
	t.Cleanup(func() {
		discoverLocalTakeoverServesForAdopt = originalDiscover
		takeoverAfterAdoptGrantHookForTest = originalHook
	})

	done := make(chan bool, 1)
	go func() { done <- app.adoptSessionFromLocalServeOnce(tab.ID, path) }()
	<-grantSeen
	newPath := filepath.Join(sessionDir, "replacement.jsonl")
	newSink := &tabEventSink{tabID: tab.ID, app: app}
	app.runtimeRebuildMu.Lock()
	app.mu.Lock()
	tab.SessionPath = newPath
	tab.sink = newSink
	app.advanceSessionRuntimeEpochLocked(tab)
	app.mu.Unlock()
	app.runtimeRebuildMu.Unlock()
	close(runtimeChanged)
	if !<-done {
		t.Fatal("stale adoption did not reach a serve")
	}
	if mirror := app.takeoverMirrorForKey(sessionRuntimeKey(path)); mirror != nil {
		t.Fatal("stale adoption installed a mirror")
	}
	if oldSink.takeoverMirror.Load() != nil || newSink.takeoverMirror.Load() != nil {
		t.Fatal("stale adoption attached to an old or replacement event sink")
	}
	select {
	case <-mirrorEnded:
	case <-time.After(3 * time.Second):
		t.Fatal("stale mirror generation was not ended")
	}
}

func TestTakeoverMirrorRetainsFailedReturnUntilReservationSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pending-return.jsonl")
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	var mirrorEnded atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mirror-end" {
			mirrorEnded.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	m := &takeoverMirror{
		sessionPath: path,
		client:      srv.Client(),
		record:      takeoverServeRecord{base: srv.URL},
		grant:       takeoverGrant{MirrorID: "mirror", SourceWriterID: "serve", ReturnHandoffID: "return"},
		wake:        make(chan struct{}, 1),
	}
	wantErr := errors.New("injected reservation write failure")
	var calls atomic.Int32
	m.releaseHandoff = func(candidate *agent.SessionLease, writerID, handoffID string) error {
		if calls.Add(1) == 1 {
			return wantErr
		}
		return candidate.ReleaseForHandoff(writerID, handoffID)
	}
	m.holdPendingReturn(lease)
	if m.retryPendingReturn(true) {
		t.Fatal("first retry unexpectedly succeeded")
	}
	if mirrorEnded.Load() {
		t.Fatal("mirror ended before the reverse reservation existed")
	}
	if third, err := agent.TryAcquireSessionLease(path); !errors.Is(err, agent.ErrSessionLeaseHeld) {
		if third != nil {
			third.Release()
		}
		t.Fatalf("third writer acquired pending lease: %v", err)
	}
	if !m.retryPendingReturn(true) {
		t.Fatal("second retry did not publish the reservation")
	}
	m.mirrorEnd()
	if !mirrorEnded.Load() {
		t.Fatal("mirror-end was not sent after reservation success")
	}
	info, err := agent.LoadSessionLeaseInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.HandoffTo != "serve" || info.HandoffID != "return" {
		t.Fatalf("return reservation = %+v", info)
	}
}

func TestTakeoverMirrorRetriesSameDrainedFramesInOrder(t *testing.T) {
	var mu sync.Mutex
	var batches [][]eventwire.Event
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		batches = append(batches, append([]eventwire.Event(nil), body.Frames...))
		mu.Unlock()
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()

	m := &takeoverMirror{
		sessionPath: "session.jsonl", client: srv.Client(),
		record: takeoverServeRecord{base: srv.URL}, grant: takeoverGrant{MirrorID: "mirror-1"},
		wake: make(chan struct{}, 1),
	}
	m.forwardEvent(event.Event{Kind: event.Text, Text: "first"})
	m.forwardEvent(event.Event{Kind: event.Text, Text: "second"})
	if !m.pushOnce(false) {
		t.Fatal("forwarder stopped unexpectedly")
	}
	if !m.pushOnce(false) {
		t.Fatal("forwarder stopped unexpectedly")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 2 {
		t.Fatalf("batches = %+v, want two complete batches", batches)
	}
	for i := range batches[0] {
		if batches[0][i].Text != batches[1][i].Text {
			t.Fatalf("retry reordered frame %d: %q != %q", i, batches[0][i].Text, batches[1][i].Text)
		}
	}
}

func TestTakeoverMirrorReadoptsAfterServeMoves(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := t.TempDir()
	sessionDir := config.ProjectSessionDir(workspace)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "session.jsonl")
	deadServe := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := deadServe.URL
	deadClient := deadServe.Client()
	deadServe.Close()
	var delivered atomic.Bool
	newServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/adopt":
			_ = json.NewEncoder(w).Encode(takeoverGrant{
				SessionPath: path, MirrorID: "new", ReturnHandoffID: "return", SourceWriterID: "source", TargetWriterID: agent.SessionWriterID(),
			})
		case "/external/frames":
			delivered.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer newServe.Close()
	originalDiscover := discoverLocalTakeoverServesForMirror
	discoverLocalTakeoverServesForMirror = func() []takeoverServeRecord {
		return []takeoverServeRecord{{base: newServe.URL, token: "fresh", state: bootstrap.ServeState{Workspace: workspace}}}
	}
	t.Cleanup(func() { discoverLocalTakeoverServesForMirror = originalDiscover })
	m := &takeoverMirror{
		sessionPath: path,
		client:      deadClient,
		record:      takeoverServeRecord{base: deadURL},
		grant:       takeoverGrant{MirrorID: "old"},
		wake:        make(chan struct{}, 1),
	}
	m.bindingRevision = 1
	m.forwardEvent(event.Event{Kind: event.Text, Text: "recover"})
	if !m.pushOnce(false) {
		t.Fatal("desktop mirror stopped during re-adoption")
	}
	if !m.pushOnce(false) {
		t.Fatal("desktop mirror stopped after re-adoption")
	}
	if !delivered.Load() {
		t.Fatal("desktop mirror did not re-adopt and deliver through the new serve")
	}
	_, record, _, grant, revision := m.snapshotBinding()
	if record.base != newServe.URL || grant.MirrorID != "new" || revision <= 1 {
		t.Fatalf("re-adopted binding = base %q grant %+v revision %d", record.base, grant, revision)
	}
}

func TestTakeoverMirrorDemotionReturnsRefreshedGeneration(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := t.TempDir()
	sessionDir := config.ProjectSessionDir(workspace)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "demote.jsonl")
	oldStarted := make(chan struct{})
	oldRelease := make(chan struct{})
	var oldEnds atomic.Int32
	oldServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/external/frames":
			close(oldStarted)
			<-oldRelease
			w.WriteHeader(http.StatusUnauthorized)
		case "/mirror-end":
			oldEnds.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer oldServe.Close()
	newEnds := make(chan string, 1)
	newServe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			w.WriteHeader(http.StatusNoContent)
		case "/adopt":
			_ = json.NewEncoder(w).Encode(takeoverGrant{
				SessionPath: path, MirrorID: "mirror-new", ReturnHandoffID: "return-new",
				SourceWriterID: "serve-new", TargetWriterID: agent.SessionWriterID(),
			})
		case "/mirror-end":
			var body struct {
				MirrorID string `json:"mirrorId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			newEnds <- body.MirrorID
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer newServe.Close()
	originalDiscover := discoverLocalTakeoverServesForMirror
	discoverLocalTakeoverServesForMirror = func() []takeoverServeRecord {
		return []takeoverServeRecord{{base: newServe.URL, token: "fresh", state: bootstrap.ServeState{Workspace: workspace}}}
	}
	t.Cleanup(func() { discoverLocalTakeoverServesForMirror = originalDiscover })

	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{ID: "demote-tab"}
	tab.adoptSessionLease(lease)
	app := NewApp()
	m := newTakeoverMirror(app, sessionRuntimeKey(path), tab.ID, path, nil,
		takeoverServeRecord{base: oldServe.URL}, oldServe.Client(),
		takeoverGrant{SessionPath: path, MirrorID: "mirror-old", ReturnHandoffID: "return-old", SourceWriterID: "serve-old", TargetWriterID: agent.SessionWriterID()},
	)
	m.forwardEvent(event.Event{Kind: event.Text, Text: "generation fence"})
	pushDone := make(chan bool, 1)
	go func() { pushDone <- m.pushOnce(false) }()
	<-oldStarted
	returnDone := make(chan error, 1)
	go func() { returnDone <- m.returnLeaseForDemotion(tab) }()
	close(oldRelease)
	if !<-pushDone {
		t.Fatal("forwarder stopped while re-adopting")
	}
	if err := <-returnDone; err != nil {
		t.Fatal(err)
	}
	info, err := agent.LoadSessionLeaseInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil || info.HandoffTo != "serve-new" || info.HandoffID != "return-new" {
		t.Fatalf("reverse reservation = %+v", info)
	}
	select {
	case mirrorID := <-newEnds:
		if mirrorID != "mirror-new" {
			t.Fatalf("mirror-end id = %q, want refreshed generation", mirrorID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("refreshed mirror generation was not ended")
	}
	if oldEnds.Load() != 0 || tab.sessionLeaseRuntimeKey() != "" || !m.returned.Load() {
		t.Fatalf("old ends=%d tab lease=%q returned=%v", oldEnds.Load(), tab.sessionLeaseRuntimeKey(), m.returned.Load())
	}
}

func TestTakeoverMirrorChunksWithoutDroppingFrames(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Frames []eventwire.Event `json:"frames"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Frames) > takeoverMirrorMaxQueue {
			t.Fatalf("batch size = %d", len(body.Frames))
		}
		for _, frame := range body.Frames {
			got = append(got, frame.Text)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"reclaimRequested": false})
	}))
	defer srv.Close()
	m := &takeoverMirror{
		sessionPath: "session.jsonl", client: srv.Client(), record: takeoverServeRecord{base: srv.URL},
		grant: takeoverGrant{MirrorID: "mirror-1"}, wake: make(chan struct{}, 1),
	}
	for i := range takeoverMirrorMaxQueue + 37 {
		m.forwardEvent(event.Event{Kind: event.Text, Text: strconv.Itoa(i)})
	}
	if !m.pushOnce(false) {
		t.Fatal("chunked frame push stopped unexpectedly")
	}
	if !m.pushOnce(false) {
		t.Fatal("chunked frame push stopped unexpectedly")
	}
	if len(got) != takeoverMirrorMaxQueue+37 {
		t.Fatalf("received %d frames", len(got))
	}
	for i, text := range got {
		if text != strconv.Itoa(i) {
			t.Fatalf("frame %d = %q", i, text)
		}
	}
}

func TestRebindWithTakeoverMirrorDoesNotReenterAppLock(t *testing.T) {
	app, tab, _, _, targetPath, loaded := newAtomicRebindTestApp(t)
	key := sessionRuntimeKey(targetPath)
	app.takeoverMu.Lock()
	if app.takeoverMirrors == nil {
		app.takeoverMirrors = map[string]*takeoverMirror{}
	}
	app.takeoverMirrors[key] = &takeoverMirror{app: app, key: key, sessionPath: targetPath}
	app.takeoverMu.Unlock()

	done := make(chan error, 1)
	go func() { done <- app.rebindTabToLoadedSessionPath(tab, targetPath, loaded) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("rebind deadlocked while reconnecting takeover mirror")
	}
}

func TestOwnershipProbeFailurePreservesSpectatorPin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	app := NewApp()
	app.remoteTabs = map[string]*remoteTab{}
	tab := &remoteTab{
		id: "remote-1", gen: 4, client: srv.Client(), selectionRevision: 9,
		routing: remoteTabSessionRouting{currentPath: "/sessions/a.jsonl"},
		session: remoteTabSessionState{takenOver: true},
	}
	app.remoteTabs[tab.id] = tab
	app.markRemoteTabSpectatorIfLocalOwned(context.Background(), tab.id, tab.client, srv.URL, tab.gen)
	if !tab.session.takenOver {
		t.Fatal("failed ownership probe cleared the spectator pin")
	}
}

func TestLateOwnershipProbeCannotChangeNewSelection(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(SessionTakeoverView{Holder: "external", Mirrored: true})
	}))
	defer srv.Close()
	app := NewApp()
	app.remoteTabs = map[string]*remoteTab{}
	tab := &remoteTab{
		id: "remote-1", gen: 4, client: srv.Client(), selectionRevision: 9,
		routing: remoteTabSessionRouting{currentPath: "/sessions/old.jsonl"},
	}
	app.remoteTabs[tab.id] = tab
	done := make(chan struct{})
	go func() {
		app.markRemoteTabSpectatorIfLocalOwned(context.Background(), tab.id, tab.client, srv.URL, tab.gen)
		close(done)
	}()
	<-started
	app.remoteTabMu.Lock()
	tab.selectionRevision++
	tab.routing.currentPath = "/sessions/new.jsonl"
	app.remoteTabMu.Unlock()
	close(release)
	<-done
	if tab.session.takenOver {
		t.Fatal("late ownership probe marked the newer selection read-only")
	}
}

func TestLateReclaimSuccessCannotUnlockNewSelection(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/reclaim" {
			close(started)
			<-release
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "not available", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	app := NewApp()
	app.remoteTabs = map[string]*remoteTab{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", gen: 4, client: srv.Client(), base: srv.URL, selectionRevision: 9,
		routing: remoteTabSessionRouting{currentPath: "/sessions/old.jsonl"},
		session: remoteTabSessionState{takenOver: true},
	}
	app.remoteTabs[tab.id] = tab
	done := make(chan error, 1)
	go func() { done <- app.ReclaimRemoteTabSession(tab.id) }()
	<-started
	app.remoteTabMu.Lock()
	tab.selectionRevision++
	tab.routing.currentPath = "/sessions/new.jsonl"
	tab.session.takenOver = true
	app.remoteTabMu.Unlock()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !tab.session.takenOver {
		t.Fatal("late reclaim response unlocked the newer selection")
	}
}

func TestFailedReclaimKeepsSpectatorUntilOwnershipProbeCompletes(t *testing.T) {
	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reclaim":
			http.Error(w, "mirror generation changed", http.StatusConflict)
		case "/ownership":
			close(probeStarted)
			<-probeRelease
			_ = json.NewEncoder(w).Encode(SessionTakeoverView{Holder: "external", Mirrored: true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	app := NewApp()
	app.remoteTabs = map[string]*remoteTab{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", gen: 4, client: srv.Client(), base: srv.URL, selectionRevision: 9,
		routing: remoteTabSessionRouting{currentPath: "/sessions/a.jsonl"},
		session: remoteTabSessionState{takenOver: true},
	}
	app.remoteTabs[tab.id] = tab
	if err := app.ReclaimRemoteTabSession(tab.id); err == nil {
		t.Fatal("failed reclaim unexpectedly succeeded")
	}
	<-probeStarted
	if !tab.session.takenOver {
		t.Fatal("ambiguous reclaim failure unlocked input before ownership proof")
	}
	close(probeRelease)
	app.remoteTabTasks.Wait()
	if !tab.session.takenOver {
		t.Fatal("external owner probe cleared spectator state")
	}
}
