package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRemoteTabReconnectDefersCachedSessionSelectionUntilReady(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "old", Path: oldPath, Title: "Old", Current: true},
		{Name: "target", Path: targetPath, Title: "Target"},
	})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "old", SessionPath: oldPath})

	started := make(chan string, 2)
	fs.mu.Lock()
	fs.resumeStarted = started
	fs.mu.Unlock()
	a.remoteTabsHostStatus("box", "reconnecting", "")
	waitForTabState(t, a, meta.ID, "reconnecting")
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{
		SessionName: "target", SessionPath: targetPath, SessionTitle: "Target",
	}); err != nil {
		t.Fatal(err)
	}

	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	state, sessionPath, route := tab.state, tab.session.path, tab.routing.currentPath
	pending := tab.pendingSelection
	a.remoteTabMu.Unlock()
	if state != "reconnecting" || sessionPath != oldPath || route != oldPath || pending == nil || pending.path != targetPath {
		t.Fatalf("deferred state/session/route/pending = %q/%q/%q/%+v", state, sessionPath, route, pending)
	}
	select {
	case path := <-started:
		t.Fatalf("selection resumed %q before the tab was ready", path)
	default:
	}

	a.remoteTabsHostStatus("box", "connected", "")
	select {
	case path := <-started:
		if path != targetPath {
			t.Fatalf("ready resume path = %q, want %q", path, targetPath)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deferred selection was not resumed after reconnect")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.remoteTabMu.Lock()
		state, sessionPath, route = tab.state, tab.session.path, tab.routing.currentPath
		pending = tab.pendingSelection
		a.remoteTabMu.Unlock()
		if state == "ready" && sessionPath == targetPath && route == targetPath && pending == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("final state/session/route/pending = %q/%q/%q/%+v", state, sessionPath, route, pending)
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case path := <-started:
		t.Fatalf("deferred selection resumed more than once; extra path %q", path)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRemoteTabDeferredResumeRejectsSupersededSelectionRevision(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("")), Request: req,
		}, nil
	})}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, base: "http://127.0.0.1:43210", gen: 7,
		selectionRevision: 2,
		routing:           remoteTabSessionRouting{currentPath: "/sessions/newer.jsonl", running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.resumeRemoteTabSessionPathForSelection(tab.id, "stale", "/sessions/stale.jsonl", "Stale", 1)
	if requests != 0 {
		t.Fatalf("superseded deferred selection sent %d Serve requests", requests)
	}
}

func TestReadyTabRapidSelectionsRollbackToServeAuthoritativeSnapshot(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const firstPath = "/sessions/first.jsonl"
	const secondPath = "/sessions/second.jsonl"
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusConflict, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("busy")), Request: req,
		}, nil
	})}
	ref := RemoteTabRef{HostID: "box", Workspace: "~/app"}
	tab := &remoteTab{
		id: "remote-1", ref: ref, state: "ready", client: client, base: "http://127.0.0.1:43210", gen: 7,
		session: remoteTabSessionState{name: "old", path: oldPath}, topicTitle: "Old",
		routing: remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}

	firstOpts := RemoteTabOpenOptions{SessionName: "first", SessionPath: firstPath, SessionTitle: "First"}
	first := a.registerRemoteTabOpen(&remoteTab{id: "unused-first", ref: ref}, "Box", firstOpts)
	if !a.commitRemoteTabOpenRegistration(&first, "Box", firstOpts) || tab.pendingSelection == nil {
		t.Fatal("first ready selection was not retained until its resume started")
	}
	secondOpts := RemoteTabOpenOptions{SessionName: "second", SessionPath: secondPath, SessionTitle: "Second"}
	second := a.registerRemoteTabOpen(&remoteTab{id: "unused-second", ref: ref}, "Box", secondOpts)
	if !a.commitRemoteTabOpenRegistration(&second, "Box", secondOpts) {
		t.Fatal("second ready selection was not committed")
	}
	if second.previousSelection == nil || second.previousSelection.currentPath != oldPath {
		t.Fatalf("second rollback snapshot = %+v, want Serve-authoritative %q", second.previousSelection, oldPath)
	}

	if handled := a.resumeRemoteTabSessionPathForOpenSelection(tab.id, "first", firstPath, "First", first.selection.revision, first.previousSelection); !handled {
		t.Fatal("superseded first selection requested rollback")
	}
	if requests != 0 {
		t.Fatalf("superseded first selection sent %d Serve requests", requests)
	}
	if handled := a.resumeRemoteTabSessionPathForOpenSelection(tab.id, "second", secondPath, "Second", second.selection.revision, second.previousSelection); handled {
		t.Fatal("rejected second selection was treated as committed")
	}
	a.restoreRejectedRemoteTabOpenSelection(tab.id, second.previousSelection)
	if requests != 1 || tab.routing.currentPath != oldPath || tab.session.path != oldPath || tab.topicTitle != "Old" {
		t.Fatalf("rejected rapid selection left requests/route/session/title = %d/%q/%q/%q", requests, tab.routing.currentPath, tab.session.path, tab.topicTitle)
	}
}

