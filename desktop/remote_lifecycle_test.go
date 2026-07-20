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
		server: RemoteServerView{HostID: "box", Workspace: "/work", State: "ready"},
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
		_, err := mgr.ServerLogs(context.Background(), "box", 20)
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
		server: RemoteServerView{HostID: "box", Workspace: "/new", State: "ready"}, token: "new-token",
	}
	mgr.mu.Lock()
	mgr.hosts["box"] = replacement
	mgr.mu.Unlock()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale EnsureServer unexpectedly succeeded")
	}
	if got := mgr.ServerStatus("box"); got.Workspace != "/new" || got.State != "ready" {
		t.Fatalf("replacement server state was overwritten: %+v", got)
	}
	if replacement.token != "new-token" {
		t.Fatalf("replacement token = %q, want new-token", replacement.token)
	}
}

func TestStopServerRejectsEmptyWorkspace(t *testing.T) {
	mgr := newDesktopRemoteManager(nil)
	hostCtx, hostCancel := context.WithCancel(context.Background())
	defer hostCancel()
	mgr.hosts["box"] = &managedHost{ctx: hostCtx, cancel: hostCancel, client: newLifecycleSSHClient(nil)}
	called := false
	mgr.stopServe = func(context.Context, bootstrap.Conn, string) error { called = true; return nil }
	if err := mgr.StopServer("box"); err == nil {
		t.Fatal("StopServer accepted an empty workspace")
	}
	if called {
		t.Fatal("StopServer called bootstrap.Stop with an empty workspace")
	}
}

func TestDesktopCLIBinaryPathFallsBackToPATH(t *testing.T) {
	dir := t.TempDir()
	name := "reasonix"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cli := filepath.Join(dir, name)
	if err := os.WriteFile(cli, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got := desktopCLIBinaryPath(); got != cli {
		t.Fatalf("desktopCLIBinaryPath = %q, want %q", got, cli)
	}
}

func TestHasUsableServeForwardRequiresExactTargetAndURL(t *testing.T) {
	entries := []forward.Entry{{
		Spec: forward.Spec{Name: serveForwardName, TargetAddr: "127.0.0.1:9000"},
		Up:   true, BoundAddr: "127.0.0.1:45000",
	}}
	if !hasUsableServeForward(entries, "127.0.0.1:9000", "http://127.0.0.1:45000/") {
		t.Fatal("exact existing serve forward was not reusable")
	}
	if hasUsableServeForward(entries, "127.0.0.1:9001", "http://127.0.0.1:45000/") {
		t.Fatal("stale serve target was reused")
	}
	if hasUsableServeForward(entries, "127.0.0.1:9000", "http://127.0.0.1:45001/") {
		t.Fatal("mismatched local URL was reused")
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
	defer cancel()
	for _, hostID := range []string{"a", "b"} {
		mh := &managedHost{ctx: ctx, cancel: cancel, client: newLifecycleSSHClient(nil)}
		mgr.hosts[hostID] = mh
		prompt := mgr.hostKeyPrompt(hostID, mh)
		go func() {
			_, _ = prompt(ctx, remote.HostKeyQuestion{Address: hostID + ":22", KeyType: "ssh-ed25519", Fingerprint: hostID})
		}()
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
