package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemoteTabAllSessionEventsRouteOnlyCurrentFrames(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "current", Path: "/sessions/current.jsonl", Current: true},
		{Name: "background", Path: "/sessions/background.jsonl", Running: true},
	})
	fs.mu.Lock()
	fs.eventFrames = []string{
		`{"kind":"turn_started","sessionPath":"/sessions/background.jsonl"}`,
		`{"kind":"turn_started","sessionPath":"/sessions/current.jsonl"}`,
		`{"kind":"ready","sessionPath":"/sessions/current.jsonl"}`,
	}
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "current", SessionPath: "/sessions/current.jsonl", SessionTitle: "Current"})
	events := log.recorded()
	for _, event := range events {
		if strings.HasPrefix(event, "remote-tab:"+meta.ID+":event") && strings.Contains(event, "/sessions/background.jsonl") {
			t.Fatalf("background frame leaked to foreground reducer: %v", events)
		}
	}
	if !slices.ContainsFunc(events, func(event string) bool {
		return strings.HasPrefix(event, "remote-tab:"+meta.ID+":event") && strings.Contains(event, "/sessions/current.jsonl")
	}) {
		t.Fatalf("current-session frame was not forwarded: %v", events)
	}
	a.remoteTabMu.Lock()
	backgroundRunning := a.remoteTabs[meta.ID].routing.running["/sessions/background.jsonl"]
	a.remoteTabMu.Unlock()
	if !backgroundRunning {
		t.Fatal("background running state was not retained for the project tree")
	}
	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(sessions, func(session RemoteSessionView) bool {
		return session.Path == "/sessions/background.jsonl" && session.Running
	}) {
		t.Fatalf("background session is not marked running: %+v", sessions)
	}
	fs.mu.Lock()
	fs.sessions[1].Running = false
	fs.mu.Unlock()
	sessions, err = a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(sessions, func(session RemoteSessionView) bool {
		return session.Path == "/sessions/background.jsonl" && session.Running
	}) {
		t.Fatalf("authoritative idle listing did not clear stale running state: %+v", sessions)
	}
}

func TestBackgroundCompletionNoticeRefreshesRemoteRows(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const backgroundPath = "/sessions/background.jsonl"
	log := &eventLog{}
	a := &App{remoteEventHook: log.add, remoteTabs: map[string]*remoteTab{
		"remote-1": {
			id: "remote-1", gen: 7,
			routing: remoteTabSessionRouting{
				currentPath: currentPath,
				running:     map[string]bool{backgroundPath: true},
			},
		},
	}}
	if a.routeRemoteTabFrame("remote-1", 7, backgroundPath, "notice") {
		t.Fatal("background completion notice was routed to the foreground")
	}
	if got := log.count("remote-tab:updated"); got != 1 {
		t.Fatalf("background completion emitted %d row refreshes, want 1", got)
	}
}

func TestRecoveredBackgroundTerminalFrameRefreshesRemoteRows(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const originalPath = "/sessions/background.jsonl"
	const recoveredPath = "/sessions/background-recovered.jsonl"
	log := &eventLog{}
	a := &App{remoteEventHook: log.add, remoteTabs: map[string]*remoteTab{
		"remote-1": {
			id: "remote-1", gen: 7,
			routing: remoteTabSessionRouting{
				currentPath: currentPath,
				running:     map[string]bool{originalPath: true},
			},
		},
	}}
	if a.routeRemoteTabFrame("remote-1", 7, recoveredPath, "turn_done") {
		t.Fatal("recovered background terminal frame was routed to the foreground")
	}
	if got := log.count("remote-tab:updated"); got != 1 {
		t.Fatalf("recovered terminal frame emitted %d row refreshes, want 1", got)
	}
}

func TestFocusOnlyAttachRoutesImmediatePendingPrompt(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: currentPath, Current: true}})
	fs.mu.Lock()
	fs.eventFrames = []string{
		`{"kind":"approval_request","sessionPath":"/sessions/current.jsonl","approval":{"id":"approval-1"}}`,
		`{"kind":"ready","sessionPath":"/sessions/current.jsonl"}`,
	}
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{})
	events := log.recorded()
	if !slices.ContainsFunc(events, func(event string) bool {
		return strings.HasPrefix(event, "remote-tab:"+meta.ID+":event") && strings.Contains(event, `"approval-1"`)
	}) {
		t.Fatalf("focus-only attach dropped the immediate pending prompt: %v", events)
	}
	a.remoteTabMu.Lock()
	gotPath := a.remoteTabs[meta.ID].routing.currentPath
	a.remoteTabMu.Unlock()
	if gotPath != currentPath {
		t.Fatalf("focus-only path = %q, want %q", gotPath, currentPath)
	}
}

