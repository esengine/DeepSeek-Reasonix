package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestSetActiveTabRepublishesTerminalRemoteState(t *testing.T) {
	log := &eventLog{}
	tab := &remoteTab{
		id: "remote-terminal", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"},
		state: "serve_down", err: "bootstrap failed",
		routing: remoteTabSessionRouting{running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteEventHook: log.add}
	if err := a.SetActiveTab(tab.id); err != nil {
		t.Fatal(err)
	}
	events := strings.Join(log.recorded(), "\n")
	if !strings.Contains(events, `remote-tab:remote-terminal:state {"state":"serve_down","error":"bootstrap failed"}`) {
		t.Fatalf("terminal activation events = %s", events)
	}
}

func TestRemoteTabServeDownSavedSessionClearsPendingBeforeDelayedMarker(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const savedPath = "/sessions/saved.jsonl"
	feed := make(chan string, 1)
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "old", Path: oldPath, Title: "Old", Current: true},
		{Name: "saved", Path: savedPath, Title: "Saved"},
	})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "old", SessionPath: oldPath})

	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	if tab.cancel != nil {
		tab.cancel()
	}
	tab.gen++
	tab.cancel, tab.client, tab.base, tab.token = nil, nil, "", ""
	tab.state = "serve_down"
	tab.pendingEvents = map[string]json.RawMessage{
		"approval_request:old": json.RawMessage(`{"kind":"approval_request","approval":{"id":"old"}}`),
	}
	tab.runtime = remoteTabRuntimeState{pendingPrompt: true, cancellable: true}
	a.remoteTabMu.Unlock()
	fs.mu.Lock()
	fs.eventFeed = feed
	fs.mu.Unlock()

	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "saved", SessionPath: savedPath, SessionTitle: "Saved"}); err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	a.remoteTabMu.Lock()
	path, pending, prompt := tab.routing.currentPath, len(tab.pendingEvents), tab.runtime.pendingPrompt
	a.remoteTabMu.Unlock()
	if path != savedPath || pending != 0 || prompt {
		t.Fatalf("saved attach route/pending/prompt = %q/%d/%v, want %q/0/false", path, pending, prompt, savedPath)
	}

	eventPrefix := "remote-tab:" + meta.ID + ":event"
	before := log.count(eventPrefix)
	feed <- `{"kind":"session_changed","sessionPath":"/sessions/saved.jsonl","sessionCurrent":true}`
	waitForRemoteEventCount(t, log, eventPrefix, before+1)
	a.remoteTabMu.Lock()
	pending, prompt = len(tab.pendingEvents), tab.runtime.pendingPrompt
	a.remoteTabMu.Unlock()
	if pending != 0 || prompt {
		t.Fatalf("delayed saved-session marker restored stale prompt: pending=%d prompt=%v", pending, prompt)
	}
}

func TestExternalResumeRefreshesAdoptedSessionTitle(t *testing.T) {
	const firstPath = "/sessions/first.jsonl"
	const nextPath = "/sessions/next.jsonl"
	feed := make(chan string, 1)
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "first", Path: firstPath, Title: "First title", Current: true}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	fs.mu.Lock()
	fs.eventFeed = feed
	fs.mu.Unlock()
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "first", SessionPath: firstPath})
	fs.mu.Lock()
	fs.sessions = []serveSessionEntry{{Name: "next", Path: nextPath, Title: "Next title", Current: true}}
	fs.mu.Unlock()

	eventPrefix := "remote-tab:" + meta.ID + ":event"
	before := log.count(eventPrefix)
	feed <- `{"kind":"session_changed","sessionPath":"/sessions/next.jsonl","sessionCurrent":true}`
	waitForRemoteEventCount(t, log, eventPrefix, before+1)
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.remoteTabMu.Lock()
		title, path := a.remoteTabs[meta.ID].topicTitle, a.remoteTabs[meta.ID].routing.currentPath
		a.remoteTabMu.Unlock()
		if title == "Next title" && path == nextPath {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("externally adopted title/path = %q/%q, want Next title/%q", title, path, nextPath)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRemoteAttachRetiringSessionConflictKeepsCurrentReady(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "current", Path: currentPath, Title: "Current title", Current: true},
		{Name: "retiring", Path: "/sessions/retiring.jsonl", Title: "Retiring title"},
	})
	fs.mu.Lock()
	fs.failEnter = "session is finishing background teardown; retry shortly"
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "retiring", SessionPath: "/sessions/retiring.jsonl"})
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	state, path, title := tab.state, tab.routing.currentPath, tab.topicTitle
	a.remoteTabMu.Unlock()
	if state != "ready" || path != currentPath || title != "Current title" {
		t.Fatalf("soft attach state/path/title = %q/%q/%q, want ready/%q/Current title", state, path, title, currentPath)
	}
}

