package main

import (
	"strings"
	"testing"
)

// RemoteTabSnapshot sanitizes user rows even when an older remote Serve
// returns provider-only transient blocks in /history.
func TestRemoteTabSnapshotStripsTransientUserBlocks(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "s1", Path: "/remote/sessions/s1.jsonl", Current: true}})
	fs.mu.Lock()
	fs.historyBody = `[{"role":"user","content":"<reasoning-language>zh</reasoning-language>\n\nhello"},{"role":"assistant","content":"three <b>bold</b> stays"}]`
	fs.mu.Unlock()
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "s1", SessionPath: "/remote/sessions/s1.jsonl"})

	snap, err := a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := string(snap.History)
	if strings.Contains(body, "<reasoning-language>") || !strings.Contains(body, "hello") {
		t.Fatalf("history was not sanitized: %.200s", body)
	}
	if !strings.Contains(body, "three <b>bold</b> stays") {
		t.Fatalf("assistant content changed: %.200s", body)
	}
}
