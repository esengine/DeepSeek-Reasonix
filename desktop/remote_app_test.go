package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/remote"
	"reasonix/internal/remote/workbench/target"
)

// fakeRemoteKernel implements remoteKernel for binding-layer tests.
type fakeRemoteKernel struct {
	hosts           []RemoteHostView
	statuses        []RemoteConnectionStatusView
	writeResult     RemoteWriteResult
	ensureView      RemoteServerView
	ensureToken     string
	ensureErr       error
	resolveCalls    []bool
	secretCalls     []remoteSecretAnswer
	secretPromptIDs []string
	closed          bool
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
func (f *fakeRemoteKernel) ResolveSecret(hostID, promptID, secret string, accept bool) error {
	f.secretPromptIDs = append(f.secretPromptIDs, promptID)
	f.secretCalls = append(f.secretCalls, remoteSecretAnswer{secret: secret, accept: accept})
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

func TestConfirmRemoteSecretDelegatesWithoutPersisting(t *testing.T) {
	fake := &fakeRemoteKernel{}
	a := appWithFakeKernel(fake)
	if err := a.ConfirmRemoteSecret("box", "prompt-7", "one-shot-secret", true); err != nil {
		t.Fatal(err)
	}
	if len(fake.secretCalls) != 1 || fake.secretCalls[0].secret != "one-shot-secret" || !fake.secretCalls[0].accept || fake.secretPromptIDs[0] != "prompt-7" {
		t.Fatalf("secret calls = %+v", fake.secretCalls)
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

// TestUpdateHostPreservesHiddenFields pins the data-loss fix: blank secret
// inputs and an edit that does not model forwards must not wipe those fields.
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

	// Edit via the desktop input with blank secrets, changing only the user.
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

func TestSSHConfigReimportPreservesReasonixSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "box", Host: "old.example", Workspace: "/srv/app", ServeInstall: "never",
			PasswordEnv: "REMOTE_BOX_PASSWORD",
			Forwards:    []config.RemoteForwardEntry{{Type: "local", Bind: "127.0.0.1:8080", Target: "127.0.0.1:80"}},
		})
	}); err != nil {
		t.Fatal(err)
	}

	mgr := newDesktopRemoteManager(&App{})
	if _, err := mgr.AddHost(RemoteHostInput{
		Label: "box", Host: "box", UseSSHConfig: true, PreserveExistingSettings: true,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	host, ok := cfg.RemoteHost("box")
	if !ok {
		t.Fatal("reimported host is missing")
	}
	if host.Host != "box" || !host.UseSSHConfig || host.Workspace != "/srv/app" || host.ServeInstall != "never" {
		t.Fatalf("reimported host settings = %+v", host)
	}
	if host.PasswordEnv != "REMOTE_BOX_PASSWORD" || len(host.Forwards) != 1 {
		t.Fatalf("reimport wiped hidden settings: %+v", host)
	}
}

func TestRemoteHostCredentialsStayOutOfConfigAndCanBeCleared(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)

	mgr := newDesktopRemoteManager(&App{})
	in := RemoteHostInput{
		Label: "secure-box", Host: "10.0.0.12", Port: 22, User: "dev", ServeInstall: "auto",
		Password: "server-password", KeyPassphrase: "private-key-passphrase",
	}
	view, err := mgr.AddHost(in)
	if err != nil {
		t.Fatalf("AddHost: %v", err)
	}
	if !view.PasswordSet || !view.KeyPassphraseSet {
		t.Fatalf("credential flags = password:%v passphrase:%v", view.PasswordSet, view.KeyPassphraseSet)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	host, ok := cfg.RemoteHost("secure-box")
	if !ok {
		t.Fatal("saved host missing")
	}
	wantPasswordEnv := config.RemotePasswordCredentialEnvName("secure-box")
	wantPassphraseEnv := config.RemotePassphraseCredentialEnvName("secure-box")
	if host.PasswordEnv != wantPasswordEnv || host.PassphraseEnv != wantPassphraseEnv {
		t.Fatalf("credential refs = password:%q passphrase:%q", host.PasswordEnv, host.PassphraseEnv)
	}
	t.Cleanup(func() {
		_ = config.RemoveCredential(wantPasswordEnv)
		_ = config.RemoveCredential(wantPassphraseEnv)
	})
	if got := config.ResolveCredentialForRootGlobalFirst(home, wantPasswordEnv); !got.Set || got.Value != in.Password {
		t.Fatalf("stored password = set:%v value:%q", got.Set, got.Value)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, wantPassphraseEnv); !got.Set || got.Value != in.KeyPassphrase {
		t.Fatalf("stored passphrase = set:%v value:%q", got.Set, got.Value)
	}
	configBytes, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configBytes), in.Password) || strings.Contains(string(configBytes), in.KeyPassphrase) {
		t.Fatalf("plaintext secret leaked into config.toml:\n%s", configBytes)
	}

	// Blank secret fields preserve both references and stored values.
	if _, err := mgr.UpdateHost("secure-box", RemoteHostInput{
		Label: "secure-box", Host: "10.0.0.12", Port: 22, User: "ops", ServeInstall: "auto",
	}); err != nil {
		t.Fatalf("UpdateHost blank credentials: %v", err)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, wantPasswordEnv); !got.Set || got.Value != in.Password {
		t.Fatalf("blank edit changed password: %+v", got)
	}

	view, err = mgr.UpdateHost("secure-box", RemoteHostInput{
		Label: "secure-box", Host: "10.0.0.12", Port: 22, User: "ops", ServeInstall: "auto", ClearPassword: true,
	})
	if err != nil {
		t.Fatalf("UpdateHost clear password: %v", err)
	}
	if view.PasswordSet || !view.KeyPassphraseSet {
		t.Fatalf("credential flags after clear = password:%v passphrase:%v", view.PasswordSet, view.KeyPassphraseSet)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, wantPasswordEnv); got.Set {
		t.Fatal("generated password credential remains after explicit clear")
	}

	if err := mgr.RemoveHost("secure-box"); err != nil {
		t.Fatalf("RemoveHost: %v", err)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, wantPassphraseEnv); got.Set {
		t.Fatal("generated passphrase credential remains after host removal")
	}
}