func TestRemoteResumeTransportFailureReconcilesCommittedTarget(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	seedBridgeTestHost(t, "box")
	postCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/resume":
			postCalls++
			// Model a response lost after Serve has already committed targetPath.
			return nil, io.ErrUnexpectedEOF
		case req.Method == http.MethodGet && req.URL.Path == "/sessions":
			body := `[{"name":"target","path":"/sessions/target.jsonl","title":"Target","current":true}]`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		default:
			return nil, errors.New("unexpected request")
		}
	})}
	tab := &remoteTab{
		id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready",
		client: client, base: "http://127.0.0.1:43210", gen: 7,
		topicTitle: "Old",
		session:    remoteTabSessionState{name: "old", path: oldPath},
		routing:    remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.resumeRemoteTabSessionPath(tab.id, "target", targetPath, "Target")
	a.remoteTabMu.Lock()
	state, path, sessionPath, title := tab.state, tab.routing.currentPath, tab.session.path, tab.topicTitle
	rehydrating := tab.routing.rehydratingPath
	a.remoteTabMu.Unlock()
	if postCalls != 1 || state != "ready" || path != targetPath || sessionPath != targetPath || title != "Target" || rehydrating != "" {
		t.Fatalf("reconciled resume calls/state/route/session/title/rehydrating = %d/%q/%q/%q/%q/%q", postCalls, state, path, sessionPath, title, rehydrating)
	}
}

func TestRemoteResumeReplayStopsAfterLaterSessionAdoption(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const laterPath = "/sessions/later.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{name: "target", path: targetPath},
		routing: remoteTabSessionRouting{
			currentPath: targetPath, rehydratingPath: targetPath, running: map[string]bool{},
			rehydratingFrames: []json.RawMessage{
				json.RawMessage(`{"kind":"text","text":"target-first","sessionPath":"/sessions/target.jsonl"}`),
				json.RawMessage(`{"kind":"text","text":"target-second","sessionPath":"/sessions/target.jsonl"}`),
			},
		},
	}
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	firstFrameEntered := make(chan struct{})
	releaseFirstFrame := make(chan struct{})
	var firstFrameOnce sync.Once
	a.remoteEventHook = func(name string, payload any) {
		log.add(name, payload)
		if name == "remote-tab:"+tab.id+":event" {
			data, _ := json.Marshal(payload)
			if strings.Contains(string(data), "target-first") {
				firstFrameOnce.Do(func() { close(firstFrameEntered) })
				<-releaseFirstFrame
			}
		}
	}
	route := remoteTabProvisionalResume{targetPath: targetPath, previousPath: oldPath, active: true}
	resumeDone := make(chan struct{})
	go func() {
		a.publishRemoteTabResumeReady(tab.id, tab, client, tab.gen, route)
		close(resumeDone)
	}()
	select {
	case <-firstFrameEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first replay frame did not reach publication")
	}
	adoptDone := make(chan struct{})
	go func() {
		a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, laterPath, true)
		close(adoptDone)
	}()
	select {
	case <-adoptDone:
		t.Fatal("later route adoption overtook an in-flight frame publication")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirstFrame)
	select {
	case <-adoptDone:
	case <-time.After(2 * time.Second):
		t.Fatal("later route adoption did not complete after publication")
	}
	select {
	case <-resumeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("resume replay did not stop after later adoption")
	}
	events := strings.Join(log.recorded(), "\n")
	secondFrame := strings.Index(events, "target-second")
	laterReady := strings.LastIndex(events, "remote-tab:"+tab.id+":state")
	if !strings.Contains(events, "target-first") || secondFrame < 0 || laterReady < secondFrame {
		t.Fatalf("later adoption overtook the ordered replay: %s", events)
	}
	a.remoteTabMu.Lock()
	path, rehydrating := tab.routing.currentPath, tab.routing.rehydratingPath
	a.remoteTabMu.Unlock()
	if path != laterPath || rehydrating != "" {
		t.Fatalf("later route/rehydration = %q/%q, want %q/empty", path, rehydrating, laterPath)
	}
}

