package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/remote"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/remote/forward"
	"reasonix/internal/remote/sftpfs"
	"reasonix/internal/remote/sshtest"

	"golang.org/x/crypto/ssh"
)

type lifecycleSSHClient struct {
	mu       sync.Mutex
	startErr error
	closed   bool
	sub      func(remote.StatusEvent)
	forwards *forward.Set
}

type lifecycleEventSink struct {
	statuses chan RemoteConnectionStatusView
}

func (s *lifecycleEventSink) onStatus(v RemoteConnectionStatusView) { s.statuses <- v }
func (*lifecycleEventSink) onForwards(string, []RemoteForwardView)  {}
func (*lifecycleEventSink) onServer(RemoteServerView)               {}

func newLifecycleSSHClient(startErr error) *lifecycleSSHClient {
	return &lifecycleSSHClient{startErr: startErr, forwards: forward.NewSet(nil)}
}

func TestDesktopSecretPromptPublishesMetadataAndReturnsOneShotSecret(t *testing.T) {
	sink := &lifecycleEventSink{statuses: make(chan RemoteConnectionStatusView, 2)}
	mgr := newDesktopRemoteManager(sink)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	generation := &managedHost{ctx: ctx, cancel: cancel, status: RemoteConnectionStatusView{HostID: "box", State: "connecting"}}
	mgr.hosts["box"] = generation

	type promptResult struct {
		secret string
		err    error
	}
	result := make(chan promptResult, 1)
	go func() {
		secret, err := mgr.secretPrompt("box", generation)(ctx, remote.SecretPassword, "dev@box.test", "")
		result <- promptResult{secret: secret, err: err}
	}()

	var promptID string
	select {
	case status := <-sink.statuses:
		if status.State != "pending_secret" || status.SecretPrompt == nil {
			t.Fatalf("status = %+v", status)
		}
		if status.SecretPrompt.Host != "dev@box.test" || status.SecretPrompt.Kind != "password" {
			t.Fatalf("prompt metadata = %+v", status.SecretPrompt)
		}
		promptID = status.SecretPrompt.PromptID
		if promptID == "" {
			t.Fatal("prompt ID was empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("secret prompt status was not emitted")
	}

	if err := mgr.ResolveSecret("box", "stale-prompt", "wrong-secret", true); err == nil {
		t.Fatal("stale prompt ID resolved the active credential request")
	}
	if err := mgr.ResolveSecret("box", promptID, "one-shot-secret", true); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-result:
		if got.err != nil || got.secret != "one-shot-secret" {
			t.Fatalf("prompt result = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("secret prompt did not resolve")
	}
}

func (c *lifecycleSSHClient) Start(context.Context) error {
	c.mu.Lock()
	sub, err := c.sub, c.startErr
	c.mu.Unlock()
	if sub != nil {
		if err != nil {
			sub(remote.StatusEvent{Status: remote.StatusStopped, Err: err})
		} else {
			sub(remote.StatusEvent{Status: remote.StatusConnected})
		}
	}
	return err
}

func (c *lifecycleSSHClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	c.forwards.Close()
	return nil
}

func (c *lifecycleSSHClient) Subscribe(fn func(remote.StatusEvent)) func() {
	c.mu.Lock()
	c.sub = fn
	c.mu.Unlock()
	fn(remote.StatusEvent{Status: remote.StatusIdle})
	return func() {}
}

func (c *lifecycleSSHClient) Forwards() *forward.Set { return c.forwards }
func (c *lifecycleSSHClient) Exec(context.Context, string) (remote.ExecResult, error) {
	return remote.ExecResult{}, nil
}
func (c *lifecycleSSHClient) SFTP() (*sftpfs.FS, error) { return nil, errors.New("unused") }

func seedLifecycleHost(t *testing.T, hostID string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: hostID, Host: "127.0.0.1", Port: 22, User: "tester"})
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConnectCanReplaceStoppedGeneration(t *testing.T) {
	seedLifecycleHost(t, "box")
	mgr := newDesktopRemoteManager(nil)
	first := newLifecycleSSHClient(errors.New("first dial failed"))
	second := newLifecycleSSHClient(nil)
	var calls int
	mgr.newClient = func(remote.Options) (desktopSSHClient, error) {
		calls++
		if calls == 1 {
			return first, nil
		}
		return second, nil
	}

	if err := mgr.Connect("box"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		statuses := mgr.Statuses()
		if len(statuses) == 1 && statuses[0].State == "stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first generation did not stop: %+v", statuses)
		}
		time.Sleep(time.Millisecond)
	}
	if err := mgr.Connect("box"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("newClient calls = %d, want 2", calls)
	}
	first.mu.Lock()
	firstClosed := first.closed
	first.mu.Unlock()
	if !firstClosed {
		t.Fatal("replaced stopped client was not closed")
	}
}