func TestNamedAttachPublishesResolvedRouteBeforeResume(t *testing.T) {
	const targetPath = "/sessions/target.jsonl"
	feed := make(chan string, 1)
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "target", Path: targetPath}})
	fs.mu.Lock()
	fs.eventFeed = feed
	fs.resumeStarted = make(chan string, 1)
	fs.resumeRelease = make(chan struct{})
	started, release := fs.resumeStarted, fs.resumeRelease
	fs.mu.Unlock()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "target"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("named resume request did not start")
	}
	feed <- `{"kind":"approval_request","sessionPath":"/sessions/target.jsonl","approval":{"id":"approval-target"}}`
	deadline := time.Now().Add(time.Second)
	for !slices.ContainsFunc(log.recorded(), func(event string) bool {
		return strings.HasPrefix(event, "remote-tab:"+meta.ID+":event") && strings.Contains(event, "approval-target")
	}) {
		if time.Now().After(deadline) {
			t.Fatalf("named target prompt was dropped while /resume was pending: %v", log.recorded())
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	waitForTabState(t, a, meta.ID, "ready")
}

func TestForegroundRecoveryPathIsReconciledBeforeRoutingFrame(t *testing.T) {
	const oldPath = "/sessions/current.jsonl"
	const recoveryPath = "/sessions/current-recovery.jsonl"
	feed := make(chan string, 1)
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: oldPath, Current: true}})
	fs.mu.Lock()
	fs.eventFrames = []string{`{"kind":"ready","sessionPath":"/sessions/current.jsonl"}`}
	fs.eventFeed = feed
	fs.statusPayload = `{"running":false,"sessionName":"current","sessionPath":"/sessions/current.jsonl"}`
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{})
	fs.mu.Lock()
	fs.statusPayload = `{"running":false,"sessionName":"current-recovery","sessionPath":"/sessions/current-recovery.jsonl"}`
	fs.mu.Unlock()
	feed <- `{"kind":"notice","text":"continued on recovery","sessionPath":"/sessions/current-recovery.jsonl"}`
	deadline := time.Now().Add(2 * time.Second)
	for {
		events := log.recorded()
		if slices.ContainsFunc(events, func(event string) bool {
			return strings.HasPrefix(event, "remote-tab:"+meta.ID+":event") && strings.Contains(event, "continued on recovery")
		}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery frame was not routed after status reconciliation: %v", events)
		}
		time.Sleep(time.Millisecond)
	}
	a.remoteTabMu.Lock()
	got := a.remoteTabs[meta.ID].routing.currentPath
	a.remoteTabMu.Unlock()
	if got != recoveryPath {
		t.Fatalf("foreground route = %q, want recovered path %q", got, recoveryPath)
	}
}

func TestUnknownBackgroundPathReconcilesStatusOnlyOnce(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const backgroundPath = "/sessions/background.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: currentPath, Current: true}})
	fs.mu.Lock()
	fs.statusPayload = `{"running":false,"sessionName":"current","sessionPath":"/sessions/current.jsonl"}`
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{})
	a.remoteTabMu.Lock()
	gen := a.remoteTabs[meta.ID].gen
	a.remoteTabMu.Unlock()
	countStatusRequests := func() int {
		count := 0
		for _, call := range fs.recorded() {
			if strings.HasPrefix(call, "GET /status") {
				count++
			}
		}
		return count
	}
	before := countStatusRequests()
	beforeRefreshes := log.count("remote-tab:updated")
	if a.routeRemoteTabFrameReconciled(meta.ID, gen, backgroundPath, "notice") {
		t.Fatal("unknown background frame was routed to the foreground")
	}
	if a.routeRemoteTabFrameReconciled(meta.ID, gen, backgroundPath, "turn_done") {
		t.Fatal("known background frame was routed to the foreground")
	}
	after := countStatusRequests()
	if after-before != 1 {
		t.Fatalf("unknown background path triggered %d status requests, want 1", after-before)
	}
	if got := log.count("remote-tab:updated") - beforeRefreshes; got != 2 {
		t.Fatalf("unknown background notice and terminal frame emitted %d row refreshes, want 2", got)
	}
}