func TestRemoteResumeReplaysPromptArrivingDuringDrain(t *testing.T) {
	const targetPath = "/sessions/target.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		routing: remoteTabSessionRouting{
			currentPath: targetPath, rehydratingPath: targetPath, running: map[string]bool{},
			rehydratingFrames: []json.RawMessage{
				json.RawMessage(`{"kind":"text","text":"drain-start","sessionPath":"/sessions/target.jsonl"}`),
			},
		},
	}
	firstPublished := make(chan struct{})
	release := make(chan struct{})
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.remoteEventHook = func(name string, payload any) {
		log.add(name, payload)
		data, _ := json.Marshal(payload)
		if name == "remote-tab:"+tab.id+":event" && strings.Contains(string(data), "drain-start") {
			close(firstPublished)
			<-release
		}
	}
	done := make(chan struct{})
	go func() {
		a.publishRemoteTabResumeReady(tab.id, tab, client, tab.gen, remoteTabProvisionalResume{targetPath: targetPath, active: true})
		close(done)
	}()
	select {
	case <-firstPublished:
	case <-time.After(time.Second):
		t.Fatal("resume drain did not publish its first frame")
	}
	// The aggregate snapshot has already copied an empty prompt set. A prompt
	// arriving now must therefore reach the frontend through the fenced drain.
	a.remoteTabMu.Lock()
	snapshotPending := len(tab.pendingEvents)
	a.remoteTabMu.Unlock()
	if snapshotPending != 0 {
		t.Fatalf("snapshot pending events = %d, want 0", snapshotPending)
	}
	approval := json.RawMessage(`{"kind":"approval_request","approval":{"id":"during-drain"},"sessionPath":"/sessions/target.jsonl","sessionCurrent":true}`)
	if !a.bufferRemoteTabResumeFrame(tab.id, tab.gen, targetPath, "approval_request", approval) {
		t.Fatal("prompt arriving during drain was not buffered")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resume drain did not finish")
	}
	events := strings.Join(log.recorded(), "\n")
	if !strings.Contains(events, `"id":"during-drain"`) {
		t.Fatalf("prompt arriving after snapshot was not replayed: %s", events)
	}
	a.remoteTabMu.Lock()
	pending, rehydrating := len(tab.pendingEvents), tab.routing.rehydratingPath
	a.remoteTabMu.Unlock()
	if pending != 1 || rehydrating != "" {
		t.Fatalf("post-drain pending/rehydrating = %d/%q, want 1/empty", pending, rehydrating)
	}
}

func TestRemoteRotationResponseCannotOverwriteLaterRouteAdoption(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const responsePath = "/sessions/response.jsonl"
	const laterPath = "/sessions/later.jsonl"
	requestEntered := make(chan struct{})
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/new" {
			http.NotFound(w, r)
			return
		}
		close(requestEntered)
		<-releaseResponse
		w.Header().Set("X-Reasonix-Session-Path", responsePath)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: server.Client(), base: server.URL, gen: 7,
		session: remoteTabSessionState{name: "old", path: oldPath},
		routing: remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	done := make(chan error, 1)
	go func() { done <- a.rotateRemoteTabSession(tab.id, "/new") }()
	select {
	case <-requestEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("rotation request did not reach Serve")
	}
	a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, laterPath, true)
	close(releaseResponse)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rotation did not finish")
	}
	a.remoteTabMu.Lock()
	path, sessionPath := tab.routing.currentPath, tab.session.path
	a.remoteTabMu.Unlock()
	if path != laterPath || sessionPath == responsePath {
		t.Fatalf("stale rotation response replaced later route: routing/session = %q/%q, want later route %q", path, sessionPath, laterPath)
	}
}

