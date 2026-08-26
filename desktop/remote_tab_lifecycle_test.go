package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemoteTabSnapshotReplaysAndClearsPendingPrompt(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	a.remoteTabMu.Lock()
	gen := a.remoteTabs[meta.ID].gen
	a.remoteTabMu.Unlock()
	a.cacheRemotePendingEvent(meta.ID, gen, "approval_request", json.RawMessage(`{"kind":"approval_request","approval":{"id":"approval-1","tool":"bash"}}`))
	snap, err := a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PendingEvents) != 1 || !strings.Contains(string(snap.PendingEvents[0]), "approval-1") {
		t.Fatalf("pending replay = %s", snap.PendingEvents)
	}
	if err := a.ApproveRemoteTab(meta.ID, "approval-1", "deny"); err != nil {
		t.Fatal(err)
	}
	snap, err = a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PendingEvents) != 0 {
		t.Fatalf("resolved prompt was still replayed: %s", snap.PendingEvents)
	}

	form := json.RawMessage(`{"kind":"extension_surface","extension":{"pluginId":"remote-plugin","surfaceId":"setup","kind":"form","form":{"title":"Remote setup","fields":[{"key":"region","label":"Region","kind":"input"}]}}}`)
	if !a.cacheRemotePendingExtensionForm(meta.ID, gen, form) {
		t.Fatal("actionable extension form was not retained")
	}
	snap, err = a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PendingEvents) != 1 || !strings.Contains(string(snap.PendingEvents[0]), "remote-plugin") {
		t.Fatalf("pending extension form replay = %s", snap.PendingEvents)
	}
	fs.mu.Lock()
	fs.failNext = "form rejected"
	fs.mu.Unlock()
	if err := a.SubmitRemoteTabExtensionForm(meta.ID, "remote-plugin", "setup", map[string]any{"region": "us-west"}); err == nil {
		t.Fatal("failed extension form submission succeeded")
	}
	snap, err = a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PendingEvents) != 1 {
		t.Fatalf("failed extension form submission cleared replay: %s", snap.PendingEvents)
	}
	if err := a.SubmitRemoteTabExtensionForm(meta.ID, "remote-plugin", "setup", map[string]any{"region": "us-west"}); err != nil {
		t.Fatal(err)
	}
	snap, err = a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PendingEvents) != 0 {
		t.Fatalf("submitted extension form was still replayed: %s", snap.PendingEvents)
	}
}

func TestRemoteTabDoesNotPublishReadyWithoutEventStream(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	fs.mu.Lock()
	fs.eventsStatus = http.StatusServiceUnavailable
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "error")
	time.Sleep(50 * time.Millisecond)
	a.remoteTabMu.Lock()
	state := a.remoteTabs[meta.ID].state
	a.remoteTabMu.Unlock()
	if state == "ready" {
		t.Fatal("tab published ready after /events failed")
	}
}

func TestRemoteTabDoesNotPublishReadyWhenEventStreamClosesDuringAttach(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	fs.mu.Lock()
	fs.eventsCloseEarly = true
	fs.enterDelay = 100 * time.Millisecond
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "serve_down")
	for _, event := range log.recorded() {
		if strings.HasPrefix(event, "remote-tab:"+meta.ID+":state ") && strings.Contains(event, `"state":"ready"`) {
			t.Fatalf("closed event stream published ready: %v", log.recorded())
		}
	}
}

func TestRemoteTabReviveAppliesRequestedNamedSession(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	if tab.cancel != nil {
		tab.cancel()
	}
	tab.gen++
	tab.cancel, tab.client, tab.base, tab.token = nil, nil, "", ""
	tab.state = "disconnected"
	tab.session = remoteTabSessionState{newSession: true}
	a.remoteTabMu.Unlock()
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "saved"}); err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	_, resumed, _ := fs.snapshot()
	if resumed != "/saved.jsonl" {
		t.Fatalf("revived shell resumed %q, want the selected session", resumed)
	}
}

func TestRemoteTabServeDownCanRetry(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "error", Error: "temporary"}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "serve_down")
	kernel.ensureView = RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true}); err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
}