func TestReadyTabInFlightSelectionsSerializeThroughDefinitiveRollback(t *testing.T) {
	isolateDesktopUserDirs(t)
	const oldPath = "/sessions/old.jsonl"
	const firstPath = "/sessions/first.jsonl"
	const secondPath = "/sessions/second.jsonl"
	firstStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var payload struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(req.Body).Decode(&payload)
		switch payload.Path {
		case firstPath:
			firstStarted <- struct{}{}
			<-releaseFirst
		case secondPath:
			secondStarted <- struct{}{}
		default:
			t.Fatalf("unexpected resume path %q", payload.Path)
		}
		return &http.Response{
			StatusCode: http.StatusConflict, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("busy")), Request: req,
		}, nil
	})}
	ref := RemoteTabRef{HostID: "box", Workspace: "~/app"}
	tab := &remoteTab{
		id: "remote-1", ref: ref, state: "ready", client: client, base: "http://127.0.0.1:43210", gen: 7,
		session: remoteTabSessionState{name: "old", path: oldPath}, topicTitle: "Old",
		routing: remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}

	firstOpts := RemoteTabOpenOptions{SessionName: "first", SessionPath: firstPath, SessionTitle: "First"}
	first := a.registerRemoteTabOpen(&remoteTab{id: "unused-first", ref: ref}, "Box", firstOpts)
	if !a.commitRemoteTabOpenRegistration(&first, "Box", firstOpts) {
		t.Fatal("first ready selection was not committed")
	}
	a.resumeRemoteTabOpenAsync(tab.id, "first", firstPath, "First", first.previousSelection)
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first resume did not start")
	}

	secondOpts := RemoteTabOpenOptions{SessionName: "second", SessionPath: secondPath, SessionTitle: "Second"}
	second := a.registerRemoteTabOpen(&remoteTab{id: "unused-second", ref: ref}, "Box", secondOpts)
	if !a.commitRemoteTabOpenRegistration(&second, "Box", secondOpts) {
		t.Fatal("second selection was not queued")
	}
	a.remoteTabMu.Lock()
	queued, sessionPath := tab.pendingSelection, tab.session.path
	a.remoteTabMu.Unlock()
	if queued != second.selection || !queued.deferred || sessionPath != firstPath {
		t.Fatalf("in-flight selection queue/session = %+v/%q, want deferred second/%q", queued, sessionPath, firstPath)
	}
	select {
	case <-secondStarted:
		t.Fatal("second resume started before the first resolved")
	default:
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("queued second resume did not start after first rollback")
	}
	if second.selection.previous == nil || second.selection.previous.currentPath != oldPath {
		t.Fatalf("second rollback snapshot = %+v, want authoritative %q", second.selection.previous, oldPath)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		a.remoteTabMu.Lock()
		path, sessionPath, title, pending := tab.routing.currentPath, tab.session.path, tab.topicTitle, tab.pendingSelection
		a.remoteTabMu.Unlock()
		if path == oldPath && sessionPath == oldPath && title == "Old" && pending == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rejected in-flight selections left route/session/title/pending = %q/%q/%q/%+v", path, sessionPath, title, pending)
		}
		time.Sleep(time.Millisecond)
	}
	a.remoteTabTasks.Wait()
}

