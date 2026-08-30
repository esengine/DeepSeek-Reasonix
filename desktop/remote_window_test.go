package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/remote"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/remote/sshtest"
)

type reconnectWindowSink struct {
	app      *App
	statuses chan RemoteConnectionStatusView
}

func (s *reconnectWindowSink) onStatus(v RemoteConnectionStatusView) {
	s.app.onStatus(v)
	select {
	case s.statuses <- v:
	default:
	}
}

func (s *reconnectWindowSink) onForwards(hostID string, forwards []RemoteForwardView) {
	s.app.onForwards(hostID, forwards)
}

func (s *reconnectWindowSink) onServer(v RemoteServerView) { s.app.onServer(v) }

// TestRemoteWindowHelperProcess is the target of registry tests that need a
// real live child process: it is spawned via os.Args[0] with a marker env and
// blocks until the test kills it.
func TestRemoteWindowHelperProcess(t *testing.T) {
	if os.Getenv("REMOTE_WINDOW_HELPER_PROCESS") != "1" {
		return
	}
	time.Sleep(120 * time.Second)
}

func spawnRemoteWindowHelper(t *testing.T) *os.Process {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestRemoteWindowHelperProcess")
	cmd.Env = append(os.Environ(), "REMOTE_WINDOW_HELPER_PROCESS=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process
}

func waitRemoteWindowHelperExit(t *testing.T, proc *os.Process) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("remote window helper process did not exit after kill")
	}
}

func TestRemoteWindowTicketRoundTripAndRemoval(t *testing.T) {
	launch := remoteWindowLaunch{
		URL:     "http://127.0.0.1:54321/?token=secret-token",
		Title:   "Reasonix [SSH: box]",
		HostKey: "host-key-digest",
	}
	ticket, err := writeRemoteWindowLaunch(launch)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ticket, "secret-token") || filepath.Base(ticket) != ticket {
		t.Fatalf("ticket leaked URL data or path: %q", ticket)
	}
	path, err := remoteWindowTicketPath(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("ticket permissions = %o, want 600", got)
		}
	}
	got, err := consumeRemoteWindowLaunch(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if *got != launch {
		t.Fatalf("launch = %+v, want %+v", *got, launch)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("ticket was not removed after consumption: %v", err)
	}
}

func TestConsumeInitialRemoteWindowLaunchIsIdempotentAcrossDomReady(t *testing.T) {
	launch := remoteWindowLaunch{
		URL:     "http://127.0.0.1:54321/?token=secret-token",
		Title:   "Reasonix [SSH: box]",
		HostKey: "host-key-digest",
	}
	ticket, err := writeRemoteWindowLaunch(launch)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{remoteWindowTicket: ticket}

	got, first, err := a.consumeInitialRemoteWindowLaunch()
	if err != nil {
		t.Fatal(err)
	}
	if !first || got == nil || *got != launch {
		t.Fatalf("first consume = (%+v, %v), want (%+v, true)", got, first, launch)
	}
	if _, err := os.Stat(filepath.Join(config.MemoryUserDir(), ticket)); !os.IsNotExist(err) {
		t.Fatalf("initial ticket was not removed: %v", err)
	}

	got, first, err = a.consumeInitialRemoteWindowLaunch()
	if err != nil {
		t.Fatalf("repeated domReady returned an error: %v", err)
	}
	if first || got != nil {
		t.Fatalf("repeated consume = (%+v, %v), want (nil, false)", got, first)
	}
}