func TestRemoteStatusAdoptionWaitsForFramePublication(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const laterPath = "/sessions/later.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: currentPath},
		routing: remoteTabSessionRouting{currentPath: currentPath, running: map[string]bool{}},
		runtime: remoteTabRuntimeState{revision: 11},
	}
	frameEntered := make(chan struct{})
	releaseFrame := make(chan struct{})
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.remoteEventHook = func(name string, payload any) {
		log.add(name, payload)
		if name == "remote-tab:"+tab.id+":event" {
			close(frameEntered)
			<-releaseFrame
		}
	}
	frameDone := make(chan bool, 1)
	go func() {
		frame := json.RawMessage(`{"kind":"text","text":"current-frame","sessionPath":"/sessions/current.jsonl"}`)
		frameDone <- a.publishRemoteTabFrame(tab.id, tab.gen, currentPath, "text", frame)
	}()
	select {
	case <-frameEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("current frame did not reach publication")
	}
	statusDone := make(chan bool, 1)
	go func() {
		statusDone <- a.recordRemoteTabSessionStatus(tab.id, client, tab.gen, 11, json.RawMessage(`{"sessionPath":"/sessions/later.jsonl"}`))
	}()
	select {
	case <-statusDone:
		t.Fatal("status route adoption overtook an in-flight frame publication")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFrame)
	if !<-frameDone || !<-statusDone {
		t.Fatal("ordered frame publication or status adoption was rejected")
	}
	events := strings.Join(log.recorded(), "\n")
	frameIndex := strings.Index(events, "current-frame")
	readyIndex := strings.LastIndex(events, "remote-tab:"+tab.id+":state")
	if frameIndex < 0 || readyIndex < frameIndex {
		t.Fatalf("status ready barrier overtook the current frame: %s", events)
	}
	a.remoteTabMu.Lock()
	adoptedPath := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if adoptedPath != laterPath {
		t.Fatalf("status route = %q, want %q", adoptedPath, laterPath)
	}
}

func TestRemoteRejectedResumeReconciliationWaitsForFramePublication(t *testing.T) {
	const previousPath = "/sessions/previous.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const authoritativePath = "/sessions/authoritative.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: targetPath},
		routing: remoteTabSessionRouting{
			currentPath: targetPath, rehydratingPath: targetPath, running: map[string]bool{},
		},
	}
	frameEntered := make(chan struct{})
	releaseFrame := make(chan struct{})
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.remoteEventHook = func(name string, payload any) {
		log.add(name, payload)
		if name == "remote-tab:"+tab.id+":event" {
			close(frameEntered)
			<-releaseFrame
		}
	}
	frameDone := make(chan bool, 1)
	go func() {
		frame := json.RawMessage(`{"kind":"text","text":"target-frame","sessionPath":"/sessions/target.jsonl"}`)
		frameDone <- a.publishRemoteTabFrameForRoute(tab.id, tab, client, tab.gen, targetPath, true, "text", frame)
	}()
	select {
	case <-frameEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("target frame did not reach publication")
	}
	reconcileDone := make(chan struct{})
	go func() {
		route := remoteTabProvisionalResume{targetPath: targetPath, previousPath: previousPath, active: true}
		a.reconcileRemoteTabRejectedResume(tab.id, tab, client, tab.gen, route, serveSessionEntry{Path: authoritativePath}, errors.New("resume response lost"))
		close(reconcileDone)
	}()
	select {
	case <-reconcileDone:
		t.Fatal("ambiguous-resume reconciliation overtook an in-flight frame")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFrame)
	if !<-frameDone {
		t.Fatal("target frame was rejected before authoritative reconciliation")
	}
	select {
	case <-reconcileDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ambiguous-resume reconciliation did not complete")
	}
	events := strings.Join(log.recorded(), "\n")
	frameIndex := strings.Index(events, "target-frame")
	readyIndex := strings.LastIndex(events, "remote-tab:"+tab.id+":state")
	if frameIndex < 0 || readyIndex < frameIndex {
		t.Fatalf("authoritative ready barrier overtook the target frame: %s", events)
	}
	a.remoteTabMu.Lock()
	currentPath := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if currentPath != authoritativePath {
		t.Fatalf("reconciled route = %q, want %q", currentPath, authoritativePath)
	}
}