func TestClearRemoteHostCredentialDoesNotDeleteUserManagedEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	const key = "TEAM_SHARED_SSH_PASSWORD"
	if _, err := config.SetCredential(key, "shared-secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.RemoveCredential(key) })
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "shared-box", Host: "10.0.0.15", User: "dev", PasswordEnv: key,
		})
	}); err != nil {
		t.Fatal(err)
	}

	mgr := newDesktopRemoteManager(&App{})
	if _, err := mgr.UpdateHost("shared-box", RemoteHostInput{
		Label: "shared-box", Host: "10.0.0.15", Port: 22, User: "dev", ServeInstall: "auto", ClearPassword: true,
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	host, ok := cfg.RemoteHost("shared-box")
	if !ok || host.PasswordEnv != "" {
		t.Fatalf("password reference was not cleared: %+v", host)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, key); !got.Set || got.Value != "shared-secret" {
		t.Fatalf("user-managed credential was deleted: %+v", got)
	}
}

func TestRemoteHostCredentialWriteRollsBackOnFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)

	mgr := newDesktopRemoteManager(&App{})
	_, err := mgr.AddHost(RemoteHostInput{
		Label: "rollback-box", Host: "10.0.0.19", Port: 22, User: "dev", ServeInstall: "auto",
		Password: "must-not-remain", KeyPassphrase: "invalid\npassphrase",
	})
	if err == nil {
		t.Fatal("expected credential validation failure")
	}
	passwordEnv := config.RemotePasswordCredentialEnvName("rollback-box")
	passphraseEnv := config.RemotePassphraseCredentialEnvName("rollback-box")
	t.Cleanup(func() {
		_ = config.RemoveCredential(passwordEnv)
		_ = config.RemoveCredential(passphraseEnv)
	})
	if got := config.ResolveCredentialForRootGlobalFirst(home, passwordEnv); got.Set {
		t.Fatal("first credential write was not rolled back after the second failed")
	}
	cfg, loadErr := config.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if _, ok := cfg.RemoteHost("rollback-box"); ok {
		t.Fatal("host config was saved despite credential write failure")
	}
}

