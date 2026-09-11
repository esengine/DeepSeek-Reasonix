package main

import (
	"strings"
	"testing"
)

// Blank sessions expose host\0workspace\0; after materialization the tab
// adopts Serve's Current name/path so the tree highlight migrates.

// TestRemoteTabBlankSessionIdentity pins the "+ new session" flow: after a
// named session was active, resetting to a blank session must flip the tab
// meta TopicID to the blank-row identity.
func TestRemoteTabBlankSessionIdentity(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First chat", Turns: 2, Current: true},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)

	named := openReadyRemoteTab(t, a, RemoteTabOpenOptions{
		SessionName:  "s1",
		SessionPath:  "/remote/sessions/s1.jsonl",
		SessionTitle: "First chat",
	})
	if want := "box\x00~/app\x00s1"; named.TopicID != want {
		t.Fatalf("named TopicID = %q, want %q", named.TopicID, want)
	}

	blank, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	if blank.ID != named.ID {
		t.Fatalf("blank open created a second tab %q; want reuse of %q", blank.ID, named.ID)
	}
	wantBlank := "box\x00~/app\x00"
	if blank.TopicID != wantBlank {
		t.Fatalf("blank TopicID = %q, want %q", blank.TopicID, wantBlank)
	}
	for _, tab := range a.ListTabs() {
		if tab.ID == blank.ID && tab.TopicID != wantBlank {
			t.Fatalf("ListTabs TopicID = %q, want %q", tab.TopicID, wantBlank)
		}
	}
	cleanupRemoteTabPumps(t, a)
}

// TestRemoteTabAdoptsMaterializedSessionIdentity pins the migration: once the
// serve listing marks the materialized session as Current, the tab adopts its
// Name/Path (not just the title) so the tree highlight moves off the blank
// row onto the named row.
func TestRemoteTabAdoptsMaterializedSessionIdentity(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First chat", Turns: 2},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)

	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	if want := "box\x00~/app\x00"; meta.TopicID != want {
		t.Fatalf("blank TopicID = %q, want %q", meta.TopicID, want)
	}
	// Ready is published immediately before the fresh-session metadata update.
	// Wait for that bootstrap event so it cannot race the adoption event below,
	// which is especially visible under Windows scheduling.
	waitForRemoteEventCount(t, log, "remote-tab:updated", 1)

	// The first turn lands: the serve now lists the fresh session as Current.
	fs.mu.Lock()
	fs.sessions = append(fs.sessions, serveSessionEntry{
		Name: "fresh-123", Path: "/remote/sessions/fresh.jsonl", Title: "Earned title", Turns: 1, Current: true,
	})
	fs.mu.Unlock()

	updatesBefore := log.count("remote-tab:updated")
	a.refreshRemoteTabTitle(meta.ID)

	wantTopic := "box\x00~/app\x00fresh-123"
	tab := a.remoteTabs[meta.ID]
	if tab == nil {
		t.Fatal("remote tab vanished")
	}
	if got := remoteTabTopicID(tab); got != wantTopic {
		t.Fatalf("adopted topic id = %q, want %q", got, wantTopic)
	}
	if tab.session.path != "/remote/sessions/fresh.jsonl" {
		t.Fatalf("adopted session path = %q, want /remote/sessions/fresh.jsonl", tab.session.path)
	}
	if tab.session.reset {
		t.Fatal("sessionReset must clear once the materialized session is adopted")
	}
	if got := log.count("remote-tab:updated"); got != updatesBefore+1 {
		t.Fatalf("materialized identity emitted %d updates after refresh, want 1", got-updatesBefore)
	}
	cleanupRemoteTabPumps(t, a)
}

// TestRemoteTabTopicIDSeparatorShape guards the identity shape itself: the
// blank id ends with the separator, so it can never equal a named row id.
func TestRemoteTabTopicIDSeparatorShape(t *testing.T) {
	blank := remoteTabTopicID(&remoteTab{ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}})
	if !strings.HasSuffix(blank, "\x00") || blank != "box\x00~/app\x00" {
		t.Fatalf("blank topic id = %q, want box\\0~/app\\0", blank)
	}
	named := remoteTabTopicID(&remoteTab{ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, session: remoteTabSessionState{name: "s1"}})
	if named != "box\x00~/app\x00s1" {
		t.Fatalf("named topic id = %q, want box\\0~/app\\0s1", named)
	}
}