func TestRemoteRejectedResumeReconciliationCannotOverwriteNewerAdoption(t *testing.T) {
	const previousPath = "/sessions/previous.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const staleAuthoritativePath = "/sessions/stale-authoritative.jsonl"
	const newerPath = "/sessions/newer.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: targetPath},
		routing: remoteTabSessionRouting{
			currentPath: targetPath, rehydratingPath: targetPath, running: map[string]bool{},
		},
	}
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteEventHook: log.add}
	// This route marker arrives after the reconciliation query returned C but
	// before its caller acquired the route/publication mutex.
	a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, newerPath, true)
	eventsBefore := len(log.recorded())
	route := remoteTabProvisionalResume{targetPath: targetPath, previousPath: previousPath, active: true}
	a.reconcileRemoteTabRejectedResume(tab.id, tab, client, tab.gen, route, serveSessionEntry{Path: staleAuthoritativePath}, errors.New("resume response lost"))
	a.remoteTabMu.Lock()
	currentPath := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if currentPath != newerPath {
		t.Fatalf("stale reconciliation replaced newer route with %q, want %q", currentPath, newerPath)
	}
	if eventsAfter := len(log.recorded()); eventsAfter != eventsBefore {
		t.Fatalf("stale reconciliation emitted %d events after newer adoption, want 0", eventsAfter-eventsBefore)
	}
}

func TestRemoteRejectedResumeReconcilesReselectedCurrentSession(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const authoritativePath = "/sessions/authoritative.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: currentPath},
		routing: remoteTabSessionRouting{currentPath: currentPath, pathRevision: 9, running: map[string]bool{}},
	}
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteEventHook: log.add}
	route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, currentPath)
	if route.active || route.pathRevision != 9 {
		t.Fatalf("reselection route = %+v, want inactive revision 9", route)
	}
	a.reconcileRemoteTabRejectedResume(tab.id, tab, client, tab.gen, route, serveSessionEntry{Path: authoritativePath}, errors.New("resume response lost"))
	a.remoteTabMu.Lock()
	got := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if got != authoritativePath {
		t.Fatalf("reselected current route = %q, want authoritative %q", got, authoritativePath)
	}
	if log.count("remote-tab:"+tab.id+":state") != 1 {
		t.Fatalf("authoritative reconciliation did not publish one ready barrier: %v", log.recorded())
	}
}

func TestRemoteRejectedResumePreservesProbedAuthoritativeSelection(t *testing.T) {
	isolateDesktopUserDirs(t)
	const previousPath = "/sessions/previous.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const authoritativePath = "/sessions/authoritative.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7, selectionRevision: 11,
		session: remoteTabSessionState{name: "previous", path: previousPath}, topicTitle: "Previous",
		routing: remoteTabSessionRouting{currentPath: previousPath, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	previous := &remoteTabOpenSelection{
		session: tab.session, topicTitle: tab.topicTitle, currentPath: previousPath, revision: tab.selectionRevision,
	}
	route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, targetPath)
	handled := a.reconcileRemoteTabRejectedResume(
		tab.id, tab, client, tab.gen, route,
		serveSessionEntry{Name: "authoritative", Path: authoritativePath, Title: "Authoritative"},
		errors.New("resume response lost"),
	)
	if !handled {
		a.restoreRejectedRemoteTabOpenSelection(tab.id, previous)
	}
	a.remoteTabMu.Lock()
	gotPath, gotSession, gotTitle := tab.routing.currentPath, tab.session.path, tab.topicTitle
	a.remoteTabMu.Unlock()
	if gotPath != authoritativePath || gotSession != authoritativePath || gotTitle != "Authoritative" {
		t.Fatalf("ambiguous resume restored stale selection: route/session/title = %q/%q/%q", gotPath, gotSession, gotTitle)
	}
}