func TestStaleClientStatusCannotOverwriteReplacement(t *testing.T) {
	mgr := newDesktopRemoteManager(nil)
	oldCtx, oldCancel := context.WithCancel(context.Background())
	defer oldCancel()
	newCtx, newCancel := context.WithCancel(context.Background())
	defer newCancel()
	old := &managedHost{ctx: oldCtx, cancel: oldCancel, client: newLifecycleSSHClient(nil)}
	current := &managedHost{
		ctx: newCtx, cancel: newCancel, client: newLifecycleSSHClient(nil),
		status: RemoteConnectionStatusView{HostID: "box", State: "connected"},
	}
	mgr.hosts["box"] = current
	mgr.onClientStatus("box", old, remote.StatusEvent{Status: remote.StatusStopped, Err: errors.New("late")})
	if got := mgr.Statuses()[0]; got.State != "connected" || got.Error != "" {
		t.Fatalf("replacement status was overwritten: %+v", got)
	}
}

func TestServerLogsCancellationOnDisconnect(t *testing.T) {
	sink := &lifecycleEventSink{statuses: make(chan RemoteConnectionStatusView, 1)}
	mgr := newDesktopRemoteManager(sink)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	mh := &managedHost{
		ctx: hostCtx, cancel: hostCancel, client: newLifecycleSSHClient(nil),
		serves: map[string]*serveEntry{"/work": {view: RemoteServerView{HostID: "box", Workspace: "/work", State: "ready"}}},
	}
	mgr.hosts["box"] = mh
	entered := make(chan struct{})
	mgr.serveLogs = func(ctx context.Context, _ bootstrap.Conn, _ string, _ int, _ *strings.Builder) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := mgr.ServerLogs(context.Background(), "box", "/work", 20)
		done <- err
	}()
	<-entered
	if err := mgr.Disconnect("box"); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-sink.statuses:
		if status.HostID != "box" || status.State != "stopped" {
			t.Fatalf("Disconnect status = %+v", status)
		}
	default:
		t.Fatal("Disconnect did not publish a stopped status")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ServerLogs error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServerLogs was not canceled by Disconnect")
	}
}

func TestEnsureServerResultCannotMutateReplacement(t *testing.T) {
	seedLifecycleHost(t, "box")
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	old := &managedHost{ctx: hostCtx, cancel: hostCancel, client: newLifecycleSSHClient(nil)}
	mgr.hosts["box"] = old
	entered := make(chan struct{})
	release := make(chan struct{})
	mgr.ensureServe = func(context.Context, bootstrap.Conn, bootstrap.Options) (bootstrap.Result, error) {
		close(entered)
		<-release
		return bootstrap.Result{State: bootstrap.ServeState{Addr: "127.0.0.1:9999"}, Token: "old-token"}, nil
	}
	mgr.localBinary = func() string { return "" }
	done := make(chan error, 1)
	go func() {
		_, _, err := mgr.EnsureServer(context.Background(), "box", "/old")
		done <- err
	}()
	<-entered
	if err := mgr.Disconnect("box"); err != nil {
		t.Fatal(err)
	}
	newCtx, newCancel := context.WithCancel(context.Background())
	defer newCancel()
	replacement := &managedHost{
		ctx: newCtx, cancel: newCancel, client: newLifecycleSSHClient(nil),
		serves: map[string]*serveEntry{
			"/new": {view: RemoteServerView{HostID: "box", Workspace: "/new", State: "ready"}, token: "new-token"},
		},
	}
	mgr.mu.Lock()
	mgr.hosts["box"] = replacement
	mgr.mu.Unlock()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale EnsureServer unexpectedly succeeded")
	}
	if got := mgr.ServerStatus("box", "/new"); got.Workspace != "/new" || got.State != "ready" {
		t.Fatalf("replacement server state was overwritten: %+v", got)
	}
	if got := replacement.serves["/new"].token; got != "new-token" {
		t.Fatalf("replacement token = %q, want new-token", got)
	}
}