// TestScanSSHConfigReturnsNonNil pins the JSON-contract fix: an empty scan must
// encode as [] (not null), which the React import page iterates safely.
func TestScanSSHConfigReturnsNonNil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home) // no ~/.ssh/config here => empty result
	t.Setenv("USERPROFILE", home)
	mgr := newDesktopRemoteManager(&App{})
	out, err := mgr.ScanSSHConfig()
	if err != nil {
		t.Fatalf("ScanSSHConfig: %v", err)
	}
	if out == nil {
		t.Fatal("ScanSSHConfig returned nil slice (would encode as JSON null and crash the import page)")
	}
}

func TestScanSSHConfigPreservesAliasInsteadOfSnapshottingEffectiveFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configBody := "Host live-box\n  HostName 192.0.2.40\n  User dev\n  Port 2202\n  IdentityFile ~/.ssh/live-box\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := newDesktopRemoteManager(&App{})
	out, err := mgr.ScanSSHConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("scan = %+v", out)
	}
	got := out[0]
	if got.Label != "live-box" || got.Host != "live-box" || !got.UseSSHConfig || !got.PreserveExistingSettings {
		t.Fatalf("alias was not preserved: %+v", got)
	}
	if got.Port != 0 || got.User != "" || got.IdentityFile != "" || got.ProxyJump != "" {
		t.Fatalf("effective config was snapshotted instead of resolved live: %+v", got)
	}
}

func TestOpenRemoteWorkspacePersistsLastWorkspace(t *testing.T) {
	// Workbench path: OpenRemoteWorkspace no longer opens a Serve HTML window.
	// Persistence of last workspace is still via saveLastRemoteWorkspace after a
	// successful connect; unit-test the persistence helper directly.
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	a := &App{ctx: context.Background()}
	a.saveLastRemoteWorkspace("box", "/home/dev/app")
	got := a.RemoteLastWorkspace("box")
	if got != "/home/dev/app" {
		t.Fatalf("last workspace = %q, want /home/dev/app", got)
	}
	if _, err := os.Stat(filepath.Join(config.MemoryUserDir(), "desktop-remote.json")); err != nil {
		t.Fatalf("desktop-remote.json not written: %v", err)
	}
}

func TestWorkbenchSwitchLocalAndHint(t *testing.T) {
	a := &App{ctx: context.Background()}
	active := a.WorkbenchActiveTarget()
	if active["kind"] != "local" {
		t.Fatalf("active = %+v", active)
	}
	a.workbench().targets.RememberRemote(target.RemoteHint{HostID: "lab", Workspace: "/w"})
	hint := a.WorkbenchLastRemoteHint()
	if hint["hostId"] != "lab" || hint["workspace"] != "/w" {
		t.Fatalf("hint = %+v", hint)
	}
	switched := a.WorkbenchSwitchLocal()
	if switched["kind"] != "local" {
		t.Fatalf("switch = %+v", switched)
	}
}