func TestRemoteTabResumeRequeuesSelectionWhenReadinessDrops(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	started := make(chan string, 1)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost && req.URL.Path == "/resume" {
			started <- targetPath
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"running":false}`)), Request: req}, nil
	})}
	previous := &remoteTabOpenSelection{
		session: remoteTabSessionState{name: "old", path: oldPath}, topicTitle: "Old",
		currentPath: oldPath, revision: 1,
	}
	tab := &remoteTab{
		id: "remote-1", state: "reconnecting", gen: 7, selectionRevision: 1,
		topicTitle: "Target", session: remoteTabSessionState{name: "target", path: targetPath},
		routing: remoteTabSessionRouting{currentPath: targetPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	if deferred := a.resumeRemoteTabSessionPathForOpenSelection(tab.id, "target", targetPath, "Target", 1, previous); !deferred {
		t.Fatal("readiness loss did not preserve the committed selection")
	}
	if tab.pendingSelection == nil || !tab.pendingSelection.identityCommitted || tab.pendingSelection.previous != previous {
		t.Fatalf("queued selection = %+v, want committed selection with original rollback", tab.pendingSelection)
	}
	select {
	case <-started:
		t.Fatal("resume ran while the tab was reconnecting")
	default:
	}

	a.remoteTabMu.Lock()
	tab.state, tab.client, tab.base = "ready", client, "http://127.0.0.1:43210"
	a.remoteTabMu.Unlock()
	a.applyPendingRemoteTabOpenSelection(tab.id)
	select {
	case path := <-started:
		if path != targetPath {
			t.Fatalf("resumed path = %q, want %q", path, targetPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued selection was not resumed after readiness returned")
	}
}

func TestRemoteTabReadinessLossPreservesNewerQueuedSelection(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const firstPath = "/sessions/first.jsonl"
	const secondPath = "/sessions/second.jsonl"
	ref := RemoteTabRef{HostID: "box", Workspace: "~/app"}
	previous := &remoteTabOpenSelection{
		session: remoteTabSessionState{name: "old", path: oldPath}, topicTitle: "Old",
		currentPath: oldPath, revision: 1,
	}
	firstPending := &remoteTabPendingOpenSelection{
		name: "first", path: firstPath, title: "First", revision: 1,
		identityCommitted: true, previous: previous,
	}
	tab := &remoteTab{
		id: "remote-1", ref: ref, state: "ready", selectionRevision: 1,
		session: remoteTabSessionState{name: "first", path: firstPath}, topicTitle: "First",
		routing:          remoteTabSessionRouting{currentPath: firstPath, running: map[string]bool{}},
		pendingSelection: firstPending,
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}

	tab.sessionMu.Lock()
	a.resumeRemoteTabOpenAsync(tab.id, "first", firstPath, "First", previous)
	deadline := time.Now().Add(2 * time.Second)
	for tab.selectionMu.TryLock() {
		tab.selectionMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("first selection did not acquire the lifecycle lock")
		}
		time.Sleep(time.Millisecond)
	}
	secondOpts := RemoteTabOpenOptions{SessionName: "second", SessionPath: secondPath, SessionTitle: "Second"}
	second := a.registerRemoteTabOpen(&remoteTab{id: "unused-second", ref: ref}, "Box", secondOpts)
	if !a.commitRemoteTabOpenRegistration(&second, "Box", secondOpts) {
		t.Fatal("newer selection was not queued")
	}
	a.remoteTabMu.Lock()
	tab.state = "reconnecting"
	a.remoteTabMu.Unlock()
	tab.sessionMu.Unlock()
	a.remoteTabTasks.Wait()

	a.remoteTabMu.Lock()
	pending := tab.pendingSelection
	a.remoteTabMu.Unlock()
	if pending != second.selection || !pending.deferred || pending.path != secondPath || pending.revision != 0 {
		t.Fatalf("readiness loss replaced newer selection: %+v", pending)
	}
}

func TestRemoteTabNewSessionSupersedesDeferredCachedSelection(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const freshPath = "/sessions/fresh.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "old", Path: oldPath, Title: "Old", Current: true},
		{Name: "target", Path: targetPath, Title: "Target"},
	})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "old", SessionPath: oldPath})

	resumeStarted := make(chan string, 1)
	newStarted := make(chan struct{}, 1)
	fs.mu.Lock()
	fs.resumeStarted, fs.newStarted, fs.newSessionPath = resumeStarted, newStarted, freshPath
	fs.mu.Unlock()
	a.remoteTabsHostStatus("box", "reconnecting", "")
	waitForTabState(t, a, meta.ID, "reconnecting")
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "target", SessionPath: targetPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true}); err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	pending := tab.pendingSelection
	a.remoteTabMu.Unlock()
	if pending == nil || !pending.newSession || pending.path != "" {
		t.Fatalf("pending selection = %+v, want newest New Session intent", pending)
	}

	a.remoteTabsHostStatus("box", "connected", "")
	select {
	case <-newStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("deferred New Session did not rotate after reconnect")
	}
	select {
	case path := <-resumeStarted:
		t.Fatalf("superseded cached session resumed %q", path)
	default:
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.remoteTabMu.Lock()
		state, path, reset, currentPending := tab.state, tab.routing.currentPath, tab.session.reset, tab.pendingSelection
		a.remoteTabMu.Unlock()
		if state == "ready" && path == freshPath && reset && currentPending == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("final state/path/reset/pending = %q/%q/%v/%+v", state, path, reset, currentPending)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRemoteTabNewSessionRestoresRouteFromCommittedDeferredSelection(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const freshPath = "/sessions/fresh.jsonl"
	ref := RemoteTabRef{HostID: "box", Workspace: "~/app"}
	previous := &remoteTabOpenSelection{
		session: remoteTabSessionState{name: "old", path: oldPath}, topicTitle: "Old",
		currentPath: oldPath, revision: 1,
	}
	tab := &remoteTab{
		id: "remote-1", ref: ref, state: "reconnecting", selectionRevision: 1,
		session: remoteTabSessionState{name: "target", path: targetPath}, topicTitle: "Target",
		routing: remoteTabSessionRouting{currentPath: targetPath, running: map[string]bool{}},
		pendingSelection: &remoteTabPendingOpenSelection{
			name: "target", path: targetPath, title: "Target", revision: 1,
			deferred: true, identityCommitted: true, previous: previous,
		},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	opts := RemoteTabOpenOptions{NewSession: true}
	registration := a.registerRemoteTabOpen(&remoteTab{id: "unused", ref: ref}, "Box", opts)
	if !a.commitRemoteTabOpenRegistration(&registration, "Box", opts) {
		t.Fatal("New Session did not reuse the reconnecting tab")
	}
	if tab.session.path != oldPath || tab.routing.currentPath != oldPath || tab.pendingSelection == nil || !tab.pendingSelection.newSession {
		t.Fatalf("supersession left session/route/pending = %q/%q/%+v", tab.session.path, tab.routing.currentPath, tab.pendingSelection)
	}

	expectedPath := make(chan string, 1)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		expectedPath <- req.Header.Get(expectedSessionPathHeader)
		header := make(http.Header)
		header.Set("X-Reasonix-Session-Path", freshPath)
		return &http.Response{StatusCode: http.StatusNoContent, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	a.remoteTabMu.Lock()
	tab.state, tab.client, tab.base = "ready", client, "http://127.0.0.1:43210"
	a.remoteTabMu.Unlock()
	a.applyPendingRemoteTabOpenSelection(tab.id)
	select {
	case got := <-expectedPath:
		if got != oldPath {
			t.Fatalf("/new expected-session path = %q, want authoritative %q", got, oldPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deferred New Session did not reach Serve")
	}
}

func TestRemoteTabDeferredNewSessionRechecksCurrentBlankState(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const freshPath = "/sessions/fresh.jsonl"
	requested := make(chan struct{}, 1)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/new" {
			t.Fatalf("request = %s %s, want POST /new", req.Method, req.URL.Path)
		}
		requested <- struct{}{}
		header := make(http.Header)
		header.Set("X-Reasonix-Session-Path", freshPath)
		return &http.Response{StatusCode: http.StatusNoContent, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, base: "http://127.0.0.1:43210",
		session:           remoteTabSessionState{name: "old", path: oldPath, reset: false},
		routing:           remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
		selectionRevision: 1,
		pendingSelection: &remoteTabPendingOpenSelection{
			newSession: true, reuseBlank: true, revision: 1, deferred: true,
		},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.applyPendingRemoteTabOpenSelection(tab.id)

	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred New Session was discarded using the stale blank snapshot")
	}
	a.remoteTabMu.Lock()
	path, reset := tab.routing.currentPath, tab.session.reset
	a.remoteTabMu.Unlock()
	if path != freshPath || !reset {
		t.Fatalf("rotated state path/reset = %q/%v, want %q/true", path, reset, freshPath)
	}
}
