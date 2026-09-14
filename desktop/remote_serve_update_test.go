package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/remote/forward"
	"reasonix/internal/remote/sshtest"
)

func TestRemoteServeUpdateAvailable(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "1.9.5"
	cases := []struct {
		serve string
		want  bool
	}{
		{"1.9.0", true},
		{"v1.9.0", true},
		{"reasonix v1.8.2", true},
		{"1.9.5", false},
		{"2.0.0", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := remoteServeUpdateAvailable(tc.serve); got != tc.want {
			t.Errorf("remoteServeUpdateAvailable(%q) = %v, want %v", tc.serve, got, tc.want)
		}
	}
	version = "dev"
	if remoteServeUpdateAvailable("1.0.0") {
		t.Error("a dev desktop must never flag an update")
	}
}

// newUpdateTestManager wires a manager whose host entry and forwards are real
// enough for the full ensure path: the SSH client is backed by an sshtest
// server so the local serve forward can bind.
func newUpdateTestManager(t *testing.T, hostID string) *desktopRemoteManager {
	t.Helper()
	seedLifecycleHost(t, hostID)
	srv := sshtest.Start(t, sshtest.Options{SFTPRoot: t.TempDir()})
	cl, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{User: "t", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	fs := forward.NewSet(nil)
	if err := fs.Attach(cl); err != nil {
		t.Fatal(err)
	}
	client := &lifecycleSSHClient{forwards: fs}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mgr := newDesktopRemoteManager(nil)
	mgr.mu.Lock()
	mgr.hosts[hostID] = &managedHost{ctx: ctx, cancel: cancel, client: client}
	mgr.mu.Unlock()
	return mgr
}

// captureEnsure records the bootstrap options of every ensure call and
// answers with a fresh 1.9.5 serve.
func captureEnsure(mgr *desktopRemoteManager) *[]bootstrap.Options {
	var seen []bootstrap.Options
	mgr.ensureServe = func(_ context.Context, _ bootstrap.Conn, opts bootstrap.Options) (bootstrap.Result, error) {
		seen = append(seen, opts)
		return bootstrap.Result{
			State: bootstrap.ServeState{PID: 9, Addr: "127.0.0.1:45678", Workspace: "/ws", Version: "1.9.5", ServeCaps: bootstrap.ServeCapsToken, TokenFile: "/tok"},
			Token: "fresh",
		}, nil
	}
	return &seen
}

func TestUpdateServerStopsAndForceEnsures(t *testing.T) {
	mgr := newUpdateTestManager(t, "box")
	ready := RemoteServerView{HostID: "box", Workspace: "/ws", State: "ready", LocalURL: "http://127.0.0.1:1/"}
	mh := mgr.managed("box")
	mgr.mu.Lock()
	mh.serves = map[string]*serveEntry{"/ws": {view: ready, token: "old", addr: "127.0.0.1:1111"}}
	mgr.mu.Unlock()

	stopped := ""
	mgr.stopServe = func(_ context.Context, _ bootstrap.Conn, workspace string) error {
		stopped = workspace
		return nil
	}
	seen := captureEnsure(mgr)

	view, token, err := mgr.UpdateServer(context.Background(), "box", "/ws")
	if err != nil {
		t.Fatalf("UpdateServer: %v", err)
	}
	if stopped != "/ws" {
		t.Fatalf("stopServe workspace = %q, want /ws", stopped)
	}
	if len(*seen) != 1 || !(*seen)[0].ForceUpgrade {
		t.Fatalf("ensure options = %+v, want one forced call", *seen)
	}
	if view.State != "ready" || view.ServeVersion != "1.9.5" || token != "fresh" {
		t.Fatalf("view = %+v, token = %q", view, token)
	}
	if view.UpdateAvailable {
		t.Fatal("a dev desktop must not flag an update")
	}
}

func TestUpdateServerRequiresTrackedServe(t *testing.T) {
	mgr := newUpdateTestManager(t, "box")
	if _, _, err := mgr.UpdateServer(context.Background(), "box", "/ws"); err == nil {
		t.Fatal("updating an untracked workspace must fail")
	}
}

func TestEnsureServerDoesNotForceUpgrade(t *testing.T) {
	mgr := newUpdateTestManager(t, "box")
	seen := captureEnsure(mgr)

	view, _, err := mgr.EnsureServer(context.Background(), "box", "/ws")
	if err != nil {
		t.Fatalf("EnsureServer: %v", err)
	}
	if len(*seen) != 1 || (*seen)[0].ForceUpgrade {
		t.Fatalf("ensure options = %+v, want one unforced call", *seen)
	}
	if view.State != "ready" || view.ServeVersion != "1.9.5" {
		t.Fatalf("ready view must carry the serve version: %+v", view)
	}
}

func TestUpdateRemoteServerReattachesTabs(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", Workspace: "~/app", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	before := kernel.ensureCalls
	if err := a.UpdateRemoteServer("box", "~/app"); err != nil {
		t.Fatal(err)
	}
	if kernel.updateCalls != 1 {
		t.Fatalf("kernel UpdateServer calls = %d, want 1", kernel.updateCalls)
	}
	waitForTabState(t, a, meta.ID, "ready")
	a.remoteTabMu.Lock()
	_, client := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].client
	a.remoteTabMu.Unlock()
	if client == nil {
		t.Fatal("reattached tab must have a live client")
	}
	if kernel.ensureCalls <= before {
		t.Fatalf("reattach must drive a fresh ensure: %d -> %d", before, kernel.ensureCalls)
	}
}

func TestUpdateRemoteServerErrorStillReattaches(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", Workspace: "~/app", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	kernel.ensureErr = errors.New("boom")
	before := kernel.ensureCalls
	err := a.UpdateRemoteServer("box", "~/app")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want the kernel failure", err)
	}
	if kernel.updateCalls != 1 {
		t.Fatalf("kernel UpdateServer calls = %d, want 1", kernel.updateCalls)
	}
	waitForTabState(t, a, meta.ID, "error")
	if kernel.ensureCalls <= before {
		t.Fatalf("a failed update must still reattach tabs: %d -> %d", before, kernel.ensureCalls)
	}
}
