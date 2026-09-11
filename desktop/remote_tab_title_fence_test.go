package main

import (
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestRemoteTabTitleRefreshRejectsDifferentServeCurrent(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "a", Path: "/a.jsonl", Title: "Title A", Current: true},
		{Name: "b", Path: "/b.jsonl", Title: "Title B"},
	})
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "a", SessionPath: "/a.jsonl", SessionTitle: "Selected A"})
	fs.mu.Lock()
	fs.sessions[0].Current = false
	fs.sessions[1].Current = true
	fs.mu.Unlock()

	a.refreshRemoteTabTitle(meta.ID)

	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	name, sessionPath, route, title := tab.session.name, tab.session.path, tab.routing.currentPath, tab.topicTitle
	a.remoteTabMu.Unlock()
	if name != "a" || sessionPath != "/a.jsonl" || route != "/a.jsonl" || title != "Selected A" {
		t.Fatalf("different Serve current replaced title-refresh target: %q/%q/%q/%q", name, sessionPath, route, title)
	}
}

func TestRemoteTabTitleRefreshKeepsBlankPreferencesAfterReconnect(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	if err := setRemoteSessionTitleOverride("box", "~/app", "", "Unsaved title"); err != nil {
		t.Fatal(err)
	}
	if err := setRemoteSessionPinned("box", "~/app", "", true); err != nil {
		t.Fatal(err)
	}

	fs.mu.Lock()
	fs.sessions = []serveSessionEntry{{Name: "stale", Path: "/stale.jsonl", Title: "Serve title", Current: true}}
	fs.sessionsStarted = make(chan struct{}, 1)
	fs.sessionsRelease = make(chan struct{})
	started, release := fs.sessionsStarted, fs.sessionsRelease
	fs.mu.Unlock()
	done := make(chan struct{})
	go func() { a.refreshRemoteTabTitle(meta.ID); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("title refresh did not start")
	}
	a.remoteTabMu.Lock()
	a.remoteTabs[meta.ID].gen++
	a.remoteTabMu.Unlock()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("title refresh did not return")
	}

	if got := remoteSessionTitleOverride("box", "~/app", ""); got != "Unsaved title" {
		t.Fatalf("blank title after stale response = %q", got)
	}
	if !remoteSessionPinned("box", "~/app", "") {
		t.Fatal("blank pin was removed by a stale response")
	}
	if got := remoteSessionTitleOverride("box", "~/app", "stale"); got != "" || remoteSessionPinned("box", "~/app", "stale") {
		t.Fatalf("stale response received blank preferences: title=%q pinned=%v", got, remoteSessionPinned("box", "~/app", "stale"))
	}
}

func TestRemoteTabTitleRefreshMigratesPreferencesWithoutHoldingRegistryLock(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	if err := setRemoteSessionTitleOverride("box", "~/app", "", "Unsaved title"); err != nil {
		t.Fatal(err)
	}

	fs.mu.Lock()
	fs.sessions = []serveSessionEntry{{Name: "fresh", Path: "/fresh.jsonl", Title: "Serve title", Current: true}}
	fs.sessionsStarted = make(chan struct{}, 1)
	fs.sessionsRelease = make(chan struct{})
	started, release := fs.sessionsStarted, fs.sessionsRelease
	fs.mu.Unlock()
	done := make(chan struct{})
	go func() { a.refreshRemoteTabTitle(meta.ID); close(done) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("title refresh did not start")
	}

	unlockPrefs, err := config.LockConfigFileEdits(remotePrefsPath())
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for remotePrefsMu.TryLock() {
		remotePrefsMu.Unlock()
		if time.Now().After(deadline) {
			unlockPrefs()
			t.Fatal("title refresh did not enter preference migration")
		}
		time.Sleep(time.Millisecond)
	}

	registryAvailable := make(chan bool, 1)
	go func() {
		a.remoteTabMu.Lock()
		registered := a.remoteTabs[meta.ID] != nil
		a.remoteTabMu.Unlock()
		registryAvailable <- registered
	}()
	select {
	case registered := <-registryAvailable:
		if !registered {
			unlockPrefs()
			t.Fatal("remote tab disappeared during preference migration")
		}
	case <-time.After(250 * time.Millisecond):
		unlockPrefs()
		<-registryAvailable
		t.Fatal("preference migration held the global remote tab registry lock")
	}
	unlockPrefs()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("title refresh did not finish after preference lock release")
	}

	a.remoteTabMu.Lock()
	tab := a.remoteTabs[meta.ID]
	name, path, title := tab.session.name, tab.session.path, tab.topicTitle
	a.remoteTabMu.Unlock()
	if name != "fresh" || path != "/fresh.jsonl" || title != "Unsaved title" {
		t.Fatalf("materialized identity/title = %q/%q/%q", name, path, title)
	}
}