func TestRemoteRejectedResumeRollbackCannotMarkNewerRouteErrored(t *testing.T) {
	const previousPath = "/sessions/previous.jsonl"
	const targetPath = "/sessions/target.jsonl"
	const newerPath = "/sessions/newer.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: previousPath},
		routing: remoteTabSessionRouting{currentPath: previousPath, running: map[string]bool{}},
	}
	log := &eventLog{}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteEventHook: log.add}
	route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, targetPath)
	a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, newerPath, true)
	eventsBefore := len(log.recorded())
	a.reconcileRemoteTabRejectedResume(tab.id, tab, client, tab.gen, route, serveSessionEntry{Path: previousPath}, errors.New("resume response lost"))
	a.remoteTabMu.Lock()
	got := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if got != newerPath {
		t.Fatalf("stale rollback replaced newer route with %q, want %q", got, newerPath)
	}
	if eventsAfter := len(log.recorded()); eventsAfter != eventsBefore {
		t.Fatalf("stale rollback emitted %d events after newer adoption, want 0", eventsAfter-eventsBefore)
	}
}

func TestRemoteReselectionSuccessCannotOverwriteNewerAdoption(t *testing.T) {
	const previousPath = "/sessions/previous.jsonl"
	const newerPath = "/sessions/newer.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: previousPath},
		routing: remoteTabSessionRouting{currentPath: previousPath, pathRevision: 3, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	route := a.beginRemoteTabProvisionalResume(tab.id, tab, client, tab.gen, previousPath)
	a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, newerPath, true)
	if _, committed := a.commitRemoteTabResume(tab.id, tab, client, tab.gen, route, serveSessionEntry{Path: previousPath}, "Previous"); committed {
		t.Fatal("late reselection success overwrote a newer route")
	}
	a.remoteTabMu.Lock()
	got := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if got != newerPath {
		t.Fatalf("late reselection changed route to %q, want %q", got, newerPath)
	}
}