func TestStopServerRejectsUnknownWorkspace(t *testing.T) {
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	mgr.hosts["box"] = &managedHost{
		ctx: hostCtx, cancel: hostCancel, client: newLifecycleSSHClient(nil),
		serves: map[string]*serveEntry{"/srv/a": {view: RemoteServerView{HostID: "box", Workspace: "/srv/a", State: "ready"}}},
	}
	called := false
	mgr.stopServe = func(context.Context, bootstrap.Conn, string) error { called = true; return nil }
	if err := mgr.StopServer("box", "/srv/missing"); err == nil {
		t.Fatal("StopServer accepted an unknown workspace")
	}
	if called {
		t.Fatal("StopServer called bootstrap.Stop for an untracked workspace")
	}
}

func TestDesktopCLIBinaryPathFallsBackToPATH(t *testing.T) {
	dir := t.TempDir()
	_, name := desktopCLIBinaryNames(runtime.GOOS)
	cli := filepath.Join(dir, name)
	if err := os.WriteFile(cli, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got := desktopCLIBinaryPath(); got != cli {
		t.Fatalf("desktopCLIBinaryPath = %q, want %q", got, cli)
	}
}

func TestDesktopCLIBinaryNamesAvoidWindowsPortableEntryCollision(t *testing.T) {
	packaged, command := desktopCLIBinaryNames("windows")
	if packaged != "reasonix-cli.exe" || command != "reasonix.exe" {
		t.Fatalf("Windows CLI names = (%q, %q)", packaged, command)
	}
	if strings.EqualFold(packaged, "Reasonix.exe") {
		t.Fatalf("packaged CLI %q collides with the desktop entry point", packaged)
	}
	if packaged, command := desktopCLIBinaryNames("linux"); packaged != "reasonix" || command != "reasonix" {
		t.Fatalf("Linux CLI names = (%q, %q)", packaged, command)
	}
}

func TestHasUsableServeForwardRequiresExactTargetAndURL(t *testing.T) {
	entries := []forward.Entry{{
		Spec: forward.Spec{Name: serveForwardName("/srv/a"), TargetAddr: "127.0.0.1:9000"},
		Up:   true, BoundAddr: "127.0.0.1:45000",
	}}
	if !hasUsableServeForward(entries, serveForwardName("/srv/a"), "127.0.0.1:9000", "http://127.0.0.1:45000/") {
		t.Fatal("exact existing serve forward was not reusable")
	}
	if hasUsableServeForward(entries, serveForwardName("/srv/a"), "127.0.0.1:9001", "http://127.0.0.1:45000/") {
		t.Fatal("stale serve target was reused")
	}
	if hasUsableServeForward(entries, serveForwardName("/srv/a"), "127.0.0.1:9000", "http://127.0.0.1:45001/") {
		t.Fatal("mismatched local URL was reused")
	}
	if hasUsableServeForward(entries, serveForwardName("/other"), "127.0.0.1:9000", "http://127.0.0.1:45000/") {
		t.Fatal("another workspace's forward was reused")
	}
}

func TestDesktopNormalizeBind(t *testing.T) {
	if got := desktopNormalizeBind("8080"); got != "127.0.0.1:8080" {
		t.Fatalf("desktopNormalizeBind bare port = %q", got)
	}
	if got := desktopNormalizeBind("0.0.0.0:8080"); got != "0.0.0.0:8080" {
		t.Fatalf("desktopNormalizeBind address = %q", got)
	}
}

func TestHostKeyPromptsAreSerializedForGlobalDialog(t *testing.T) {
	sink := &lifecycleEventSink{statuses: make(chan RemoteConnectionStatusView, 2)}
	mgr := newDesktopRemoteManager(sink)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	type pendingPrompt struct {
		hostID string
		prompt remote.HostKeyPrompt
	}
	prompts := make([]pendingPrompt, 0, 2)
	for _, hostID := range []string{"a", "b"} {
		mh := &managedHost{ctx: ctx, cancel: cancel, client: newLifecycleSSHClient(nil)}
		mgr.hosts[hostID] = mh
		prompts = append(prompts, pendingPrompt{hostID: hostID, prompt: mgr.hostKeyPrompt(hostID, mh)})
	}
	for _, pending := range prompts {
		wg.Go(func() {
			_, _ = pending.prompt(ctx, remote.HostKeyQuestion{
				Address:     pending.hostID + ":22",
				KeyType:     "ssh-ed25519",
				Fingerprint: pending.hostID,
			})
		})
	}

	first := <-sink.statuses
	select {
	case second := <-sink.statuses:
		t.Fatalf("second prompt %q replaced unresolved prompt %q", second.HostID, first.HostID)
	case <-time.After(50 * time.Millisecond):
	}
	if err := mgr.ResolveHostKey(first.HostID, true); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-sink.statuses:
		if second.HostID == first.HostID {
			t.Fatalf("serialized prompt repeated host %q", second.HostID)
		}
		if err := mgr.ResolveHostKey(second.HostID, false); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second prompt did not appear after resolving the first")
	}
}

