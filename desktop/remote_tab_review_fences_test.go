package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewerTerminalStateOverridesStaleRunningListingRow(t *testing.T) {
	testTerminalListingRace(t, false, false)
}

func TestNewerTerminalStateRetainsBackgroundRunningListingRow(t *testing.T) {
	testTerminalListingRace(t, true, false)
}

func TestRecoveredTerminalPathRetriesStaleRunningListingRow(t *testing.T) {
	testTerminalListingRace(t, false, true)
}

func testTerminalListingRace(t *testing.T, serveStillRunning, recovered bool) {
	t.Helper()
	const path = "/sessions/current.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: path, Current: true, Running: true}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionPath: path})
	fs.mu.Lock()
	fs.sessionsStarted, fs.sessionsRelease = make(chan struct{}, 1), make(chan struct{})
	started, release := fs.sessionsStarted, fs.sessionsRelease
	fs.mu.Unlock()
	t.Cleanup(func() { closeTestSignal(release) })
	type result struct {
		sessions []RemoteSessionView
		err      error
	}
	done := make(chan result, 1)
	go func() {
		sessions, err := a.RemoteProjectSessions("box", "~/app")
		done <- result{sessions, err}
	}()
	waitTestSignal(t, started, "session listing did not start")
	a.remoteTabMu.Lock()
	gen := a.remoteTabs[meta.ID].gen
	a.remoteTabMu.Unlock()
	terminalPath := path
	if recovered {
		terminalPath = "/sessions/current-recovery.jsonl"
	}
	if routed := a.routeRemoteTabFrame(meta.ID, gen, terminalPath, "turn_done"); routed != !recovered {
		t.Fatalf("terminal route result = %v, recovered=%v", routed, recovered)
	}
	fs.mu.Lock()
	fs.sessions[0] = serveSessionEntry{Name: "current", Path: terminalPath, Current: true, Running: serveStillRunning}
	fs.mu.Unlock()
	closeTestSignal(release)
	var got result
	select {
	case got = <-done:
	case <-time.After(time.Second):
		t.Fatal("session listing did not finish")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	for _, session := range got.sessions {
		if session.Path == terminalPath && session.Running == serveStillRunning {
			return
		}
	}
	t.Fatalf("running row did not reconcile to %v: %+v", serveStillRunning, got.sessions)
}

func TestAttachRemoteTabServeRejectsResponseAfterNewerAdoption(t *testing.T) {
	const responsePath, newerPath = "/sessions/response.jsonl", "/sessions/newer.jsonl"
	feed := make(chan string, 1)
	fs := newFakeServe(t, "s3cret", nil)
	fs.mu.Lock()
	fs.eventFeed, fs.newSessionPath = feed, responsePath
	fs.newStarted, fs.newRelease = make(chan struct{}, 1), make(chan struct{})
	started, release := fs.newStarted, fs.newRelease
	fs.mu.Unlock()
	t.Cleanup(func() { closeTestSignal(release) })
	tab := &remoteTab{id: "remote-1", state: "connecting", routing: remoteTabSessionRouting{running: map[string]bool{}}}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		entered bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		entered, err := a.attachRemoteTabServe(ctx, tab.id, fs.server.URL, "s3cret", "serve-1", RemoteTabOpenOptions{NewSession: true})
		done <- result{entered, err}
	}()
	waitTestSignal(t, started, "new-session attach did not start")
	feed <- `{"kind":"session_changed","sessionPath":"/sessions/newer.jsonl","sessionCurrent":true}`
	waitRemoteTestPath(t, a, tab, newerPath)
	closeTestSignal(release)
	select {
	case got := <-done:
		if got.err != nil || got.entered {
			t.Fatalf("rejected attach result = entered %v, err %v", got.entered, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("new-session attach did not finish")
	}
	waitRemoteTestPath(t, a, tab, newerPath)
}

func TestRemoteModelResponseCannotLabelNewerForegroundSession(t *testing.T) {
	testRemoteModelRouteFence(t, false)
}

func TestRemoteModelResponseLabelsExpectedSessionAfterABARouteChange(t *testing.T) {
	testRemoteModelRouteFence(t, true)
}

func testRemoteModelRouteFence(t *testing.T, returnToExpected bool) {
	t.Helper()
	isolateDesktopUserDirs(t)
	const oldPath, newerPath = "/sessions/old.jsonl", "/sessions/newer.jsonl"
	started, release := make(chan struct{}), make(chan struct{})
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.Header.Get(expectedSessionPathHeader)
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: server.Client(), base: server.URL, gen: 7, model: "old-model",
		routing: remoteTabSessionRouting{currentPath: oldPath, pathRevision: 2, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	done := make(chan error, 1)
	go func() { done <- a.SetRemoteTabModel(tab.id, "next-model") }()
	waitTestSignal(t, started, "model request did not start")
	a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, newerPath, true)
	if returnToExpected {
		a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, oldPath, true)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	model, path := tab.model, tab.routing.currentPath
	a.remoteTabMu.Unlock()
	wantPath, wantModel := newerPath, "old-model"
	if returnToExpected {
		wantPath, wantModel = oldPath, "next-model"
	}
	if requestPath != oldPath || path != wantPath || model != wantModel {
		t.Fatalf("request/model/path = %q/%q/%q, want %q/%q/%q", requestPath, model, path, oldPath, wantModel, wantPath)
	}
}

func closeTestSignal(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func waitTestSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func waitRemoteTestPath(t *testing.T, a *App, tab *remoteTab, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		a.remoteTabMu.Lock()
		path := tab.routing.currentPath
		a.remoteTabMu.Unlock()
		if path == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("remote path = %q, want %q", path, want)
		}
		time.Sleep(time.Millisecond)
	}
}