func TestKnownBackgroundPathAdoptsFrameForegroundMarker(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const resumedPath = "/sessions/background.jsonl"
	log := &eventLog{}
	a := &App{remoteEventHook: log.add, remoteTabs: map[string]*remoteTab{
		"remote-1": {
			id: "remote-1", gen: 7,
			session: remoteTabSessionState{path: currentPath},
			routing: remoteTabSessionRouting{
				currentPath: currentPath,
				running:     map[string]bool{resumedPath: true},
			},
		},
	}}
	a.adoptRemoteTabFrameCurrent("remote-1", 7, resumedPath, false)
	if !a.routeRemoteTabFrameReconciled("remote-1", 7, resumedPath, "text") {
		t.Fatal("publication-time foreground marker did not reclassify a cached background path")
	}
	a.remoteTabMu.Lock()
	path := a.remoteTabs["remote-1"].routing.currentPath
	revision := a.remoteTabs["remote-1"].routing.revision
	a.remoteTabMu.Unlock()
	if path != resumedPath || revision != 1 {
		t.Fatalf("adopted route = %q revision %d, want %q revision 1", path, revision, resumedPath)
	}
	if got := log.count("remote-tab:updated"); got != 1 {
		t.Fatalf("foreground adoption emitted %d row refreshes, want 1", got)
	}
}

func TestForegroundMarkerPublishesRehydrateBeforeForwardedFrame(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const resumedPath = "/sessions/resumed.jsonl"
	log := &eventLog{}
	a := &App{remoteEventHook: log.add, remoteTabs: map[string]*remoteTab{
		"remote-1": {
			id: "remote-1", gen: 7, state: "ready",
			session: remoteTabSessionState{path: currentPath},
			routing: remoteTabSessionRouting{currentPath: currentPath, running: map[string]bool{}},
		},
	}}
	if !a.routeRemoteTabWireFrame("remote-1", 7, resumedPath, "text", true, false) {
		t.Fatal("new foreground frame was not routed")
	}
	a.emitRemoteEvent("remote-tab:remote-1:event", map[string]any{"kind": "text", "sessionPath": resumedPath})
	events := log.recorded()
	stateIndex, frameIndex := -1, -1
	for i, got := range events {
		if strings.HasPrefix(got, "remote-tab:remote-1:state ") {
			stateIndex = i
		}
		if strings.HasPrefix(got, "remote-tab:remote-1:event ") {
			frameIndex = i
		}
	}
	if stateIndex < 0 || frameIndex < 0 || stateIndex >= frameIndex {
		t.Fatalf("session rehydrate was not published before the frame: %v", events)
	}
}

func TestKnownBackgroundPromptReconcilesLegacyServeStatus(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const resumedPath = "/sessions/background.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: currentPath, Current: true}})
	fs.mu.Lock()
	fs.statusPayload = `{"running":true,"sessionName":"background","sessionPath":"/sessions/background.jsonl"}`
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionPath: currentPath})
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	tab.routing.running[resumedPath] = true
	gen := tab.gen
	a.remoteTabMu.Unlock()
	if !a.routeRemoteTabFrameReconciled(meta.ID, gen, resumedPath, "approval_request") {
		t.Fatal("legacy foreground prompt from a cached background path was discarded")
	}
	a.remoteTabMu.Lock()
	got := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if got != resumedPath {
		t.Fatalf("legacy status reconciliation route = %q, want %q", got, resumedPath)
	}
}