// TestEnsureServerFailureKeepsOwnershipOnPreviousReadyServe is the failed-
// start isolation contract: when a new workspace's Serve fails to start, the
// still-running previous workspace's entry stays untouched, so Stop and Logs
// keep operating on the workspace that actually runs.
func TestEnsureServerFailureKeepsOwnershipOnPreviousReadyServe(t *testing.T) {
	seedLifecycleHost(t, "box")
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	client := newLifecycleSSHClient(nil)
	mgr.hosts["box"] = &managedHost{
		ctx: hostCtx, cancel: hostCancel, client: client,
		serves: map[string]*serveEntry{
			"/srv/a": {view: RemoteServerView{HostID: "box", Workspace: "/srv/a", State: "ready", LocalURL: "http://127.0.0.1:54321/"}, token: "token-a"},
		},
	}
	mgr.ensureServe = func(context.Context, bootstrap.Conn, bootstrap.Options) (bootstrap.Result, error) {
		return bootstrap.Result{}, errors.New("serve launch failed")
	}
	var stopped, logged []string
	mgr.stopServe = func(_ context.Context, _ bootstrap.Conn, workspace string) error {
		stopped = append(stopped, workspace)
		return nil
	}
	mgr.serveLogs = func(_ context.Context, _ bootstrap.Conn, workspace string, _ int, _ *strings.Builder) error {
		logged = append(logged, workspace)
		return nil
	}

	if _, _, err := mgr.EnsureServer(context.Background(), "box", "/srv/b"); err == nil {
		t.Fatal("expected the serve launch failure")
	}
	status := mgr.ServerStatus("box", "/srv/a")
	if status.State != "ready" || status.Workspace != "/srv/a" {
		t.Fatalf("server state after failed start = %+v, want the previous ready /srv/a", status)
	}
	if got := mgr.hosts["box"].serves["/srv/a"].token; got != "token-a" {
		t.Fatalf("token after failed start = %q, want the previous token", got)
	}
	if status := mgr.ServerStatus("box", "/srv/b"); status.State != "error" {
		t.Fatalf("failed workspace state = %+v, want an error entry for /srv/b", status)
	}

	if err := mgr.StopServer("box", "/srv/a"); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "/srv/a" {
		t.Fatalf("StopServer operated on %v, want the previous /srv/a", stopped)
	}
	if _, err := mgr.ServerLogs(context.Background(), "box", "/srv/a", 50); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 || logged[0] != "/srv/a" {
		t.Fatalf("ServerLogs operated on %v, want the previous /srv/a", logged)
	}
}