func TestRemoteTabFocusOnlyAttachPreservesCurrentServeSession(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "current", Path: "/current.jsonl", Title: "Current", Current: true}})
	kernel := &fakeRemoteKernel{statuses: []RemoteConnectionStatusView{{HostID: "box", State: "connected"}}, ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret"}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	newCalled, resumed, _ := fs.snapshot()
	if newCalled != 0 || resumed != "" {
		t.Fatalf("focus-only attach changed Serve session: new=%d resume=%q", newCalled, resumed)
	}
}

func TestRemoteSavedSessionLookupCannotReviveDisconnectedGeneration(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	fs.sessionsStarted = make(chan struct{}, 1)
	fs.sessionsRelease = make(chan struct{})
	kernel := &fakeRemoteKernel{statuses: []RemoteConnectionStatusView{{HostID: "box", State: "connected"}}, ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret"}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	done := make(chan struct{})
	go func() { a.resumeRemoteTabSession(meta.ID, "saved"); close(done) }()
	select {
	case <-fs.sessionsStarted:
	case <-time.After(time.Second):
		t.Fatal("saved-session lookup did not start")
	}
	a.suspendRemoteTabPumps("box", "reconnecting", "")
	close(fs.sessionsRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("saved-session lookup did not finish")
	}
	a.remoteTabMu.Lock()
	state := a.remoteTabs[meta.ID].state
	a.remoteTabMu.Unlock()
	if state != "reconnecting" {
		t.Fatalf("stale session lookup changed state to %q", state)
	}
}

func TestRemoteTabReplacementServeReentersLearnedSessionBeforeReady(t *testing.T) {
	oldServe := newFakeServe(t, "s3cret", nil)
	newServe := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "generated", Path: "/generated.jsonl", Title: "Generated"}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: oldServe.server.URL, InstanceID: "serve-old"}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	oldServe.mu.Lock()
	oldServe.statusPayload = `{"running":false,"sessionName":"generated"}`
	oldServe.mu.Unlock()
	if _, err := a.RemoteTabStatus(meta.ID); err != nil {
		t.Fatal(err)
	}

	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	if tab.cancel != nil {
		tab.cancel()
	}
	tab.gen++
	tab.cancel, tab.client, tab.base, tab.token = nil, nil, "", ""
	tab.state = "reconnecting"
	a.remoteTabMu.Unlock()
	kernel.ensureView = RemoteServerView{HostID: "box", State: "ready", LocalURL: newServe.server.URL, InstanceID: "serve-new"}

	if !a.reattachRemoteTabOnce(meta.ID) {
		t.Fatal("replacement serve reattach failed")
	}
	_, resumed, _ := newServe.snapshot()
	if resumed != "/generated.jsonl" {
		t.Fatalf("replacement serve resumed %q, want /generated.jsonl", resumed)
	}
	a.remoteTabMu.Lock()
	state, instanceID := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].session.instanceID
	a.remoteTabMu.Unlock()
	if state != "ready" || instanceID != "serve-new" {
		t.Fatalf("reattached state/instance = %q/%q", state, instanceID)
	}
}

func TestRemoteTabServeDownRetryPreservesNamedSession(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "saved"})
	a.parkRemoteTabsForServer("box", "~/app", "serve_down", "stopped")
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{}); err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")
	_, resumed, _ := fs.snapshot()
	if resumed != "/saved.jsonl" {
		t.Fatalf("retry resumed %q, want the parked named session", resumed)
	}
}

func TestRemoteResumeBusyKeepsCurrentSessionReady(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	fs.mu.Lock()
	fs.failEnter = "cannot resume while a turn is running"
	fs.mu.Unlock()
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "saved"}); err != nil {
		t.Fatal(err)
	}
	a.remoteTabMu.Lock()
	state, message := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].err
	a.remoteTabMu.Unlock()
	if state != "ready" || !strings.Contains(message, "Finish the current turn") {
		t.Fatalf("busy resume state/error = %q/%q, want ready non-terminal notice", state, message)
	}
}

func TestRemoteResumeRejectedKeepsCurrentSessionReady(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	fs.mu.Lock()
	fs.failEnter = "session is already leased by another process"
	fs.mu.Unlock()
	a.resumeRemoteTabSession(meta.ID, "saved")
	a.remoteTabMu.Lock()
	state, message := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].err
	a.remoteTabMu.Unlock()
	if state != "ready" || !strings.Contains(message, "already leased") {
		t.Fatalf("rejected resume state/error = %q/%q, want ready action error", state, message)
	}
}

