package main

import (
	"net/http"
	"testing"
)

// The explicit-intent listing contract: a remote group click may cold-start
// the serve, while the passive listing keeps refusing to wake one.

// TestRemoteProjectSessionsEnsureColdStartsServe pins the group-click path:
// with nothing running, EnsureRemoteProjectSessions boots the serve through
// EnsureServer and returns the mapped rows.
func TestRemoteProjectSessionsEnsureColdStartsServe(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First chat", Turns: 2},
	})
	kernel := &fakeRemoteKernel{
		statuses:     []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:   RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken:  "s3cret",
		snapshotMiss: true, // registry says nothing is running; EnsureServer can boot it
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)

	// The read-only listing must still refuse while the serve is down.
	if _, err := a.RemoteProjectSessions("box", "~/app"); err == nil {
		t.Fatal("read-only listing must fail while the serve is stopped")
	}

	rows, err := a.EnsureRemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if kernel.ensureCalls == 0 {
		t.Fatal("ensure listing never cold-started the serve")
	}
	if len(rows) != 1 || rows[0].Name != "s1" || rows[0].Title != "First chat" || rows[0].Turns != 2 {
		t.Fatalf("ensured sessions = %+v, want the mapped s1 entry", rows)
	}
	cleanupRemoteTabPumps(t, a)
}

// TestRemoteProjectSessionsEnsureReusesRunningServe pins the warm path: when
// a serve registration is already live, the ensure listing must not run a
// second EnsureServer round.
func TestRemoteProjectSessionsEnsureReusesRunningServe(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "First chat", Turns: 1},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)

	rows, err := a.EnsureRemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if kernel.ensureCalls != 0 {
		t.Fatalf("ensure listing booted a serve that was already running: %d calls", kernel.ensureCalls)
	}
	if len(rows) != 1 || rows[0].Name != "s1" {
		t.Fatalf("ensured sessions = %+v, want s1", rows)
	}
	cleanupRemoteTabPumps(t, a)
}

// TestRemoteProjectSessionsEnsureRejectsSuspendedTabClient pins reconnect
// recovery: a suspended tab retains its old client for later reattachment, but
// an explicit group refresh must not reuse that client as a live Serve path.
func TestRemoteProjectSessionsEnsureRejectsSuspendedTabClient(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "Recovered chat", Turns: 3},
	})
	kernel := &fakeRemoteKernel{
		statuses:     []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:   RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken:  "s3cret",
		snapshotMiss: true,
	}
	seedBridgeTestHost(t, "box")
	a := &App{
		remoteRuntime: kernel,
		remoteTabs: map[string]*remoteTab{
			"suspended": {
				id: "suspended", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"},
				state: "reconnecting", client: &http.Client{}, base: "http://127.0.0.1:1",
			},
		},
	}
	cleanupRemoteTabPumps(t, a)

	rows, err := a.EnsureRemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if kernel.ensureCalls == 0 {
		t.Fatal("ensure listing reused the suspended tab client instead of recovering Serve")
	}
	if len(rows) != 1 || rows[0].Name != "s1" {
		t.Fatalf("ensured sessions = %+v, want recovered s1", rows)
	}
}