func TestRemoteResumeCommitPublicationBlocksNewerAdoption(t *testing.T) {
	isolateDesktopUserDirs(t)
	const targetPath = "/sessions/target.jsonl"
	const newerPath = "/sessions/newer.jsonl"
	client := &http.Client{}
	tab := &remoteTab{
		id: "remote-1", state: "ready", client: client, gen: 7,
		session: remoteTabSessionState{path: targetPath},
		routing: remoteTabSessionRouting{
			currentPath: targetPath, rehydratingPath: targetPath, pathRevision: 4, running: map[string]bool{},
		},
	}
	metadataEntered := make(chan struct{})
	releaseMetadata := make(chan struct{})
	var once sync.Once
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	a.remoteEventHook = func(name string, _ any) {
		if name == "remote-tab:updated" {
			once.Do(func() {
				close(metadataEntered)
				<-releaseMetadata
			})
		}
	}
	route := remoteTabProvisionalResume{targetPath: targetPath, active: true}
	resumeDone := make(chan bool, 1)
	go func() {
		resumeDone <- a.commitAndPublishRemoteTabResume(tab.id, tab, client, tab.gen, route, serveSessionEntry{Path: targetPath}, "Target")
	}()
	select {
	case <-metadataEntered:
	case <-time.After(time.Second):
		t.Fatal("resume metadata publication did not start")
	}
	adoptDone := make(chan struct{})
	go func() {
		a.adoptRemoteTabFrameCurrent(tab.id, tab.gen, newerPath, true)
		close(adoptDone)
	}()
	select {
	case <-adoptDone:
		t.Fatal("newer adoption overtook the in-flight resume publication")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseMetadata)
	select {
	case committed := <-resumeDone:
		if !committed {
			t.Fatal("resume commit was unexpectedly rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("resume publication did not finish")
	}
	select {
	case <-adoptDone:
	case <-time.After(time.Second):
		t.Fatal("newer adoption remained blocked after resume publication")
	}
	a.remoteTabMu.Lock()
	path := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if path != newerPath {
		t.Fatalf("foreground route = %q, want newer adoption %q", path, newerPath)
	}
}

func TestRemoteAttachResponseCannotOverwriteNewerAdoption(t *testing.T) {
	const responsePath = "/sessions/response.jsonl"
	const newerPath = "/sessions/newer.jsonl"
	tab := &remoteTab{
		id: "remote-1", gen: 7, topicTitle: "Newer",
		session: remoteTabSessionState{name: "newer", path: newerPath},
		routing: remoteTabSessionRouting{currentPath: newerPath, pathRevision: 5, running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}}
	committed := a.commitRemoteTabAttachResponse(tab.id, tab, tab.gen, 4, serveSessionEntry{
		Name: "response", Path: responsePath, Title: "Stale response",
	}, false)
	if committed {
		t.Fatal("stale attach response overwrote a newer route adoption")
	}
	a.remoteTabMu.Lock()
	path, name, title := tab.routing.currentPath, tab.session.name, tab.topicTitle
	a.remoteTabMu.Unlock()
	if path != newerPath || name != "newer" || title != "Newer" {
		t.Fatalf("stale attach response changed newer identity: path=%q name=%q title=%q", path, name, title)
	}
}

func TestExternalSessionAdoptionResetsAndSeedsForegroundRuntime(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	tab := &remoteTab{
		routing: remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{targetPath: true}},
		session: remoteTabSessionState{name: "old", path: oldPath},
		pendingEvents: map[string]json.RawMessage{
			"approval_request:old": json.RawMessage(`{"kind":"approval_request"}`),
		},
		runtime: remoteTabRuntimeState{
			running: true, turnStartedAt: 99, backgroundJobs: 3,
			pendingPrompt: true, cancelRequested: true, cancellable: true,
		},
	}
	if !adoptRemoteTabSessionPathLocked(tab, targetPath) {
		t.Fatal("target session was not adopted")
	}
	if len(tab.pendingEvents) != 0 || !tab.runtime.running || tab.runtime.turnStartedAt != 0 ||
		tab.runtime.backgroundJobs != 0 || tab.runtime.pendingPrompt || tab.runtime.cancelRequested || !tab.runtime.cancellable {
		t.Fatalf("adopted runtime retained old controller state: %+v pending=%d", tab.runtime, len(tab.pendingEvents))
	}
}

func TestRevivedSessionSelectionWaitsForVisibilityCommit(t *testing.T) {
	seedBridgeTestHost(t, "box")
	if err := editUserConfig(func(c *config.Config) error { return c.SetDesktopLayoutStyle("workbench") }); err != nil {
		t.Fatal(err)
	}
	const oldPath = "/sessions/old.jsonl"
	const targetPath = "/sessions/target.jsonl"
	remote := &remoteTab{
		id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "disconnected",
		topicTitle: "Old title",
		session:    remoteTabSessionState{name: "old", path: oldPath},
		routing:    remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
	}
	a := &App{
		tabs: map[string]*WorkspaceTab{
			"local-1": {ID: "local-1", Ctrl: &snapshotErrorSessionController{err: errors.New("snapshot failed")}},
		},
		remoteTabs: map[string]*remoteTab{remote.id: remote},
	}
	a.remoteTabLayout.activeID = remote.id
	a.remoteTabLayout.order = []string{remote.id}
	_, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{
		SessionName: "target", SessionPath: targetPath, SessionTitle: "Target title",
	})
	if err == nil || !strings.Contains(err.Error(), "snapshot failed") {
		t.Fatalf("revived open error = %v, want visibility snapshot failure", err)
	}
	a.remoteTabMu.Lock()
	state, name, sessionPath := remote.state, remote.session.name, remote.session.path
	route, title := remote.routing.currentPath, remote.topicTitle
	a.remoteTabMu.Unlock()
	if state != "disconnected" || name != "old" || sessionPath != oldPath || route != oldPath || title != "Old title" {
		t.Fatalf("failed visibility commit changed revived shell: state=%q name=%q session=%q route=%q title=%q", state, name, sessionPath, route, title)
	}
}