func TestProvisionalResumeRouteFencesStaleSessionListing(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const targetPath = "/sessions/target.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "current", Path: currentPath, Current: true},
		{Name: "target", Path: targetPath},
	})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionPath: currentPath})

	fs.mu.Lock()
	fs.sessionsStarted = make(chan struct{}, 1)
	fs.sessionsRelease = make(chan struct{})
	sessionsStarted, sessionsRelease := fs.sessionsStarted, fs.sessionsRelease
	fs.resumeStarted = make(chan string, 1)
	fs.resumeRelease = make(chan struct{})
	resumeStarted, resumeRelease := fs.resumeStarted, fs.resumeRelease
	fs.mu.Unlock()
	t.Cleanup(func() {
		for _, ch := range []chan struct{}{sessionsRelease, resumeRelease} {
			select {
			case <-ch:
			default:
				close(ch)
			}
		}
	})

	listingDone := make(chan error, 1)
	go func() {
		_, err := a.RemoteProjectSessions("box", "~/app")
		listingDone <- err
	}()
	select {
	case <-sessionsStarted:
	case <-time.After(time.Second):
		t.Fatal("session listing did not start")
	}
	resumeDone := make(chan struct{})
	go func() {
		a.resumeRemoteTabSessionPath(meta.ID, "target", targetPath, "Target")
		close(resumeDone)
	}()
	select {
	case <-resumeStarted:
	case <-time.After(time.Second):
		t.Fatal("resume request did not start")
	}
	close(sessionsRelease)
	if err := <-listingDone; err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	got := a.remoteTabs[meta.ID].routing.currentPath
	a.remoteTabMu.Unlock()
	if got != targetPath {
		t.Fatalf("stale /sessions response replaced provisional route with %q, want %q", got, targetPath)
	}
	close(resumeRelease)
	select {
	case <-resumeDone:
	case <-time.After(time.Second):
		t.Fatal("resume did not finish after release")
	}
}

func TestRemoteNewSessionFencesStaleSessionListing(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const rotatedPath = "/sessions/rotated.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: currentPath, Current: true}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionPath: currentPath})

	fs.mu.Lock()
	fs.sessionsStarted = make(chan struct{}, 1)
	fs.sessionsRelease = make(chan struct{})
	fs.newSessionPath = rotatedPath
	started, release := fs.sessionsStarted, fs.sessionsRelease
	fs.mu.Unlock()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	listingDone := make(chan error, 1)
	go func() {
		_, err := a.RemoteProjectSessions("box", "~/app")
		listingDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("session listing did not start")
	}
	if err := a.resetRemoteTabSession(meta.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-listingDone; err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	path := a.remoteTabs[meta.ID].routing.currentPath
	a.remoteTabMu.Unlock()
	if path != rotatedPath {
		t.Fatalf("stale /sessions response replaced rotated route with %q, want %q", path, rotatedPath)
	}
}

func TestFailedProvisionalResumeRestoresRouteWithNewRevision(t *testing.T) {
	const currentPath = "/sessions/current.jsonl"
	const targetPath = "/sessions/target.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: currentPath, Current: true}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionPath: currentPath})
	a.remoteTabMu.Lock()
	before := a.remoteTabs[meta.ID].routing.revision
	a.remoteTabMu.Unlock()
	fs.mu.Lock()
	fs.failEnter = "resume rejected"
	fs.mu.Unlock()
	a.resumeRemoteTabSessionPath(meta.ID, "target", targetPath, "Target")
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	path, revision := tab.routing.currentPath, tab.routing.revision
	a.remoteTabMu.Unlock()
	if path != currentPath || revision != before+2 {
		t.Fatalf("failed resume route = %q revision %d, want %q revision %d", path, revision, currentPath, before+2)
	}
}

func TestRemoteProjectSessionsAdoptsAuthoritativeCurrentRoute(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "a", Path: "/a.jsonl"}, {Name: "b", Path: "/b.jsonl", Current: true}})
	seedBridgeTestHost(t, "box")
	client, _ := remoteSessionTestClient(t, fs)
	log := &eventLog{}
	tab := &remoteTab{
		id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready",
		client: client, base: fs.server.URL, gen: 7,
		session:       remoteTabSessionState{name: "a", path: "/a.jsonl"},
		routing:       remoteTabSessionRouting{currentPath: "/a.jsonl", running: map[string]bool{}},
		pendingEvents: map[string]json.RawMessage{"approval_request:1": json.RawMessage(`{"kind":"approval_request"}`)},
		runtime:       remoteTabRuntimeState{pendingPrompt: true},
	}
	a := &App{remoteEventHook: log.add, remoteTabs: map[string]*remoteTab{tab.id: tab}}
	readyPrefix := "remote-tab:" + tab.id + ":state"
	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	current := slices.DeleteFunc(append([]RemoteSessionView(nil), sessions...), func(session RemoteSessionView) bool { return !session.Current })
	if len(current) != 1 || current[0].Path != "/b.jsonl" {
		t.Fatalf("current rows = %+v, want only authoritative session b", current)
	}
	a.remoteTabMu.Lock()
	name, path, route := tab.session.name, tab.session.path, tab.routing.currentPath
	pendingEvents, pendingPrompt := len(tab.pendingEvents), tab.runtime.pendingPrompt
	a.remoteTabMu.Unlock()
	if name != "b" || path != "/b.jsonl" || route != "/b.jsonl" {
		t.Fatalf("adopted identity = %q/%q/%q, want b//b.jsonl//b.jsonl", name, path, route)
	}
	if pendingEvents != 0 || pendingPrompt {
		t.Fatalf("listing adoption retained stale prompts: events=%d pending=%v", pendingEvents, pendingPrompt)
	}
	if got := log.count(readyPrefix); got != 1 {
		t.Fatalf("listing adoption emitted %d ready barriers, want 1", got)
	}
}