func TestWorkbenchSwitchLocalEmitsUnifiedTargetState(t *testing.T) {
	events := make(chan WorkbenchTargetStateView, 1)
	a := &App{ctx: context.Background()}
	a.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != workbenchTargetEvent || len(payload) != 1 {
			return
		}
		if view, ok := payload[0].(WorkbenchTargetStateView); ok {
			events <- view
		}
	}
	result := a.WorkbenchSwitchLocal()
	select {
	case event := <-events:
		if event.State != "disconnected" || event.Kind != target.KindLocal || event.IdentityGen != result["identityGen"] {
			t.Fatalf("target event = %+v, result = %+v", event, result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remote:workbench-target")
	}
}

func TestWorkbenchConnectFailureKeepsEventsOnCommittedLocalTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	events := make(chan WorkbenchTargetStateView, 2)
	a := &App{ctx: context.Background()}
	a.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != workbenchTargetEvent || len(payload) != 1 {
			return
		}
		if view, ok := payload[0].(WorkbenchTargetStateView); ok {
			events <- view
		}
	}
	err := a.WorkbenchConnectRemote("missing-host", "/srv/work")
	if err == nil || !strings.Contains(err.Error(), "unknown remote host") {
		t.Fatalf("connect error = %v", err)
	}
	connecting := <-events
	restored := <-events
	if connecting.State != "connecting" || connecting.Kind != target.KindLocal || connecting.HostID != "" || connecting.Workspace != "" {
		t.Fatalf("connecting event exposed candidate as active: %+v", connecting)
	}
	if restored.State != "disconnected" || restored.Kind != target.KindLocal || restored.Error != err.Error() {
		t.Fatalf("restored event = %+v", restored)
	}
	if restored.IdentityGen != connecting.IdentityGen || restored.RequestSeq != connecting.RequestSeq {
		t.Fatalf("failed candidate changed active fencing: connecting=%+v restored=%+v", connecting, restored)
	}
}

func TestWorkbenchReplacementFailureRestoresCommittedRemoteEvent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	events := make(chan WorkbenchTargetStateView, 2)
	a := &App{ctx: context.Background()}
	_, generation, err := a.workbench().targets.BeginRemoteConnect("host-a", "/workspace-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.workbench().targets.MarkRemoteConnected(generation); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := a.workbench().targets.ActivateRemote(generation); err != nil {
		t.Fatal(err)
	}
	a.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name == workbenchTargetEvent && len(payload) == 1 {
			if view, ok := payload[0].(WorkbenchTargetStateView); ok {
				events <- view
			}
		}
	}
	err = a.WorkbenchConnectRemote("missing-host", "/workspace-b")
	if err == nil {
		t.Fatal("replacement unexpectedly succeeded")
	}
	connecting := <-events
	restored := <-events
	if connecting.State != "connecting" || connecting.Kind != target.KindRemote || connecting.HostID != "host-a" || connecting.Workspace != "/workspace-a" {
		t.Fatalf("connecting event = %+v", connecting)
	}
	if restored.State != "connected" || restored.Kind != target.KindRemote || restored.HostID != "host-a" || restored.Error != err.Error() {
		t.Fatalf("restored event = %+v", restored)
	}
}

func TestWorkbenchSubmitFailsClosedDuringTargetConnect(t *testing.T) {
	a := &App{ctx: context.Background()}
	if _, _, err := a.workbench().targets.BeginRemoteConnect("lab", "/srv/work"); err != nil {
		t.Fatal(err)
	}
	handled, err := a.workbenchSubmit("hello", "hello", "", nil, false)
	if !handled || err == nil || !strings.Contains(err.Error(), "target is connecting") {
		t.Fatalf("submit handled=%v err=%v", handled, err)
	}
}

func TestWorkbenchActiveTargetIncludesPersistedReconnectHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	first := &App{ctx: context.Background()}
	first.saveLastRemoteWorkspace("persisted-host", "/srv/work")

	restarted := &App{ctx: context.Background()}
	active := restarted.WorkbenchActiveTarget()
	reconnect, ok := active["reconnect"].(map[string]string)
	if !ok || reconnect["hostId"] != "persisted-host" || reconnect["workspace"] != "/srv/work" {
		t.Fatalf("active reconnect = %#v", active["reconnect"])
	}
}