// TestEnsureServerReplaceFailureKeepsOwnershipOnPreviousReadyServe covers the
// same contract when the new Serve started but its loopback tunnel could not
// be bound: the just-started Serve is stopped, ownership returns to the
// previous ready Serve, and the failed view never replaces it.
func TestEnsureServerReplaceFailureKeepsOwnershipOnPreviousReadyServe(t *testing.T) {
	seedLifecycleHost(t, "box")
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	client := newLifecycleSSHClient(nil)
	// A closed forward set makes the tunnel Replace fail deterministically;
	// the Set semantics keep any previous forward live on a failed Replace.
	closedForwards := forward.NewSet(nil)
	closedForwards.Close()
	client.forwards = closedForwards
	mgr.hosts["box"] = &managedHost{
		ctx: hostCtx, cancel: hostCancel, client: client,
		serves: map[string]*serveEntry{
			"/srv/a": {view: RemoteServerView{HostID: "box", Workspace: "/srv/a", State: "ready", LocalURL: "http://127.0.0.1:54321/"}, token: "token-a"},
		},
	}
	mgr.ensureServe = func(context.Context, bootstrap.Conn, bootstrap.Options) (bootstrap.Result, error) {
		return bootstrap.Result{State: bootstrap.ServeState{Addr: "127.0.0.1:9999"}}, nil
	}
	var stopped []string
	mgr.stopServe = func(_ context.Context, _ bootstrap.Conn, workspace string) error {
		stopped = append(stopped, workspace)
		return nil
	}

	if _, _, err := mgr.EnsureServer(context.Background(), "box", "/srv/b"); err == nil {
		t.Fatal("expected the tunnel Replace failure")
	}
	// The newly started /srv/b Serve was cleaned up, not left orphaned.
	if len(stopped) != 1 || stopped[0] != "/srv/b" {
		t.Fatalf("cleanup stopped %v, want the failed /srv/b", stopped)
	}
	status := mgr.ServerStatus("box", "/srv/a")
	if status.State != "ready" || status.Workspace != "/srv/a" {
		t.Fatalf("server state after failed tunnel bind = %+v, want the previous ready /srv/a", status)
	}
	if got := mgr.hosts["box"].serves["/srv/a"].token; got != "token-a" {
		t.Fatalf("token after failed tunnel bind = %q, want the previous token", got)
	}
}

// TestEnsureServerFirstStartFailurePublishesError keeps the informative error
// view when there is no previous ready Serve to preserve.
func TestEnsureServerFirstStartFailurePublishesError(t *testing.T) {
	seedLifecycleHost(t, "box")
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	mgr.hosts["box"] = &managedHost{ctx: hostCtx, cancel: hostCancel, client: newLifecycleSSHClient(nil)}
	mgr.ensureServe = func(context.Context, bootstrap.Conn, bootstrap.Options) (bootstrap.Result, error) {
		return bootstrap.Result{}, errors.New("serve launch failed")
	}
	if _, _, err := mgr.EnsureServer(context.Background(), "box", "/srv/b"); err == nil {
		t.Fatal("expected the serve launch failure")
	}
	status := mgr.ServerStatus("box", "/srv/b")
	if status.State != "error" || status.Workspace != "/srv/b" {
		t.Fatalf("server state after first-start failure = %+v, want error /srv/b", status)
	}
}

// platformProbeClient scripts the uname probe CheckPlatform runs.
type platformProbeClient struct {
	*lifecycleSSHClient
	unameOut string
	execErr  error
}

