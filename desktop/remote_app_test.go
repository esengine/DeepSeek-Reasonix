package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/remote"
)

// fakeRemoteKernel implements remoteKernel for binding-layer tests.
type fakeRemoteKernel struct {
	hosts        []RemoteHostView
	statuses     []RemoteConnectionStatusView
	writeResult  RemoteWriteResult
	ensureView   RemoteServerView
	ensureToken  string
	ensureErr    error
	resolveCalls []bool
	closed       bool
}

func TestRemoteConnectionErrorDetailsPreserveHostKeyMismatch(t *testing.T) {
	root := &remote.HostKeyMismatchError{
		Host:                 "dev@example.test:2222",
		PresentedFingerprint: "SHA256:new",
		Locations: []remote.KnownHostLocation{
			{Filename: "/home/dev/.ssh/known_hosts", Line: 7},
		},
	}
	view := RemoteConnectionStatusView{HostID: "box", State: "stopped"}
	applyRemoteConnectionError(&view, errors.Join(errors.New("ssh handshake failed"), root))

	if view.ErrorDetails == nil || view.ErrorDetails.Code != "host_key_mismatch" {
		t.Fatalf("error details = %+v", view.ErrorDetails)
	}
	if view.ErrorDetails.PresentedSHA256 != "SHA256:new" {
		t.Fatalf("presented fingerprint = %q", view.ErrorDetails.PresentedSHA256)
	}
	if got := view.ErrorDetails.KnownHostRecords; len(got) != 1 || got[0].Path != "/home/dev/.ssh/known_hosts" || got[0].Line != 7 {
		t.Fatalf("known_hosts records = %+v", got)
	}
}

func TestRemoteConnectionErrorDetailsPreserveDegradedState(t *testing.T) {
	view := RemoteConnectionStatusView{HostID: "box", State: "degraded"}
	applyRemoteConnectionError(&view, errors.New("forward attach failed"))

	if view.ErrorDetails != nil {
		t.Fatalf("degraded error must not be classified as a connection failure: %+v", view.ErrorDetails)
	}
	if view.Error != "forward attach failed" {
		t.Fatalf("raw error = %q", view.Error)
	}
}

func (f *fakeRemoteKernel) Hosts() ([]RemoteHostView, error) { return f.hosts, nil }
func (f *fakeRemoteKernel) AddHost(in RemoteHostInput) (RemoteHostView, error) {
	v := RemoteHostView{ID: in.Label, Label: in.Label, Host: in.Host}
	f.hosts = append(f.hosts, v)
	return v, nil
}
func (f *fakeRemoteKernel) UpdateHost(id string, in RemoteHostInput) (RemoteHostView, error) {
	return RemoteHostView{ID: id, Host: in.Host}, nil
}
func (f *fakeRemoteKernel) RemoveHost(id string) error                { return nil }
func (f *fakeRemoteKernel) ScanSSHConfig() ([]RemoteHostInput, error) { return nil, nil }
func (f *fakeRemoteKernel) Connect(hostID string) error               { return nil }
func (f *fakeRemoteKernel) Disconnect(hostID string) error            { return nil }
func (f *fakeRemoteKernel) Statuses() []RemoteConnectionStatusView    { return f.statuses }
func (f *fakeRemoteKernel) ResolveHostKey(hostID string, accept bool) error {
	f.resolveCalls = append(f.resolveCalls, accept)
	return nil
}
func (f *fakeRemoteKernel) ListDir(context.Context, string, string) ([]RemoteDirEntry, error) {
	return []RemoteDirEntry{{Name: "file.txt"}}, nil
}
func (f *fakeRemoteKernel) ReadFile(context.Context, string, string) (RemoteFilePreview, error) {
	return RemoteFilePreview{Body: "hi"}, nil
}
func (f *fakeRemoteKernel) WriteFile(context.Context, string, string, string, int64) (RemoteWriteResult, error) {
	return f.writeResult, nil
}
func (f *fakeRemoteKernel) Mkdir(context.Context, string, string) error          { return nil }
func (f *fakeRemoteKernel) Rename(context.Context, string, string, string) error { return nil }
func (f *fakeRemoteKernel) Delete(context.Context, string, string, bool) error   { return nil }
func (f *fakeRemoteKernel) Forwards(string) []RemoteForwardView                  { return nil }
func (f *fakeRemoteKernel) AddForward(string, RemoteForwardInput) (RemoteForwardView, error) {
	return RemoteForwardView{}, nil
}
func (f *fakeRemoteKernel) RemoveForward(string, string) error { return nil }
func (f *fakeRemoteKernel) EnsureServer(context.Context, string, string) (RemoteServerView, string, error) {
	return f.ensureView, f.ensureToken, f.ensureErr
}
func (f *fakeRemoteKernel) StopServer(string) error              { return nil }
func (f *fakeRemoteKernel) ServerStatus(string) RemoteServerView { return f.ensureView }
func (f *fakeRemoteKernel) ServerLogs(context.Context, string, int) (string, error) {
	return "log line", nil
}
func (f *fakeRemoteKernel) Close() error { f.closed = true; return nil }