func TestExternalSessionResetPreservesBlankIdentity(t *testing.T) {
	const oldPath = "/sessions/old.jsonl"
	const freshPath = "/sessions/fresh.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "old", Path: oldPath}})
	seedBridgeTestHost(t, "box")
	client, _ := remoteSessionTestClient(t, fs)
	log := &eventLog{}
	tab := &remoteTab{
		id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready",
		client: client, base: fs.server.URL, gen: 7,
		topicTitle: "Old title",
		session:    remoteTabSessionState{name: "old", path: oldPath},
		routing:    remoteTabSessionRouting{currentPath: oldPath, running: map[string]bool{}},
		pendingEvents: map[string]json.RawMessage{
			"approval_request:old": json.RawMessage(`{"kind":"approval_request"}`),
		},
		runtime: remoteTabRuntimeState{running: true, pendingPrompt: true, cancellable: true},
	}
	a := &App{remoteEventHook: log.add, remoteTabs: map[string]*remoteTab{tab.id: tab}}
	if !a.routeRemoteTabWireFrame(tab.id, tab.gen, freshPath, "session_changed", true, true) {
		t.Fatal("fresh external session barrier was not routed")
	}
	a.remoteTabMu.Lock()
	name, path, route, title := tab.session.name, tab.session.path, tab.routing.currentPath, tab.topicTitle
	reset, newSession := tab.session.reset, tab.session.newSession
	pending := len(tab.pendingEvents)
	running, pendingPrompt := tab.runtime.running, tab.runtime.pendingPrompt
	a.remoteTabMu.Unlock()
	if name != "" || path != freshPath || route != freshPath || title != a.localizedDefaultTopicTitle() || !reset || !newSession {
		t.Fatalf("external reset identity = %q/%q/%q/%q reset=%v new=%v", name, path, route, title, reset, newSession)
	}
	if pending != 0 || running || pendingPrompt {
		t.Fatalf("external reset retained old runtime: pending=%d running=%v prompt=%v", pending, running, pendingPrompt)
	}
	if got := log.count("remote-tab:" + tab.id + ":state"); got != 1 {
		t.Fatalf("external reset emitted %d ready barriers, want 1", got)
	}
	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || !slices.ContainsFunc(sessions, func(session RemoteSessionView) bool {
		return session.Name == "" && session.Path == freshPath && session.Current
	}) {
		t.Fatalf("external blank session missing from listing: %+v", sessions)
	}
}

func remoteSessionTestClient(t *testing.T, fs *fakeServe) (*http.Client, context.Context) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	if err := serveHandshake(ctx, client, fs.server.URL, "s3cret"); err != nil {
		t.Fatal(err)
	}
	return client, ctx
}

func TestEnterRemoteSessionPathSkipsSessionCatalog(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	client, ctx := remoteSessionTestClient(t, fs)
	target, err := enterRemoteSessionTarget(ctx, client, fs.server.URL, RemoteTabOpenOptions{SessionName: "known", SessionPath: "/remote/sessions/known.jsonl", SessionTitle: "Known"})
	if err != nil {
		t.Fatal(err)
	}
	if target.Path != "/remote/sessions/known.jsonl" || target.Title != "Known" {
		t.Fatalf("target = %+v", target)
	}
	for _, call := range fs.recorded() {
		if strings.HasPrefix(call, "GET /sessions") {
			t.Fatalf("explicit path unnecessarily fetched the session catalog: %v", fs.recorded())
		}
	}
}

func TestEnterRemoteSessionUnknownName(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "s1", Path: "/x.jsonl"}})
	client, ctx := remoteSessionTestClient(t, fs)
	err := enterRemoteSession(ctx, client, fs.server.URL, RemoteTabOpenOptions{SessionName: "missing"})
	if err == nil || !strings.Contains(err.Error(), `"missing" not found`) {
		t.Fatalf("err = %v, want unknown session error", err)
	}
}