func (c *platformProbeClient) Exec(context.Context, string) (remote.ExecResult, error) {
	return remote.ExecResult{Stdout: []byte(c.unameOut)}, c.execErr
}

// TestCheckPlatformGatesUnsupportedOS applies the same ParseUname gate as
// EnsureServe at connect time: Linux/macOS pass, anything else fails with one
// clear message.
func TestCheckPlatformGatesUnsupportedOS(t *testing.T) {
	cases := []struct {
		name    string
		stdout  string
		execErr error
		wantErr string
	}{
		{name: "linux passes", stdout: "Linux x86_64\n"},
		{name: "darwin passes", stdout: "Darwin arm64\n"},
		{name: "mingw rejected", stdout: "MINGW64_NT-10.0-19045 x86_64\n", wantErr: "unsupported remote OS"},
		{name: "no uname rejected", stdout: "", wantErr: "cannot detect OS"},
		{name: "exec failure rejected", stdout: "Linux x86_64\n", execErr: errors.New("broken pipe"), wantErr: "cannot detect OS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := newDesktopRemoteManager(nil)
			hostCtx, hostCancel := context.WithCancel(context.Background())
			defer hostCancel()
			cl := &platformProbeClient{
				lifecycleSSHClient: newLifecycleSSHClient(nil),
				unameOut:           tc.stdout,
				execErr:            tc.execErr,
			}
			mgr.hosts["box"] = &managedHost{
				ctx: hostCtx, cancel: hostCancel, client: cl,
				status: RemoteConnectionStatusView{HostID: "box", State: "connected"},
			}
			err := mgr.CheckPlatform(context.Background(), "box")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestCheckPlatformRequiresConnection(t *testing.T) {
	mgr := newDesktopRemoteManager(nil)
	if err := mgr.CheckPlatform(context.Background(), "ghost"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("err = %v, want not connected", err)
	}
}

// multiServeEventSink records the per-workspace server views the kernel
// publishes while keeping the status channel behavior of lifecycleEventSink.
type multiServeEventSink struct {
	lifecycleEventSink
	mu      sync.Mutex
	servers []RemoteServerView
}

func (s *multiServeEventSink) onServer(v RemoteServerView) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.servers = append(s.servers, v)
}

func (s *multiServeEventSink) readyWorkspaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, v := range s.servers {
		if v.State == "ready" {
			out = append(out, v.Workspace)
		}
	}
	return out
}

// newAttachedForwardsClient returns a lifecycleSSHClient whose forward set is
// attached to a real sshtest-backed SSH client, so EnsureServer's tunnel
// Replace path can bind local listeners.
func newAttachedForwardsClient(t *testing.T) *lifecycleSSHClient {
	t.Helper()
	srv := sshtest.Start(t, sshtest.Options{})
	cfg := &ssh.ClientConfig{User: "t", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second}
	sshCl, err := ssh.Dial("tcp", srv.Addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sshCl.Close() })
	set := forward.NewSet(nil)
	set.Attach(sshCl)
	lc := newLifecycleSSHClient(nil)
	lc.forwards = set
	return lc
}

