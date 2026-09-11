package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"reasonix/internal/config"
)

// OpenRemoteProjectTab adopts the clicked identity immediately; the Serve
// resume round trip runs in the background.

func TestOpenRemoteProjectTabResumeReturnsBeforeServeRoundTrip(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First", Turns: 1, Current: true},
		{Name: "s2", Path: "/remote/sessions/s2.jsonl", Title: "Second", Turns: 1},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "s1", SessionPath: "/remote/sessions/s1.jsonl"})

	started, release := make(chan string, 1), make(chan struct{})
	fs.mu.Lock()
	fs.resumeStarted, fs.resumeRelease = started, release
	fs.mu.Unlock()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	type openResult struct {
		meta TabMeta
		err  error
	}
	result := make(chan openResult, 1)
	go func() {
		meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{
			SessionName: "s2", SessionPath: "/remote/sessions/s2.jsonl", SessionTitle: "Second",
		})
		result <- openResult{meta: meta, err: err}
	}()
	var switched TabMeta
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatal(got.err)
		}
		switched = got.meta
	case <-time.After(5 * time.Second):
		t.Fatal("open blocked on the held resume round trip")
	}
	if want := "box\x00~/app\x00s2"; switched.TopicID != want {
		t.Fatalf("returned meta TopicID = %q, want the adopted s2 identity %q", switched.TopicID, want)
	}
	select {
	case path := <-started:
		if path != "/remote/sessions/s2.jsonl" {
			t.Fatalf("resume path = %q, want s2", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background resume did not reach Serve")
	}
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[switched.ID]
	gen := tab.gen
	route := tab.routing.currentPath
	a.remoteTabMu.Unlock()
	if route != "/remote/sessions/s2.jsonl" {
		t.Fatalf("route while resume is held = %q, want target", route)
	}
	if !a.routeRemoteTabFrame(switched.ID, gen, route, "approval_request") {
		t.Fatal("target frame was rejected while resume request was held")
	}
	a.cacheRemotePendingEvent(switched.ID, gen, "approval_request", json.RawMessage(`{"kind":"approval_request","callId":"during-resume"}`))
	close(release)
	waitForRemoteSessionIdentity(t, a, meta.ID, "s2", "/remote/sessions/s2.jsonl")
	a.remoteTabMu.Lock()
	pending := len(a.remoteTabs[switched.ID].pendingEvents)
	a.remoteTabMu.Unlock()
	if pending != 1 {
		t.Fatalf("pending target event count after resume = %d, want 1", pending)
	}
	cleanupRemoteTabPumps(t, a)
}

// A slow resume that lands after a newer switch must not stomp the newer
// session's identity or re-emit ready out of order.
func TestOpenRemoteProjectTabLateResumeCannotStompNewerSwitch(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First", Turns: 1, Current: true},
		{Name: "s2", Path: "/remote/sessions/s2.jsonl", Title: "Second", Turns: 1},
		{Name: "s3", Path: "/remote/sessions/s3.jsonl", Title: "Third", Turns: 1},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "s1", SessionPath: "/remote/sessions/s1.jsonl"})

	started, release := make(chan string, 2), make(chan struct{})
	fs.mu.Lock()
	fs.resumeStarted, fs.resumeRelease = started, release
	fs.mu.Unlock()
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "s2", SessionPath: "/remote/sessions/s2.jsonl"}); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-started:
		if path != "/remote/sessions/s2.jsonl" {
			t.Fatalf("first resume path = %q, want s2", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first resume did not reach Serve")
	}
	// Switch again while the first Serve request is held: s3 must win.
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "s3", SessionPath: "/remote/sessions/s3.jsonl"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case path := <-started:
		if path != "/remote/sessions/s3.jsonl" {
			t.Fatalf("second resume path = %q, want s3", path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("newer resume did not reach Serve after the held request completed")
	}
	waitForRemoteSessionIdentity(t, a, meta.ID, "s3", "/remote/sessions/s3.jsonl")
	cleanupRemoteTabPumps(t, a)
}

func TestOpenRemoteProjectTabRejectedResumeRestoresPreviousIdentity(t *testing.T) {
	const oldPath = "/remote/sessions/s1.jsonl"
	const targetPath = "/remote/sessions/s2.jsonl"
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: oldPath, Title: "First", Turns: 1, Current: true},
		{Name: "s2", Path: targetPath, Title: "Second", Turns: 1},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "s1", SessionPath: oldPath, SessionTitle: "First"})

	fs.mu.Lock()
	fs.failEnter = "session is already leased by another process"
	fs.mu.Unlock()
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{
		SessionName: "s2", SessionPath: targetPath, SessionTitle: "Second",
	}); err != nil {
		t.Fatal(err)
	}
	waitForRemoteTabError(t, a, meta.ID, "already leased")

	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	name, path, route, title := tab.session.name, tab.session.path, tab.routing.currentPath, tab.topicTitle
	a.remoteTabMu.Unlock()
	if name != "s1" || path != oldPath || route != oldPath || title != "First" {
		t.Fatalf("rejected async resume kept target identity: name/path/route/title = %q/%q/%q/%q", name, path, route, title)
	}
}

func waitForRemoteSessionIdentity(t *testing.T, a *App, tabID, name, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.remoteTabMu.Lock()
		tab := a.remoteTabs[tabID]
		matches := tab != nil && tab.session.name == name && tab.session.path == path && tab.state == "ready"
		a.remoteTabMu.Unlock()
		if matches {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("remote tab %q did not settle on %q at %q", tabID, name, path)
}

// Re-registering an already-pinned remote project must not rewrite the user
// config file: OpenRemoteProjectTab re-adds on every click, and the repeated
// disk write shows up as switch latency.
func TestAddRemoteProjectSkipsConfigRewriteWhenPinned(t *testing.T) {
	seedBridgeTestHost(t, "box")
	if _, err := addRemoteProjectForTest(t, "box", "~/app"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	statBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	view, err := addRemoteProjectForTest(t, "box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Merged {
		t.Fatalf("re-add did not merge into the existing pin: %+v", view)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	statAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if statAfter.ModTime() != statBefore.ModTime() {
		t.Fatalf("config rewritten on re-add: mtime %v -> %v", statBefore.ModTime(), statAfter.ModTime())
	}
	if string(after) != string(before) {
		t.Fatalf("config content changed on re-add")
	}
}

func TestAddRemoteProjectSerializesNoOpDecisionWithRemoval(t *testing.T) {
	seedBridgeTestHost(t, "box")
	if _, err := addRemoteProjectForTest(t, "box", "~/app"); err != nil {
		t.Fatal(err)
	}

	unlock := config.LockUserConfigEdits()
	result := make(chan error, 1)
	go func() {
		_, err := addRemoteProjectForTest(t, "box", "~/app")
		result <- err
	}()
	select {
	case err := <-result:
		unlock()
		t.Fatalf("AddRemoteProject bypassed the edit lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if cfg == nil || !cfg.RemoveRemoteProject("box", "~/app") {
		unlock()
		t.Fatal("failed to stage concurrent project removal")
	}
	if err := cfg.SaveTo(path); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.RemoteProject("box", "~/app"); !ok {
		t.Fatal("successful AddRemoteProject was lost behind a concurrent removal")
	}
}

func addRemoteProjectForTest(t *testing.T, hostID, workspace string) (RemoteProjectView, error) {
	t.Helper()
	a := &App{}
	return a.AddRemoteProject(hostID, workspace)
}