func appWithFakeKernel(fake *fakeRemoteKernel) *App {
	a := &App{ctx: context.Background()}
	a.remoteRuntime = fake
	return a
}

func TestRemoteBindingsDelegateToKernel(t *testing.T) {
	fake := &fakeRemoteKernel{writeResult: RemoteWriteResult{OK: true, NewMtimeUnix: 42}}
	a := appWithFakeKernel(fake)

	if _, err := a.AddRemoteHost(RemoteHostInput{Label: "box", Host: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	hosts, _ := a.RemoteHosts()
	if len(hosts) != 1 || hosts[0].ID != "box" {
		t.Fatalf("hosts = %+v", hosts)
	}
	entries, err := a.ListRemoteDir("box", "/")
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListRemoteDir = %+v, %v", entries, err)
	}
	res, err := a.WriteRemoteFile("box", "/f", "data", 0)
	if err != nil || !res.OK || res.NewMtimeUnix != 42 {
		t.Fatalf("WriteRemoteFile = %+v, %v", res, err)
	}
}

func TestConfirmRemoteHostKeyDelegates(t *testing.T) {
	fake := &fakeRemoteKernel{}
	a := appWithFakeKernel(fake)
	if err := a.ConfirmRemoteHostKey("box", true); err != nil {
		t.Fatal(err)
	}
	if len(fake.resolveCalls) != 1 || fake.resolveCalls[0] != true {
		t.Fatalf("resolve calls = %+v", fake.resolveCalls)
	}
}

// TestRemoteStatusBridgesToAsyncEmitter verifies a kernel status callback lands
// on the async emitter as a remote:status event.
func TestRemoteStatusBridgesToAsyncEmitter(t *testing.T) {
	a := &App{ctx: context.Background()}
	events := make(chan runtimeEventEnvelope, 4)
	a.runtimeEvents.emit = func(ctx context.Context, name string, payload ...interface{}) {
		events <- runtimeEventEnvelope{ctx: ctx, name: name, payload: payload}
	}
	a.onStatus(RemoteConnectionStatusView{HostID: "box", State: "connected"})

	// The async emitter delivers on a background goroutine, so block briefly.
	select {
	case ev := <-events:
		if ev.name != "remote:status" {
			t.Fatalf("event name = %q, want remote:status", ev.name)
		}
		s, ok := ev.payload[0].(RemoteConnectionStatusView)
		if !ok || s.HostID != "box" || s.State != "connected" {
			t.Fatalf("payload = %+v", ev.payload[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no remote:status event emitted")
	}
}

func TestStopRemoteRuntimeClosesKernel(t *testing.T) {
	fake := &fakeRemoteKernel{}
	a := appWithFakeKernel(fake)
	a.stopRemoteRuntime()
	if !fake.closed {
		t.Fatal("kernel not closed on stopRemoteRuntime")
	}
	if a.remoteRuntime != nil {
		t.Fatal("remoteRuntime not cleared")
	}
}

// TestUpdateHostPreservesHiddenFields pins the data-loss fix: editing a host in
// the desktop UI (whose input lacks passphrase_env/password_env/forwards) must
// not wipe those stored fields.
func TestUpdateHostPreservesHiddenFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)

	mgr := newDesktopRemoteManager(&App{})
	// Seed a host with credential refs + a forward via the kernel config API.
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "box", Host: "10.0.0.9", User: "dev",
			PassphraseEnv: "REMOTE_BOX_PASSPHRASE",
			PasswordEnv:   "REMOTE_BOX_PASSWORD",
			Forwards:      []config.RemoteForwardEntry{{Type: "local", Bind: "127.0.0.1:8080", Target: "127.0.0.1:80"}},
		})
	}); err != nil {
		t.Fatal(err)
	}

	// Edit via the desktop input (no hidden fields), changing only the user.
	if _, err := mgr.UpdateHost("box", RemoteHostInput{Label: "box", Host: "10.0.0.9", Port: 22, User: "ops", ServeInstall: "auto"}); err != nil {
		t.Fatalf("UpdateHost: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	h, ok := cfg.RemoteHost("box")
	if !ok {
		t.Fatal("host missing after edit")
	}
	if h.User != "ops" {
		t.Fatalf("edit did not apply: user=%q", h.User)
	}
	if h.PassphraseEnv != "REMOTE_BOX_PASSPHRASE" || h.PasswordEnv != "REMOTE_BOX_PASSWORD" {
		t.Fatalf("edit wiped credential env refs: %+v", h)
	}
	if len(h.Forwards) != 1 || h.Forwards[0].Bind != "127.0.0.1:8080" {
		t.Fatalf("edit wiped persisted forwards: %+v", h.Forwards)
	}
}

// TestScanSSHConfigReturnsNonNil pins the JSON-contract fix: an empty scan must
// encode as [] (not null), which the React import page iterates safely.
func TestScanSSHConfigReturnsNonNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home) // no ~/.ssh/config here => empty result
	mgr := newDesktopRemoteManager(&App{})
	out, err := mgr.ScanSSHConfig()
	if err != nil {
		t.Fatalf("ScanSSHConfig: %v", err)
	}
	if out == nil {
		t.Fatal("ScanSSHConfig returned nil slice (would encode as JSON null and crash the import page)")
	}
}

func TestOpenRemoteWorkspacePersistsLastWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	fake := &fakeRemoteKernel{
		ensureView:  RemoteServerView{State: "ready", LocalURL: "http://127.0.0.1:5000/"},
		ensureToken: "tok",
	}
	a := appWithFakeKernel(fake)
	var opened remoteWindowLaunch
	a.remoteWindowOpener = func(launch remoteWindowLaunch) error {
		opened = launch
		return nil
	}
	if err := a.OpenRemoteWorkspace("box", "/home/dev/app"); err != nil {
		t.Fatal(err)
	}
	if opened.URL != "http://127.0.0.1:5000?token=tok" {
		t.Fatalf("opened URL = %q", opened.URL)
	}
	if opened.Title != "Reasonix [SSH: box]" {
		t.Fatalf("opened title = %q", opened.Title)
	}

	got := a.RemoteLastWorkspace("box")
	if got != "/home/dev/app" {
		t.Fatalf("last workspace = %q, want /home/dev/app", got)
	}
	// desktop-remote.json exists.
	if _, err := os.Stat(filepath.Join(config.MemoryUserDir(), "desktop-remote.json")); err != nil {
		t.Fatalf("desktop-remote.json not written: %v", err)
	}
}