func TestRemoteNewBusyKeepsCurrentSessionReady(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved", Current: true}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "saved"})
	a.remoteTabMu.Lock()
	previousTitle := a.remoteTabs[meta.ID].topicTitle
	a.remoteTabMu.Unlock()
	fs.mu.Lock()
	fs.failEnter = "cannot start a new session while a turn is running"
	fs.mu.Unlock()
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true}); err == nil || !strings.Contains(err.Error(), "while a turn is running") {
		t.Fatalf("busy new-session error = %v", err)
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	state, message, title, client := tab.state, tab.err, tab.topicTitle, tab.client
	a.remoteTabMu.Unlock()
	if state != "ready" || message != "" || title != previousTitle || client == nil {
		t.Fatalf("busy new-session state/error/title/client = %q/%q/%q/%v, want ready current attachment", state, message, title, client)
	}
}

func TestRemoteResumeLeaseConflictFailsAttach(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	fs.mu.Lock()
	fs.failEnter = "this session is in use by another Reasonix window or process"
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "saved"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "error")
	a.remoteTabMu.Lock()
	state, message, client := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].err, a.remoteTabs[meta.ID].client
	a.remoteTabMu.Unlock()
	if state != "error" || !strings.Contains(message, "session is in use") || client != nil {
		t.Fatalf("lease-conflict attach state/error/client = %q/%q/%v", state, message, client)
	}
}

func TestRemoteSnapshotRejectsChangedGeneration(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	fs.mu.Lock()
	fs.historyStarted = make(chan struct{}, 1)
	fs.historyRelease = make(chan struct{})
	started, release := fs.historyStarted, fs.historyRelease
	fs.mu.Unlock()
	errCh := make(chan error, 1)
	go func() {
		_, err := a.RemoteTabSnapshot(meta.ID)
		errCh <- err
	}()
	<-started
	a.suspendRemoteTabPumps("box", "reconnecting", "")
	close(release)
	if err := <-errCh; err == nil || !strings.Contains(err.Error(), "changed while loading snapshot") {
		t.Fatalf("snapshot error = %v, want generation fence", err)
	}
}

func TestRemoteStopAndCloseCancelsBeforeRemovingTab(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	fs.mu.Lock()
	fs.statusPayload = `{"running":true,"pendingPrompt":false,"backgroundJobs":1,"cancellable":true,"jobs":[{"id":"job-remote","kind":"task","label":"verify","status":"running","startedAt":1}]}`
	fs.statusAfterCancel = `{"running":false,"pendingPrompt":false,"backgroundJobs":0,"cancellable":false}`
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedClassicBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	work := a.ActiveWorkForTab(meta.ID)
	if !work.Running || !work.Cancellable {
		t.Fatalf("remote active work = %+v", work)
	}
	if err := a.CloseTabWithPolicy(meta.ID, "stop_and_close"); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(fs.recorded(), func(call string) bool { return strings.HasPrefix(call, "POST /cancel") }) {
		t.Fatalf("stop-and-close did not cancel remote work: %v", fs.recorded())
	}
	if !slices.Contains(fs.recorded(), `POST /jobs/cancel {"ids":["job-remote"]}`) {
		t.Fatalf("stop-and-close did not cancel remote background jobs: %v", fs.recorded())
	}
	a.remoteTabMu.Lock()
	_, present := a.remoteTabs[meta.ID]
	a.remoteTabMu.Unlock()
	if present {
		t.Fatal("remote tab remained after work became idle")
	}
}

func TestRemoteResumeListFailureKeepsCurrentAttachmentReady(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "saved", Path: "/saved.jsonl", Title: "Saved"}})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	fs.mu.Lock()
	fs.failSessions = true
	fs.mu.Unlock()
	a.resumeRemoteTabSession(meta.ID, "saved")
	a.remoteTabMu.Lock()
	state, message := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].err
	a.remoteTabMu.Unlock()
	if state != "ready" || !strings.Contains(message, "Could not open remote session") {
		t.Fatalf("list failure state/error = %q/%q, want ready non-terminal notice", state, message)
	}
}