// TestEnsureServerTwoWorkspacesIndependentForwards: one host, two workspaces —
// two slug-named tunnels, two ready entries, per-workspace events.
func TestEnsureServerTwoWorkspacesIndependentForwards(t *testing.T) {
	seedLifecycleHost(t, "box")
	sink := &multiServeEventSink{lifecycleEventSink: lifecycleEventSink{statuses: make(chan RemoteConnectionStatusView, 8)}}
	mgr := newDesktopRemoteManager(sink)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	cl := newAttachedForwardsClient(t)
	mgr.hosts["box"] = &managedHost{ctx: hostCtx, cancel: hostCancel, client: cl, serves: map[string]*serveEntry{}}
	mgr.ensureServe = func(_ context.Context, _ bootstrap.Conn, opts bootstrap.Options) (bootstrap.Result, error) {
		addr := "127.0.0.1:9101"
		if opts.Workspace == "/srv/b" {
			addr = "127.0.0.1:9102"
		}
		return bootstrap.Result{State: bootstrap.ServeState{Addr: addr}, Token: "tok-" + opts.Workspace}, nil
	}
	mgr.localBinary = func() string { return "" }

	for _, ws := range []string{"/srv/a", "/srv/b"} {
		view, _, err := mgr.EnsureServer(context.Background(), "box", ws)
		if err != nil || view.State != "ready" || view.LocalURL == "" {
			t.Fatalf("EnsureServer(%s) = %+v, %v", ws, view, err)
		}
	}

	forwards := cl.Forwards().List()
	if len(forwards) != 2 {
		t.Fatalf("forward count = %d, want 2 (%+v)", len(forwards), forwards)
	}
	names := map[string]bool{}
	for _, f := range forwards {
		names[f.Spec.Name] = true
		if !f.Up {
			t.Fatalf("forward %q is not up", f.Spec.Name)
		}
	}
	if !names[serveForwardName("/srv/a")] || !names[serveForwardName("/srv/b")] {
		t.Fatalf("forward names = %v, want per-workspace serve-%s and serve-%s", names, serveForwardName("/srv/a"), serveForwardName("/srv/b"))
	}
	if serveForwardName("/srv/a") == serveForwardName("/srv/b") {
		t.Fatal("distinct workspaces share one forward name")
	}

	if got := mgr.ServerStatus("box", "/srv/a"); got.State != "ready" || got.Workspace != "/srv/a" {
		t.Fatalf("status A = %+v", got)
	}
	if got := mgr.ServerStatus("box", "/srv/b"); got.State != "ready" || got.Workspace != "/srv/b" {
		t.Fatalf("status B = %+v", got)
	}
	if got := mgr.ServerStatus("box", "/srv/c"); got.State != "stopped" {
		t.Fatalf("untracked workspace status = %+v, want stopped", got)
	}
	ready := sink.readyWorkspaces()
	if len(ready) != 2 {
		t.Fatalf("ready server events = %v, want one per workspace", ready)
	}
}

// TestStopServerOneWorkspaceKeepsOther: stopping A removes only A's tunnel and
// entry; B keeps serving.
func TestStopServerOneWorkspaceKeepsOther(t *testing.T) {
	seedLifecycleHost(t, "box")
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	cl := newAttachedForwardsClient(t)
	mgr.hosts["box"] = &managedHost{ctx: hostCtx, cancel: hostCancel, client: cl, serves: map[string]*serveEntry{}}
	mgr.ensureServe = func(_ context.Context, _ bootstrap.Conn, opts bootstrap.Options) (bootstrap.Result, error) {
		addr := "127.0.0.1:9111"
		if opts.Workspace == "/srv/b" {
			addr = "127.0.0.1:9112"
		}
		return bootstrap.Result{State: bootstrap.ServeState{Addr: addr}, Token: "tok"}, nil
	}
	var stopped []string
	mgr.stopServe = func(_ context.Context, _ bootstrap.Conn, workspace string) error {
		stopped = append(stopped, workspace)
		return nil
	}
	mgr.localBinary = func() string { return "" }
	for _, ws := range []string{"/srv/a", "/srv/b"} {
		if _, _, err := mgr.EnsureServer(context.Background(), "box", ws); err != nil {
			t.Fatal(err)
		}
	}

	if err := mgr.StopServer("box", "/srv/a"); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != "/srv/a" {
		t.Fatalf("stopServe operated on %v, want [/srv/a]", stopped)
	}
	forwards := cl.Forwards().List()
	if len(forwards) != 1 || forwards[0].Spec.Name != serveForwardName("/srv/b") {
		t.Fatalf("forwards after stop = %+v, want only /srv/b's", forwards)
	}
	if got := mgr.ServerStatus("box", "/srv/a"); got.State != "stopped" {
		t.Fatalf("status A after stop = %+v", got)
	}
	if got := mgr.ServerStatus("box", "/srv/b"); got.State != "ready" {
		t.Fatalf("status B after stopping A = %+v, want ready", got)
	}
}