func TestRemoteWindowTicketRejectsUnsafeInputs(t *testing.T) {
	for _, raw := range []string{
		"https://127.0.0.1:5000/?token=x",
		"http://example.com:5000/?token=x",
		"file:///tmp/index.html",
		"javascript:alert(1)",
	} {
		if _, err := writeRemoteWindowLaunch(remoteWindowLaunch{URL: raw, HostKey: "k"}); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
	if _, err := writeRemoteWindowLaunch(remoteWindowLaunch{URL: "http://127.0.0.1:5000/"}); err == nil {
		t.Fatal("ticket without host identity accepted")
	}
	for _, ticket := range []string{"", "../.remote-window-x", "/tmp/.remote-window-x", "unrelated"} {
		if _, err := remoteWindowTicketPath(ticket); err == nil {
			t.Fatalf("unsafe ticket accepted: %q", ticket)
		}
	}
}

func TestConsumeRemoteWindowTicketRejectsBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits through os.Stat")
	}
	dir := config.MemoryUserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ticket := remoteWindowTicketPrefix + "insecure"
	path := filepath.Join(dir, ticket)
	if err := os.WriteFile(path, []byte(`{"url":"http://127.0.0.1:5000/","hostKey":"k"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if _, err := consumeRemoteWindowLaunch(ticket); err == nil {
		t.Fatal("ticket with broad permissions was accepted")
	}
}

func TestConsumeRemoteWindowTicketRejectsOversizedDescriptor(t *testing.T) {
	dir := config.MemoryUserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ticket := remoteWindowTicketPrefix + "oversized"
	path := filepath.Join(dir, ticket)
	if err := os.WriteFile(path, make([]byte, remoteWindowTicketMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := consumeRemoteWindowLaunch(ticket); err == nil {
		t.Fatal("oversized ticket was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected ticket was not removed: %v", err)
	}
}

func TestConsumeRemoteWindowTicketRejectsExpiredTicket(t *testing.T) {
	launch := remoteWindowLaunch{URL: "http://127.0.0.1:54321/", HostKey: "k"}
	ticket, err := writeRemoteWindowLaunch(launch)
	if err != nil {
		t.Fatal(err)
	}
	path, err := remoteWindowTicketPath(ticket)
	if err != nil {
		t.Fatal(err)
	}
	// Age the ticket beyond the TTL so consumption must reject it even though
	// the spawning process's AfterFunc backstop never ran.
	old := time.Now().Add(-remoteWindowTicketTTL - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := consumeRemoteWindowLaunch(ticket); err == nil {
		t.Fatal("expired ticket was accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired ticket was not removed: %v", err)
	}
}

func TestRemoteWindowNavigationJSEscapesURL(t *testing.T) {
	js, err := remoteWindowNavigationJS("http://127.0.0.1:5000/?token=x%22);alert(1)//")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(js, "window.location.replace(\"") || !strings.HasSuffix(js, "\");") {
		t.Fatalf("unexpected navigation JS: %q", js)
	}
	if strings.Contains(js, "\");alert") {
		t.Fatalf("URL escaped the JS string: %q", js)
	}
}

func TestRemoteWindowHostKeyDistinguishesHosts(t *testing.T) {
	a := remoteWindowHostKey("host-a")
	b := remoteWindowHostKey("host-b")
	ownerA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ownerB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if a == b {
		t.Fatal("distinct hosts share a window identity")
	}
	if remoteWindowHostKey("host-a") != a {
		t.Fatal("host key is not stable for the same host")
	}
	if strings.Contains(a, "host-a") || strings.Contains(b, "host-b") {
		t.Fatal("host key leaks the host label")
	}
	if remoteWindowInstanceID(a, ownerA) == remoteWindowInstanceID(b, ownerA) {
		t.Fatal("instance IDs collide across hosts")
	}
	if remoteWindowInstanceID(a, ownerA) == remoteWindowInstanceID(a, ownerB) {
		t.Fatal("a restarted Desktop would adopt the previous owner's child window")
	}
	firstInstanceID := remoteWindowInstanceID(a, ownerA)
	secondInstanceID := remoteWindowInstanceID(a, ownerA)
	if firstInstanceID != secondInstanceID {
		t.Fatal("instance ID is not stable within one Desktop owner")
	}
	if !strings.HasPrefix(firstInstanceID, remoteWindowInstancePrefix) {
		t.Fatalf("instance ID = %q, want %q prefix", firstInstanceID, remoteWindowInstancePrefix)
	}
}

func TestRemoteWindowOwnerIdentityIsRandomAndValid(t *testing.T) {
	a := newRemoteWindowOwnerID()
	b := newRemoteWindowOwnerID()
	if !isRemoteWindowOwnerID(a) || !isRemoteWindowOwnerID(b) {
		t.Fatalf("invalid owner identities: %q %q", a, b)
	}
	if a == b {
		t.Fatal("two Desktop processes received the same remote window owner identity")
	}
	for _, invalid := range []string{"", "short", strings.Repeat("g", 32), strings.Repeat("a", 31)} {
		if isRemoteWindowOwnerID(invalid) {
			t.Fatalf("invalid owner identity accepted: %q", invalid)
		}
	}
}

func TestRemoteWindowOwnerWaitDetectsParentExit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestRemoteWindowHelperProcess")
	cmd.Env = append(os.Environ(), "REMOTE_WINDOW_HELPER_PROCESS=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	exited := make(chan bool, 1)
	go func() { exited <- waitForRemoteWindowOwnerExit(ctx, cmd.Process.Pid) }()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()
	select {
	case detected := <-exited:
		if !detected {
			t.Fatal("owner watcher stopped without detecting process exit")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("owner watcher did not detect process exit")
	}
}

func TestRemoteWindowRegistryHandoffExitKeepsLiveWindow(t *testing.T) {
	r := newRemoteWindowRegistry()
	key := "host-a"

	// W1 is the live window for the host. Re-opening the host spawns W2, which
	// exits at the Wails single-instance gate after handing its ticket to W1.
	// W2's Wait must clear only W2's own entry — W1 stays registered so
	// disconnect/stop/quit can still close it and reconnect can re-point it.
	live := spawnRemoteWindowHelper(t)
	defer func() { _ = live.Kill(); waitRemoteWindowHelperExit(t, live) }()
	liveGen := r.record(key, live)
	handoff := spawnRemoteWindowHelper(t)
	defer func() { _ = handoff.Kill(); waitRemoteWindowHelperExit(t, handoff) }()
	handoffGen := r.record(key, handoff)

	r.clearIf(key, handoffGen, handoff.Pid)
	if !r.has(key) {
		t.Fatal("handoff Wait cleared the live window registration")
	}

	// The live window's own exit clears the host's registration.
	r.clearIf(key, liveGen, live.Pid)
	if r.has(key) {
		t.Fatal("registration not cleared by the live window's own Wait")
	}
}

func TestRemoteWindowHostLifecycleSkipsSupersededOperation(t *testing.T) {
	var registry remoteWindowLifecycleRegistry
	stale := registry.begin("box")
	current := registry.begin("box")

	staleCalled := false
	if err := stale.run(func(func() bool) error {
		staleCalled = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if staleCalled {
		t.Fatal("superseded host lifecycle operation executed")
	}

	currentCalled := false
	if err := current.run(func(isCurrent func() bool) error {
		currentCalled = true
		if !isCurrent() {
			t.Fatal("current host lifecycle operation lost its generation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !currentCalled {
		t.Fatal("latest host lifecycle operation did not execute")
	}
}

func TestRemoteWindowRegistryGenerationProtectsNewerWindow(t *testing.T) {
	r := newRemoteWindowRegistry()
	key := "host-b"

	p1 := spawnRemoteWindowHelper(t)
	defer func() { _ = p1.Kill(); waitRemoteWindowHelperExit(t, p1) }()
	g1 := r.record(key, p1)
	if !r.has(key) {
		t.Fatal("first child not registered")
	}

	// A newer spawn for the same host gets a distinct entry and generation. A
	// stale Wait from the old child cannot remove the new entry.
	p2 := spawnRemoteWindowHelper(t)
	defer func() { _ = p2.Kill(); waitRemoteWindowHelperExit(t, p2) }()
	g2 := r.record(key, p2)
	if g2 <= g1 {
		t.Fatalf("generation did not advance: %d then %d", g1, g2)
	}
	r.clearIf(key, g1, p1.Pid)
	if !r.has(key) {
		t.Fatal("stale Wait removed the newer registration")
	}
	r.clearIf(key, g2, p2.Pid)
	if r.has(key) {
		t.Fatal("registration not cleared by its own Wait")
	}
}

func TestRemoteWindowRegistryCloseTerminatesChild(t *testing.T) {
	r := newRemoteWindowRegistry()
	key := "host-c"
	p := spawnRemoteWindowHelper(t)
	r.record(key, p)
	r.close(key)
	waitRemoteWindowHelperExit(t, p)
	if r.has(key) {
		t.Fatal("closed child still registered")
	}
}

func TestRemoteWindowRegistryCloseAllTerminatesAll(t *testing.T) {
	r := newRemoteWindowRegistry()
	p1 := spawnRemoteWindowHelper(t)
	defer waitRemoteWindowHelperExit(t, p1)
	p2 := spawnRemoteWindowHelper(t)
	defer waitRemoteWindowHelperExit(t, p2)
	r.record("host-a", p1)
	r.record("host-b", p2)
	r.closeAll()
	waitRemoteWindowHelperExit(t, p1)
	waitRemoteWindowHelperExit(t, p2)
	if r.has("host-a") || r.has("host-b") {
		t.Fatal("closeAll left registrations behind")
	}
}

func TestRemoteWindowLifecycleSkipsPrimaryRuntime(t *testing.T) {
	a := NewApp()
	a.remoteWindowTicket = remoteWindowTicketPrefix + "test"
	a.startup(context.Background())
	if a.tabsRestored != nil {
		t.Fatal("remote window initialized local tab restore")
	}
	if a.heartbeat != nil || a.tray != nil || a.remoteRuntime != nil {
		t.Fatal("remote window initialized primary-process runtime")
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("remote window close was intercepted")
	}
	a.shutdown(context.Background())
}

func TestRemoteWindowAssetMiddlewareDoesNotLoadPrimaryFrontend(t *testing.T) {
	a := &App{remoteWindowTicket: remoteWindowTicketPrefix + "shell"}
	nextCalled := false
	h := a.remoteWindowAssetMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if nextCalled {
		t.Fatal("remote shell loaded the primary asset handler")
	}
	if strings.Contains(rec.Body.String(), "<script") {
		t.Fatal("remote shell bootstrap unexpectedly contains frontend scripts")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestRemoteWindowAssetMiddlewarePassesThroughMainApp(t *testing.T) {
	a := &App{}
	nextCalled := false
	h := a.remoteWindowAssetMiddleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !nextCalled {
		t.Fatal("main app shell intercepted by the remote window middleware")
	}
}

func TestRemoteWindowTitleSanitizesHostLabel(t *testing.T) {
	if got := remoteWindowTitle(" box\nprod "); got != "Reasonix [SSH: boxprod]" {
		t.Fatalf("title = %q", got)
	}
}

func TestServeURLWithToken(t *testing.T) {
	cases := []struct {
		localURL, token, want string
	}{
		{"http://127.0.0.1:54321/", "tok-1", "http://127.0.0.1:54321?token=tok-1"},
		{"http://127.0.0.1:54321", "tok-1", "http://127.0.0.1:54321?token=tok-1"},
		{"http://127.0.0.1:54321/?token=old", "tok-1", "http://127.0.0.1:54321/?token=old"},
		{"http://127.0.0.1:54321/", "", "http://127.0.0.1:54321/"},
	}
	for _, c := range cases {
		if got := serveURLWithToken(c.localURL, c.token); got != c.want {
			t.Fatalf("serveURLWithToken(%q, %q) = %q, want %q", c.localURL, c.token, got, c.want)
		}
	}
}

func TestOpenRemoteWorkspaceOpensWebWindow(t *testing.T) {
	fake := &fakeRemoteKernel{
		ensureView:  RemoteServerView{HostID: "box", Workspace: "/srv", State: "ready", LocalURL: "http://127.0.0.1:54321/"},
		ensureToken: "tok-123",
	}
	a := NewApp()
	a.remoteRuntime = fake
	calls := make(chan remoteWindowLaunch, 2)
	a.remoteWindowOpener = func(l remoteWindowLaunch) error {
		calls <- l
		return nil
	}
	if err := a.OpenRemoteWorkspace("box", "/srv"); err != nil {
		t.Fatal(err)
	}
	select {
	case l := <-calls:
		want := "http://127.0.0.1:54321?token=tok-123"
		if l.URL != want {
			t.Fatalf("window URL = %q, want %q", l.URL, want)
		}
		if !strings.Contains(l.Title, "box") {
			t.Fatalf("window title = %q, want host label", l.Title)
		}
		if l.HostKey != remoteWindowHostKey("box") {
			t.Fatalf("window host key = %q", l.HostKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no web window opened")
	}
	if got := a.RemoteLastWorkspace("box"); got != "/srv" {
		t.Fatalf("last workspace = %q, want /srv", got)
	}
}

func TestOpenRemoteWorkspaceFailureKeepsWindowUntouched(t *testing.T) {
	const hostID = "failing-box"
	fake := &fakeRemoteKernel{ensureErr: errors.New("serve failed")}
	a := NewApp()
	a.remoteRuntime = fake
	a.remoteWindowOpener = func(remoteWindowLaunch) error {
		t.Fatal("window opened on serve failure")
		return nil
	}
	if err := a.OpenRemoteWorkspace(hostID, "/srv"); err == nil {
		t.Fatal("expected serve failure to surface")
	}
	if got := a.RemoteLastWorkspace(hostID); got != "" {
		t.Fatalf("failed open recorded last workspace %q", got)
	}
}

// TestOpenRemoteWorkspaceWorkspaceSwitchRequiresServerSuccess covers the atomic
// switch contract: a new Serve + tunnel must be established before the window
// is re-pointed; a failed switch keeps the previous window and last workspace.
func TestOpenRemoteWorkspaceWorkspaceSwitchRequiresServerSuccess(t *testing.T) {
	fake := &fakeRemoteKernel{
		ensureView:  RemoteServerView{HostID: "box", Workspace: "/srv", State: "ready", LocalURL: "http://127.0.0.1:54321/"},
		ensureToken: "t1",
	}
	a := NewApp()
	a.remoteRuntime = fake
	calls := make(chan remoteWindowLaunch, 4)
	a.remoteWindowOpener = func(l remoteWindowLaunch) error {
		calls <- l
		return nil
	}
	if err := a.OpenRemoteWorkspace("box", "/srv"); err != nil {
		t.Fatal(err)
	}
	if l := <-calls; !strings.HasSuffix(l.URL, "token=t1") {
		t.Fatalf("first window URL = %q", l.URL)
	}

	// Successful switch to a new workspace re-points the window.
	fake.ensureView = RemoteServerView{HostID: "box", Workspace: "/srv2", State: "ready", LocalURL: "http://127.0.0.1:5555/"}
	fake.ensureToken = "t2"
	if err := a.OpenRemoteWorkspace("box", "/srv2"); err != nil {
		t.Fatal(err)
	}
	if l := <-calls; !strings.HasSuffix(l.URL, "token=t2") {
		t.Fatalf("switched window URL = %q", l.URL)
	}

	// Failed switch leaves the window and last workspace untouched.
	fake.ensureErr = errors.New("boom")
	if err := a.OpenRemoteWorkspace("box", "/srv3"); err == nil {
		t.Fatal("expected switch failure to surface")
	}
	select {
	case l := <-calls:
		t.Fatalf("window re-pointed on failed switch: %q", l.URL)
	case <-time.After(200 * time.Millisecond):
	}
	if got := a.RemoteLastWorkspace("box"); got != "/srv2" {
		t.Fatalf("last workspace after failed switch = %q, want /srv2", got)
	}
}

func TestRemoteWindowCloseOnTerminalDisconnectKeepsTransient(t *testing.T) {
	a := NewApp()
	key := remoteWindowHostKey("box")
	p := spawnRemoteWindowHelper(t)
	defer waitRemoteWindowHelperExit(t, p)
	a.remoteWindows.record(key, p)

	// Transient reconnect states keep the window.
	a.onStatus(RemoteConnectionStatusView{HostID: "box", State: "reconnecting"})
	a.onStatus(RemoteConnectionStatusView{HostID: "box", State: "degraded"})
	if !a.hasRemoteWindow("box") {
		t.Fatal("window closed during transient reconnect")
	}

	// A deterministic terminal failure closes it.
	a.onStatus(RemoteConnectionStatusView{HostID: "box", State: "stopped", Error: "auth failed"})
	waitRemoteWindowHelperExit(t, p)
	if a.hasRemoteWindow("box") {
		t.Fatal("window survived terminal disconnect")
	}
}

func TestRemoteWindowReconnectRepointsWindow(t *testing.T) {
	fake := &fakeRemoteKernel{
		ensureView:  RemoteServerView{HostID: "box", Workspace: "/srv", State: "ready", LocalURL: "http://127.0.0.1:9999/"},
		ensureToken: "tok-re",
	}
	a := NewApp()
	a.remoteRuntime = fake
	key := remoteWindowHostKey("box")
	p := spawnRemoteWindowHelper(t)
	defer func() { _ = p.Kill(); waitRemoteWindowHelperExit(t, p) }()
	a.remoteWindows.record(key, p)
	calls := make(chan remoteWindowLaunch, 2)
	a.remoteWindowOpener = func(l remoteWindowLaunch) error {
		calls <- l
		return nil
	}

	a.onStatus(RemoteConnectionStatusView{HostID: "box", State: "connected"})
	select {
	case l := <-calls:
		if l.URL != "http://127.0.0.1:9999?token=tok-re" {
			t.Fatalf("re-pointed URL = %q", l.URL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("window not re-pointed after reconnect")
	}
}

func TestRemoteWindowRecoversAcrossRealSSHDrop(t *testing.T) {
	const hostID = "box"
	sshServer := sshtest.Start(t, sshtest.Options{Password: "test-password"})
	serve := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("remote-serve-ok"))
	}))
	defer serve.Close()

	host, err := remote.ResolveHost(nil, "test@"+sshServer.Addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	knownHostsDir := t.TempDir()
	policy := &remote.HostKeyPolicy{
		SystemKnownHosts: []string{filepath.Join(knownHostsDir, "none")},
		ManagedPath:      filepath.Join(knownHostsDir, "known_hosts"),
		Prompt: func(context.Context, remote.HostKeyQuestion) (bool, error) {
			return true, nil
		},
	}

	seedLifecycleHost(t, hostID)
	a := NewApp()
	sink := &reconnectWindowSink{app: a, statuses: make(chan RemoteConnectionStatusView, 32)}
	mgr := newDesktopRemoteManager(sink)
	a.remoteRuntime = mgr
	mgr.newClient = func(opts remote.Options) (desktopSSHClient, error) {
		opts.Host = host
		opts.HostKeys = policy
		opts.Auth = remote.AuthOptions{
			DisableAgent: true,
			Password:     func() (string, error) { return "test-password", nil },
		}
		opts.Keepalive = remote.KeepalivePolicy{Interval: 25 * time.Millisecond, MaxMisses: 1, Timeout: 200 * time.Millisecond}
		opts.Backoff = remote.BackoffPolicy{Initial: time.Millisecond, Max: 10 * time.Millisecond}
		return remote.New(opts)
	}
	serveAddr := strings.TrimPrefix(serve.URL, "http://")
	mgr.ensureServe = func(_ context.Context, _ bootstrap.Conn, opts bootstrap.Options) (bootstrap.Result, error) {
		return bootstrap.Result{
			State:  bootstrap.ServeState{Addr: serveAddr, Workspace: opts.Workspace},
			Token:  "reconnect-token",
			Reused: true,
		}, nil
	}

	launches := make(chan remoteWindowLaunch, 4)
	a.remoteWindowOpener = func(launch remoteWindowLaunch) error {
		launches <- launch
		return nil
	}
	window := spawnRemoteWindowHelper(t)
	t.Cleanup(func() {
		_ = mgr.Disconnect(hostID)
		_ = window.Kill()
		waitRemoteWindowHelperExit(t, window)
	})

	if err := mgr.Connect(hostID); err != nil {
		t.Fatal(err)
	}
	waitForRemoteWindowStatus(t, sink.statuses, "connected", 0)
	a.remoteWindows.record(remoteWindowHostKey(hostID), window)
	if err := a.OpenRemoteWorkspace(hostID, "/srv/project"); err != nil {
		t.Fatal(err)
	}
	first := waitForRemoteWindowLaunch(t, launches)
	assertRemoteServeReachable(t, first.URL)

	sshServer.DropConnections()
	waitForRemoteWindowStatus(t, sink.statuses, "reconnecting", 1)
	waitForRemoteWindowStatus(t, sink.statuses, "connected", 1)
	refreshed := waitForRemoteWindowLaunch(t, launches)
	if refreshed.URL != first.URL {
		t.Fatalf("reconnected window URL = %q, want persistent forward URL %q", refreshed.URL, first.URL)
	}
	assertRemoteServeReachable(t, refreshed.URL)
}

func waitForRemoteWindowStatus(t *testing.T, statuses <-chan RemoteConnectionStatusView, state string, minAttempt int) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case status := <-statuses:
			if status.State == state && status.Attempt >= minAttempt {
				return
			}
		case <-timer.C:
			t.Fatalf("remote status did not reach %s at attempt >= %d", state, minAttempt)
		}
	}
}

func waitForRemoteWindowLaunch(t *testing.T, launches <-chan remoteWindowLaunch) remoteWindowLaunch {
	t.Helper()
	select {
	case launch := <-launches:
		return launch
	case <-time.After(10 * time.Second):
		t.Fatal("remote window was not opened")
		return remoteWindowLaunch{}
	}
}

func assertRemoteServeReachable(t *testing.T, rawURL string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET remote Serve through SSH forward: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remote Serve status = %d, want 200", resp.StatusCode)
	}
}

// TestRemoteWindowDisconnectClosesLiveWindowAfterHandoff is the real
// single-instance sequence: the live window stays registered while a
// short-lived handoff process (spawned by re-opening the host) exits at the
// gate. An explicit disconnect must still close the live window.
func TestRemoteWindowDisconnectClosesLiveWindowAfterHandoff(t *testing.T) {
	fake := &fakeRemoteKernel{}
	a := NewApp()
	a.remoteRuntime = fake
	key := remoteWindowHostKey("box")

	live := spawnRemoteWindowHelper(t)
	a.remoteWindows.record(key, live)
	handoff := spawnRemoteWindowHelper(t)
	defer func() { _ = handoff.Kill(); waitRemoteWindowHelperExit(t, handoff) }()
	handoffGen := a.remoteWindows.record(key, handoff)
	a.remoteWindows.clearIf(key, handoffGen, handoff.Pid)
	if !a.hasRemoteWindow("box") {
		t.Fatal("live window registration lost after handoff exit")
	}

	if err := a.DisconnectRemoteHost("box"); err != nil {
		t.Fatal(err)
	}
	// Disconnect kills the live window; its Wait clears the registration.
	waitRemoteWindowHelperExit(t, live)
	if a.hasRemoteWindow("box") {
		t.Fatal("live window survived explicit disconnect after handoff")
	}
}

// TestOpenRemoteWorkspaceConcurrentDisconnectClosesLateWindow forces an
// explicit disconnect to begin while the child window opener is still in
// flight. The per-host lifecycle must let the opener finish registration first
// and then close that exact process; otherwise disconnect can miss the late
// registration and leave a window pointing at a dead tunnel.
func TestOpenRemoteWorkspaceConcurrentDisconnectClosesLateWindow(t *testing.T) {
	fake := &fakeRemoteKernel{
		ensureView:  RemoteServerView{HostID: "box", Workspace: "/srv", State: "ready", LocalURL: "http://127.0.0.1:54321/"},
		ensureToken: "tok-concurrent",
	}
	a := NewApp()
	a.remoteRuntime = fake
	key := remoteWindowHostKey("box")
	live := spawnRemoteWindowHelper(t)
	waited := false
	defer func() {
		if !waited {
			_ = live.Kill()
			waitRemoteWindowHelperExit(t, live)
		}
	}()

	openerEntered := make(chan struct{})
	releaseOpener := make(chan struct{})
	a.remoteWindowOpener = func(remoteWindowLaunch) error {
		close(openerEntered)
		<-releaseOpener
		a.remoteWindows.record(key, live)
		return nil
	}

	openDone := make(chan error, 1)
	go func() { openDone <- a.OpenRemoteWorkspace("box", "/srv") }()
	select {
	case <-openerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("remote window opener did not start")
	}

	value, ok := a.remoteWindowLifecycles.hosts.Load(key)
	if !ok {
		t.Fatal("host lifecycle was not registered")
	}
	hostLifecycle := value.(*remoteWindowHostLifecycle)
	openGeneration := hostLifecycle.generation.Load()
	disconnectDone := make(chan error, 1)
	go func() { disconnectDone <- a.DisconnectRemoteHost("box") }()
	deadline := time.Now().Add(5 * time.Second)
	for hostLifecycle.generation.Load() == openGeneration {
		if time.Now().After(deadline) {
			t.Fatal("disconnect did not enter the host lifecycle")
		}
		runtime.Gosched()
	}

	close(releaseOpener)
	select {
	case err := <-openDone:
		if err != nil {
			t.Fatalf("OpenRemoteWorkspace: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OpenRemoteWorkspace did not finish")
	}
	select {
	case err := <-disconnectDone:
		if err != nil {
			t.Fatalf("DisconnectRemoteHost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DisconnectRemoteHost did not finish")
	}

	waitRemoteWindowHelperExit(t, live)
	waited = true
	if a.hasRemoteWindow("box") {
		t.Fatal("late window registration survived concurrent disconnect")
	}
}

// TestOpenRemoteWorkspaceWindowOpenFailureKeepsServeReady covers a successful
// serve/tunnel followed by a failed window open without losing ready state.
func TestOpenRemoteWorkspaceWindowOpenFailureKeepsServeReady(t *testing.T) {
	const hostID = "open-fail-box"
	fake := &fakeRemoteKernel{
		ensureView:  RemoteServerView{HostID: hostID, Workspace: "/srv2", State: "ready", LocalURL: "http://127.0.0.1:6666/"},
		ensureToken: "tok-fail",
	}
	a := NewApp()
	a.remoteRuntime = fake
	a.remoteWindowOpener = func(remoteWindowLaunch) error {
		return errors.New("window spawn failed")
	}
	err := a.OpenRemoteWorkspace(hostID, "/srv2")
	if err == nil || !strings.Contains(err.Error(), "window spawn failed") {
		t.Fatalf("open error = %v, want the opener failure", err)
	}
	// The recorded workspace stays aligned with the ready serve for reuse.
	status, _ := a.RemoteServerStatus(hostID, "/srv2")
	if status.State != "ready" || status.Workspace != "/srv2" {
		t.Fatalf("serve state after failed open = %+v, want ready /srv2", status)
	}
	if got := a.RemoteLastWorkspace(hostID); got != "/srv2" {
		t.Fatalf("last workspace = %q, want /srv2 (the running serve)", got)
	}
}

func TestRemoteWindowDisconnectAndStopCloseWindow(t *testing.T) {
	fake := &fakeRemoteKernel{}
	a := NewApp()
	a.remoteRuntime = fake
	verify := func(err error, wantOpen bool) {
		if err != nil {
			t.Fatal(err)
		}
		if got := a.hasRemoteWindow("box"); got != wantOpen {
			t.Fatalf("window open = %v, want %v", got, wantOpen)
		}
	}
	p := spawnRemoteWindowHelper(t)
	defer waitRemoteWindowHelperExit(t, p)
	a.remoteWindows.record(remoteWindowHostKey("box"), p)
	err := a.DisconnectRemoteHost("box")
	waitRemoteWindowHelperExit(t, p)
	verify(err, false)
	p2 := spawnRemoteWindowHelper(t)
	defer waitRemoteWindowHelperExit(t, p2)
	a.remoteWindows.record(remoteWindowHostKey("box"), p2)
	a.remoteWindows.setWorkspace(remoteWindowHostKey("box"), "/srv")
	verify(a.StopRemoteServer("box", "/other"), true)
	err = a.StopRemoteServer("box", "/srv")
	waitRemoteWindowHelperExit(t, p2)
	verify(err, false)
	if got := fake.stoppedWorkspaces; len(got) != 2 || got[0] != "/other" || got[1] != "/srv" {
		t.Fatalf("StopRemoteServer forwarded %v, want [/other /srv]", got)
	}
}
