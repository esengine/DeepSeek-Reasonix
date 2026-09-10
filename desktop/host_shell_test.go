package main

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/desktop/internal/hostrpc"
	"reasonix/internal/extension/rpcwire"
)

// fakeShell answers host/* requests from the Go side and records them.
type fakeShell struct {
	conn    *rpcwire.Conn
	mu      sync.Mutex
	calls   []string
	replies map[string]any
}

func (f *fakeShell) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeShell) handle(method string) {
	f.conn.Handle(method, func(_ context.Context, _ json.RawMessage) (any, error) {
		f.mu.Lock()
		f.calls = append(f.calls, method)
		reply := f.replies[method]
		f.mu.Unlock()
		if reply == nil {
			return map[string]any{}, nil
		}
		return reply, nil
	})
}

func newHostShellBridgeForTest(t *testing.T, replies map[string]any) (*App, *hostShellBridge, *fakeShell) {
	t.Helper()
	isolateDesktopUserDirs(t)
	a := NewApp()
	registry, err := newDesktopRegistry(a)
	if err != nil {
		t.Fatal(err)
	}
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	serviceConn := rpcwire.NewConn(stdinR, stdoutW, rpcwire.Options{StrictJSONRPC: true, Name: "desktop-host"})
	shellConn := rpcwire.NewConn(stdoutR, stdinW, rpcwire.Options{StrictJSONRPC: true, Name: "shell"})
	shell := &fakeShell{conn: shellConn, replies: replies}
	for _, method := range []string{
		"host/tray.ensure", "host/tray.destroy", "host/window.show", "host/window.maximise", "host/window.unminimise",
		"host/remoteWindow.open", "host/remoteWindow.close", "host/app.relaunch", "host/app.quit",
	} {
		shell.handle(method)
	}
	bridge := &hostShellBridge{app: a}
	server := hostrpc.NewServer(serviceConn, hostrpc.ServerConfig{
		Registry: registry, Contract: hostrpc.Build(registry, hostEventNames), Generation: "g-test",
		Hooks: hostrpc.Hooks{HostEvent: bridge.handleHostEvent, BeforeClose: func(ctx context.Context, reason string) bool { return bridge.beforeClose(ctx, reason) }},
	})
	bridge.server = server
	a.hostShell = bridge
	a.setNativeHost(rpcNativeHost{server: server})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.Serve(ctx) }()
	go func() { _ = shellConn.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		stdinW.Close()
		stdoutW.Close()
	})
	return a, bridge, shell
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHostShellTrayFollowsTheShellAnswer(t *testing.T) {
	a, _, shell := newHostShellBridgeForTest(t, map[string]any{"host/tray.ensure": map[string]any{"ready": true}})
	if !a.startTray() {
		t.Fatal("tray should start when the shell reports ready")
	}
	if !a.isTrayReady() {
		t.Fatal("tray readiness must follow the shell reply")
	}
	if !a.startTray() {
		t.Fatal("a second start reuses the existing tray")
	}
	a.updateTrayLocale("zh")
	a.stopTray()
	if a.isTrayReady() {
		t.Fatal("tray must be unavailable after destroy")
	}
	got := shell.methods()
	want := []string{"host/tray.ensure", "host/tray.ensure", "host/tray.destroy"}
	if len(got) != len(want) {
		t.Fatalf("shell calls %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shell calls %v, want %v", got, want)
		}
	}
}

func TestHostShellTrayUnavailableWhenTheShellHasNone(t *testing.T) {
	a, _, _ := newHostShellBridgeForTest(t, map[string]any{"host/tray.ensure": map[string]any{"ready": false, "reason": "platform_no_tray"}})
	if a.startTray() {
		t.Fatal("tray must not start when the shell has no tray")
	}
	a.mu.RLock()
	state, reason, tray := a.desktopShell.trayState, a.desktopShell.trayReason, a.tray
	a.mu.RUnlock()
	if state != "unavailable" || reason != "platform_no_tray" || tray != nil {
		t.Fatalf("tray state %q/%q tray=%v", state, reason, tray)
	}
}

func TestHostShellRemoteWindowsTrackShellClosure(t *testing.T) {
	a, bridge, shell := newHostShellBridgeForTest(t, nil)
	launch := remoteWindowLaunch{URL: "http://127.0.0.1:1/", Title: "box", HostKey: remoteWindowHostKey("box")}
	if err := a.openRemoteWindowForHost("box", "/srv", launch.URL); err != nil {
		t.Fatal(err)
	}
	if !a.hasRemoteWindow("box") {
		t.Fatal("window must be tracked after open")
	}
	payload, _ := json.Marshal(map[string]string{"hostKey": launch.HostKey})
	if err := bridge.handleHostEvent(context.Background(), "remoteWindow.closed", payload); err != nil {
		t.Fatal(err)
	}
	if a.hasRemoteWindow("box") {
		t.Fatal("a window the user closed must be forgotten")
	}
	a.closeRemoteWindowForHost("box")
	if calls := shell.methods(); len(calls) != 1 || calls[0] != "host/remoteWindow.open" {
		t.Fatalf("closing a forgotten window must not reach the shell: %v", calls)
	}
	if err := a.openRemoteWindowForHost("box", "/srv", launch.URL); err != nil {
		t.Fatal(err)
	}
	a.closeAllRemoteWindows()
	if a.hasRemoteWindow("box") {
		t.Fatal("closeAll must drop every window")
	}
	waitFor(t, "close request", func() bool {
		calls := shell.methods()
		return len(calls) == 3 && calls[2] == "host/remoteWindow.close"
	})
}

func TestHostShellQuitReasonBypassesBackgroundClose(t *testing.T) {
	_, bridge, _ := newHostShellBridgeForTest(t, nil)
	if consumeSystemQuitRequested() {
		t.Fatal("no system quit should be pending before the test")
	}
	if prevent := bridge.beforeClose(context.Background(), "quit"); prevent {
		t.Fatal("a quit must never be prevented by background close")
	}
	if consumeSystemQuitRequested() {
		t.Fatal("beforeClose must consume the quit marker it set")
	}
}

func TestHostShellEventsReachTheAppEntryPoints(t *testing.T) {
	a, bridge, shell := newHostShellBridgeForTest(t, nil)
	a.ctx = context.Background()
	for _, name := range []string{"secondInstance", "tray.open", "menu.showWindow", "unknown.event"} {
		if err := bridge.handleHostEvent(context.Background(), name, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	waitFor(t, "window show requests", func() bool {
		shown := 0
		for _, m := range shell.methods() {
			if m == "host/window.show" {
				shown++
			}
		}
		return shown >= 3
	})
}
