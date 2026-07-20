package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/remote"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/remote/forward"
)

// ── View structs mirrored in frontend/src/lib/types.ts ──

type RemoteHostView struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	User             string `json:"user"`
	IdentityFile     string `json:"identityFile"`
	ProxyJump        string `json:"proxyJump"`
	DefaultWorkspace string `json:"defaultWorkspace"`
	ServeInstall     string `json:"serveInstall"`
	UseSSHConfig     bool   `json:"useSSHConfig"`
	PasswordSet      bool   `json:"passwordSet,omitempty"`
	KeyPassphraseSet bool   `json:"keyPassphraseSet,omitempty"`
}

type RemoteHostInput struct {
	Label                    string `json:"label"`
	Host                     string `json:"host"`
	Port                     int    `json:"port"`
	User                     string `json:"user"`
	IdentityFile             string `json:"identityFile"`
	ProxyJump                string `json:"proxyJump"`
	DefaultWorkspace         string `json:"defaultWorkspace"`
	ServeInstall             string `json:"serveInstall"`
	UseSSHConfig             bool   `json:"useSSHConfig"`
	Password                 string `json:"password,omitempty"`
	KeyPassphrase            string `json:"keyPassphrase,omitempty"`
	ClearPassword            bool   `json:"clearPassword,omitempty"`
	ClearPassphrase          bool   `json:"clearPassphrase,omitempty"`
	PreserveExistingSettings bool   `json:"preserveExistingSettings,omitempty"`
}

type RemoteFingerprintView struct {
	HostID  string `json:"hostId"`
	Address string `json:"address"`
	KeyType string `json:"keyType"`
	SHA256  string `json:"sha256"`
}

type RemoteConnectionStatusView struct {
	HostID       string                            `json:"hostId"`
	State        string                            `json:"state"`
	Error        string                            `json:"error,omitempty"`
	ErrorDetails *RemoteConnectionErrorDetailsView `json:"errorDetails,omitempty"`
	Fingerprint  *RemoteFingerprintView            `json:"fingerprint,omitempty"`
	SecretPrompt *RemoteSecretPromptView           `json:"secretPrompt,omitempty"`
	Attempt      int                               `json:"attempt,omitempty"`
}

// RemoteSecretPromptView contains prompt metadata only. Secret text travels
// one way through ConfirmRemoteSecret and is never emitted in status events.
type RemoteSecretPromptView struct {
	PromptID string `json:"promptId"`
	HostID   string `json:"hostId"`
	Host     string `json:"host"`
	Kind     string `json:"kind"` // password | passphrase
	Identity string `json:"identity,omitempty"`
}

type RemoteKnownHostLocationView struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type RemoteConnectionErrorDetailsView struct {
	Code             string                        `json:"code"`
	PresentedSHA256  string                        `json:"presentedSha256,omitempty"`
	KnownHostRecords []RemoteKnownHostLocationView `json:"knownHostRecords,omitempty"`
}

type RemoteDirEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"isDir"`
	Size      int64  `json:"size"`
	MtimeUnix int64  `json:"mtimeUnix"`
	Symlink   bool   `json:"symlink"`
}

type RemoteFilePreview struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Size      int64  `json:"size"`
	MtimeUnix int64  `json:"mtimeUnix"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	Err       string `json:"err,omitempty"`
}

type RemoteWriteResult struct {
	OK           bool  `json:"ok"`
	Conflict     bool  `json:"conflict"`
	NewMtimeUnix int64 `json:"newMtimeUnix"`
}

type RemoteForwardInput struct {
	LocalPort  int    `json:"localPort"`
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Label      string `json:"label"`
}

type RemoteForwardView struct {
	ID         string `json:"id"`
	HostID     string `json:"hostId"`
	LocalPort  int    `json:"localPort"`
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Label      string `json:"label"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
}

type RemoteServerView struct {
	HostID    string `json:"hostId"`
	Workspace string `json:"workspace"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	LocalURL  string `json:"localUrl,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ── Kernel seam ──

// remoteKernel is the desktop's view of the remote subsystem. The concrete
// *desktopRemoteManager satisfies it; remote_app_test.go injects a fake.
type remoteKernel interface {
	Hosts() ([]RemoteHostView, error)
	AddHost(RemoteHostInput) (RemoteHostView, error)
	UpdateHost(id string, in RemoteHostInput) (RemoteHostView, error)
	RemoveHost(id string) error
	ScanSSHConfig() ([]RemoteHostInput, error)

	Connect(hostID string) error
	Disconnect(hostID string) error
	Statuses() []RemoteConnectionStatusView
	ResolveHostKey(hostID string, accept bool) error
	ResolveSecret(hostID, promptID, secret string, accept bool) error

	ListDir(ctx context.Context, hostID, path string) ([]RemoteDirEntry, error)
	ReadFile(ctx context.Context, hostID, path string) (RemoteFilePreview, error)
	WriteFile(ctx context.Context, hostID, path, body string, expectMtime int64) (RemoteWriteResult, error)
	Mkdir(ctx context.Context, hostID, path string) error
	Rename(ctx context.Context, hostID, oldPath, newPath string) error
	Delete(ctx context.Context, hostID, path string, recursive bool) error

	Forwards(hostID string) []RemoteForwardView
	AddForward(hostID string, in RemoteForwardInput) (RemoteForwardView, error)
	RemoveForward(hostID, forwardID string) error

	EnsureServer(ctx context.Context, hostID, workspace string) (RemoteServerView, string, error)
	StopServer(hostID string) error
	ServerStatus(hostID string) RemoteServerView
	ServerLogs(ctx context.Context, hostID string, tailLines int) (string, error)

	Close() error
}

// remoteEventSink receives kernel status transitions for bridging to the
// frontend. All methods may be called from kernel goroutines.
type remoteEventSink interface {
	onStatus(RemoteConnectionStatusView)
	onForwards(hostID string, forwards []RemoteForwardView)
	onServer(RemoteServerView)
}

// ── App wiring ──

func (a *App) remoteRT() (remoteKernel, error) {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	if a.remoteRuntime != nil {
		return a.remoteRuntime, nil
	}
	mgr := newDesktopRemoteManager(a)
	a.remoteRuntime = mgr
	return mgr, nil
}

func (a *App) stopRemoteRuntime() {
	a.remoteMu.Lock()
	rt := a.remoteRuntime
	a.remoteRuntime = nil
	a.remoteMu.Unlock()
	if rt != nil {
		_ = rt.Close()
	}
}

// emitRemoteEvent bridges a kernel callback to the frontend through the async
// emitter so a slow webview never blocks the kernel.
func (a *App) emitRemoteEvent(name string, payload any) {
	ctx := a.bootContext()
	if ctx == nil {
		return
	}
	a.runtimeEvents.Emit(ctx, name, payload)
}

// remoteEventSink implementation on *App.
func (a *App) onStatus(s RemoteConnectionStatusView) { a.emitRemoteEvent("remote:status", s) }
func (a *App) onServer(s RemoteServerView)           { a.emitRemoteEvent("remote:server", s) }
func (a *App) onForwards(hostID string, f []RemoteForwardView) {
	a.emitRemoteEvent("remote:forwards", map[string]any{"hostId": hostID, "forwards": f})
}

// ── Bound methods ──

func (a *App) RemoteHosts() ([]RemoteHostView, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return nil, err
	}
	return rt.Hosts()
}

func (a *App) AddRemoteHost(in RemoteHostInput) (RemoteHostView, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return RemoteHostView{}, err
	}
	return rt.AddHost(in)
}

func (a *App) UpdateRemoteHost(id string, in RemoteHostInput) (RemoteHostView, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return RemoteHostView{}, err
	}
	return rt.UpdateHost(id, in)
}

func (a *App) RemoveRemoteHost(id string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.RemoveHost(id)
}

func (a *App) ScanSSHConfig() ([]RemoteHostInput, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return nil, err
	}
	return rt.ScanSSHConfig()
}

func (a *App) ConnectRemoteHost(id string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	if err := rt.Connect(id); err != nil {
		view := RemoteConnectionStatusView{HostID: id, State: "stopped"}
		applyRemoteConnectionError(&view, err)
		a.onStatus(view)
		return err
	}
	return nil
}

func applyRemoteConnectionError(view *RemoteConnectionStatusView, err error) {
	if err == nil {
		return
	}
	view.Error = err.Error()
	if view.State == "degraded" {
		return
	}
	details := &RemoteConnectionErrorDetailsView{Code: "connection_failed"}
	switch {
	case errors.Is(err, remote.ErrHostKeyMismatch):
		details.Code = "host_key_mismatch"
		var mismatch *remote.HostKeyMismatchError
		if errors.As(err, &mismatch) {
			details.PresentedSHA256 = mismatch.PresentedFingerprint
			details.KnownHostRecords = make([]RemoteKnownHostLocationView, 0, len(mismatch.Locations))
			for _, location := range mismatch.Locations {
				details.KnownHostRecords = append(details.KnownHostRecords, RemoteKnownHostLocationView{
					Path: location.Filename,
					Line: location.Line,
				})
			}
		}
	case errors.Is(err, remote.ErrAuthFailed):
		details.Code = "auth_failed"
	case errors.Is(err, remote.ErrHostKeyRejected):
		details.Code = "host_key_rejected"
	}
	view.ErrorDetails = details
}

func (a *App) DisconnectRemoteHost(id string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.Disconnect(id)
}

func (a *App) RemoteConnectionStatuses() []RemoteConnectionStatusView {
	rt, err := a.remoteRT()
	if err != nil {
		return nil
	}
	return rt.Statuses()
}

func (a *App) ConfirmRemoteHostKey(hostID string, accept bool) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.ResolveHostKey(hostID, accept)
}

// ConfirmRemoteSecret resolves a one-shot interactive SSH credential prompt.
// The secret is retained only in the connection's in-memory reconnect cache;
// callers must use the host settings form when they explicitly want storage.
func (a *App) ConfirmRemoteSecret(hostID, promptID, secret string, accept bool) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.ResolveSecret(hostID, promptID, secret, accept)
}

func (a *App) ListRemoteDir(hostID, path string) ([]RemoteDirEntry, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return nil, err
	}
	return rt.ListDir(a.bootContext(), hostID, path)
}

func (a *App) ReadRemoteFile(hostID, path string) (RemoteFilePreview, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return RemoteFilePreview{}, err
	}
	return rt.ReadFile(a.bootContext(), hostID, path)
}

func (a *App) WriteRemoteFile(hostID, path, body string, expectMtimeUnix int64) (RemoteWriteResult, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return RemoteWriteResult{}, err
	}
	return rt.WriteFile(a.bootContext(), hostID, path, body, expectMtimeUnix)
}

func (a *App) MkdirRemote(hostID, path string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.Mkdir(a.bootContext(), hostID, path)
}

func (a *App) RenameRemotePath(hostID, oldPath, newPath string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.Rename(a.bootContext(), hostID, oldPath, newPath)
}

func (a *App) DeleteRemotePath(hostID, path string, recursive bool) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.Delete(a.bootContext(), hostID, path, recursive)
}

func (a *App) RemoteForwards(hostID string) ([]RemoteForwardView, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return nil, err
	}
	return rt.Forwards(hostID), nil
}

func (a *App) AddRemoteForward(hostID string, in RemoteForwardInput) (RemoteForwardView, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return RemoteForwardView{}, err
	}
	return rt.AddForward(hostID, in)
}

func (a *App) RemoveRemoteForward(hostID, forwardID string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.RemoveForward(hostID, forwardID)
}

func (a *App) EnsureRemoteServer(hostID, workspace string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	a.goSafe("remoteEnsureServer", func() {
		_, _, _ = rt.EnsureServer(a.bootContext(), hostID, workspace)
	})
	return nil
}

func (a *App) OpenRemoteWorkspace(hostID, workspace string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	view, token, err := rt.EnsureServer(a.bootContext(), hostID, workspace)
	if err != nil {
		return err
	}
	if view.LocalURL == "" {
		return fmt.Errorf("remote serve did not report a local URL")
	}
	url := view.LocalURL
	if token != "" && !strings.Contains(url, "token=") {
		url = fmt.Sprintf("%s?token=%s", strings.TrimRight(url, "/"), token)
	}
	a.saveLastRemoteWorkspace(hostID, workspace)
	return a.openRemoteWindow(url, hostID)
}

func (a *App) StopRemoteServer(hostID string) error {
	rt, err := a.remoteRT()
	if err != nil {
		return err
	}
	return rt.StopServer(hostID)
}

func (a *App) RemoteServerStatus(hostID string) (RemoteServerView, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return RemoteServerView{}, err
	}
	return rt.ServerStatus(hostID), nil
}

func (a *App) RemoteServerLogs(hostID string, tailLines int) (string, error) {
	rt, err := a.remoteRT()
	if err != nil {
		return "", err
	}
	return rt.ServerLogs(a.bootContext(), hostID, tailLines)
}

// editUserConfig runs mutate against the user-global config under the edit lock
// and saves it there. Remote hosts are user-global (pinned in LoadForRoot).
func editUserConfig(mutate func(*config.Config) error) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("cannot resolve user config path")
	}
	cfg := config.LoadForEdit(path)
	if cfg == nil {
		cfg = config.Default()
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

// ── desktopRemoteManager: concrete remoteKernel ──

type managedHost struct {
	client         desktopSSHClient
	ctx            context.Context
	cancel         context.CancelFunc
	status         RemoteConnectionStatusView
	server         RemoteServerView
	token          string
	fpAnswer       chan bool               // TOFU resolution channel; non-nil while pending
	secretAnswer   chan remoteSecretAnswer // one-shot credential channel; non-nil while pending
	secretPromptID string                  // opaque ID prevents a stale dialog resolving a later prompt
	serveMu        sync.Mutex              // serializes EnsureServer/StopServer for this host
}

type remoteSecretAnswer struct {
	secret string
	accept bool
}

type desktopSSHClient interface {
	bootstrap.Conn
	Start(context.Context) error
	Close() error
	Subscribe(func(remote.StatusEvent)) func()
	Forwards() *forward.Set
}

type desktopRemoteManager struct {
	sink remoteEventSink

	mu    sync.Mutex
	hosts map[string]*managedHost

	newClient   func(remote.Options) (desktopSSHClient, error)
	ensureServe func(context.Context, bootstrap.Conn, bootstrap.Options) (bootstrap.Result, error)
	stopServe   func(context.Context, bootstrap.Conn, string) error
	serveLogs   func(context.Context, bootstrap.Conn, string, int, *strings.Builder) error
	localBinary func() string
	promptGate  chan struct{}
	promptSeq   uint64
}

func newDesktopRemoteManager(sink remoteEventSink) *desktopRemoteManager {
	return &desktopRemoteManager{
		sink:  sink,
		hosts: map[string]*managedHost{},
		newClient: func(opts remote.Options) (desktopSSHClient, error) {
			return remote.New(opts)
		},
		ensureServe: bootstrap.EnsureServe,
		stopServe:   bootstrap.Stop,
		serveLogs: func(ctx context.Context, conn bootstrap.Conn, workspace string, n int, out *strings.Builder) error {
			return bootstrap.Logs(ctx, conn, workspace, n, out)
		},
		localBinary: desktopCLIBinaryPath,
		promptGate:  make(chan struct{}, 1),
	}
}

func (m *desktopRemoteManager) Hosts() ([]RemoteHostView, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]RemoteHostView, 0, len(cfg.Remote.Hosts))
	for _, h := range cfg.Remote.Hosts {
		out = append(out, hostEntryToView(h))
	}
	return out, nil
}

func (m *desktopRemoteManager) AddHost(in RemoteHostInput) (RemoteHostView, error) {
	var entry config.RemoteHostEntry
	if err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		entry = inputToHostEntry(in)
		if existing, ok := c.RemoteHost(entry.Name); ok {
			preserveRemoteHostHiddenFields(&entry, existing)
			if in.PreserveExistingSettings {
				preserveRemoteHostImportSettings(&entry, existing)
			}
		}
		changes, removals := applyRemoteCredentialInput(&entry, in)
		if err := c.UpsertRemoteHost(entry); err != nil {
			return nil, err
		}
		return append(changes, config.UnusedGeneratedRemoteCredentialChanges(c, removals)...), nil
	}); err != nil {
		return RemoteHostView{}, err
	}
	return hostEntryToView(entry), nil
}

func (m *desktopRemoteManager) UpdateHost(id string, in RemoteHostInput) (RemoteHostView, error) {
	var merged config.RemoteHostEntry
	if err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		entry := inputToHostEntry(in)
		entry.Name = id
		if existing, ok := c.RemoteHost(id); ok {
			preserveRemoteHostHiddenFields(&entry, existing)
		}
		changes, removals := applyRemoteCredentialInput(&entry, in)
		merged = entry
		if err := c.UpsertRemoteHost(entry); err != nil {
			return nil, err
		}
		return append(changes, config.UnusedGeneratedRemoteCredentialChanges(c, removals)...), nil
	}); err != nil {
		return RemoteHostView{}, err
	}
	return hostEntryToView(merged), nil
}

func (m *desktopRemoteManager) RemoveHost(id string) error {
	_ = m.Disconnect(id)
	removed := false
	if err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		var removals []string
		if existing, ok := c.RemoteHost(id); ok {
			for _, key := range []string{existing.PasswordEnv, existing.PassphraseEnv} {
				if config.IsGeneratedRemoteCredential(id, key) {
					removals = append(removals, key)
				}
			}
		}
		removed = c.RemoveRemoteHost(id)
		return config.UnusedGeneratedRemoteCredentialChanges(c, removals), nil
	}); err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("no remote host named %q", id)
	}
	return nil
}

func (m *desktopRemoteManager) ScanSSHConfig() ([]RemoteHostInput, error) {
	src, err := remote.LoadUserSSHConfig()
	if err != nil {
		return nil, err
	}
	// Non-nil so Wails encodes an empty result as [] (not null), which the React
	// import page iterates safely.
	out := []RemoteHostInput{}
	for _, cand := range src.Aliases() {
		out = append(out, RemoteHostInput{
			Label:                    cand.Alias,
			Host:                     cand.Alias,
			Port:                     0,
			UseSSHConfig:             true,
			PreserveExistingSettings: true,
		})
	}
	return out, nil
}

func (m *desktopRemoteManager) Connect(hostID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sshCfg, err := remote.LoadUserSSHConfig()
	if err != nil {
		return fmt.Errorf("load SSH config: %w", err)
	}
	host, err := remote.ResolveHost(cfg, hostID, sshCfg)
	if err != nil {
		return err
	}

	resolvedJumps, err := remote.ResolveJumpHosts(cfg, host.ProxyJump, sshCfg)
	if err != nil {
		return err
	}

	// Honor the user's proxy settings for the SSH dial, same as the CLI.
	dialer, derr := netclient.NewStreamDialer(cfg.NetworkProxySpec())
	if derr != nil {
		return fmt.Errorf("remote: network proxy is misconfigured: %w", derr)
	}

	hostCtx, cancel := context.WithCancel(context.Background())
	mh := &managedHost{
		ctx: hostCtx, cancel: cancel,
		status: RemoteConnectionStatusView{HostID: hostID, State: "connecting"},
	}
	secretPrompt := m.secretPrompt(hostID, mh)
	auth := desktopAuthForHost(host, secretPrompt)
	jumpHosts := make([]remote.JumpHostOptions, 0, len(resolvedJumps))
	for _, jump := range resolvedJumps {
		jumpHosts = append(jumpHosts, remote.JumpHostOptions{Host: jump, Auth: desktopAuthForHost(jump, secretPrompt)})
	}
	policy := &remote.HostKeyPolicy{Prompt: m.hostKeyPrompt(hostID, mh)}
	client, err := m.newClient(remote.Options{
		Host: host, Auth: auth, JumpHosts: jumpHosts, HostKeys: policy, Dialer: dialer,
	})
	if err != nil {
		cancel()
		return err
	}
	mh.client = client

	// Insert a fully-populated generation atomically. A stopped generation is
	// replaceable; active/connecting generations make Connect idempotent.
	var replaced *managedHost
	m.mu.Lock()
	if existing := m.hosts[hostID]; existing != nil && existing.status.State != "stopped" {
		m.mu.Unlock()
		cancel()
		_ = client.Close()
		return nil // already connecting/connected
	}
	replaced = m.hosts[hostID]
	m.hosts[hostID] = mh
	m.mu.Unlock()
	closeManagedHost(replaced)

	client.Subscribe(func(ev remote.StatusEvent) { m.onClientStatus(hostID, mh, ev) })

	go func() {
		if err := client.Start(hostCtx); err != nil {
			// Keep the stopped generation and its user-visible error. The next
			// Connect atomically replaces it with a fresh client.
			cancel()
			_ = client.Close()
			return
		}
		m.applyConfiguredForwards(hostID, mh, cfg)
	}()
	return nil
}

func desktopAuthForHost(host remote.ResolvedHost, prompt remote.SecretPrompt) remote.AuthOptions {
	auth := remote.AuthOptions{SecretPrompt: prompt}
	if host.PassphraseEnv != "" {
		env := host.PassphraseEnv
		auth.Passphrase = func() (string, error) { return config.ResolveCredential(env).Value, nil }
	}
	if host.PasswordEnv != "" {
		env := host.PasswordEnv
		auth.Password = func() (string, error) { return config.ResolveCredential(env).Value, nil }
	}
	return auth
}

func (m *desktopRemoteManager) applyConfiguredForwards(hostID string, mh *managedHost, cfg *config.Config) {
	entry, ok := cfg.RemoteHost(hostID)
	if !ok || !m.isCurrent(hostID, mh) {
		return
	}
	for _, f := range entry.Forwards {
		dir := forward.Local
		if strings.EqualFold(f.Type, "remote") {
			dir = forward.Remote
		}
		_, _ = mh.client.Forwards().Add(forward.Spec{Direction: dir, BindAddr: desktopNormalizeBind(f.Bind), TargetAddr: f.Target})
	}
	m.emitForwardsFor(hostID, mh)
}

func (m *desktopRemoteManager) Disconnect(hostID string) error {
	m.mu.Lock()
	mh := m.hosts[hostID]
	delete(m.hosts, hostID)
	var answer chan bool
	var secretAnswer chan remoteSecretAnswer
	if mh != nil {
		answer = mh.fpAnswer
		mh.fpAnswer = nil
		secretAnswer = mh.secretAnswer
		mh.secretAnswer = nil
		mh.secretPromptID = ""
	}
	if mh != nil && m.sink != nil {
		m.sink.onStatus(RemoteConnectionStatusView{HostID: hostID, State: "stopped"})
	}
	m.mu.Unlock()
	if mh == nil {
		return nil
	}
	if answer != nil {
		select {
		case answer <- false:
		default:
		}
	}
	if secretAnswer != nil {
		select {
		case secretAnswer <- remoteSecretAnswer{}:
		default:
		}
	}
	closeManagedHost(mh)
	return nil
}

func closeManagedHost(mh *managedHost) {
	if mh == nil {
		return
	}
	if mh.cancel != nil {
		mh.cancel()
	}
	if mh.client != nil {
		_ = mh.client.Close()
	}
}

func (m *desktopRemoteManager) Statuses() []RemoteConnectionStatusView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RemoteConnectionStatusView, 0, len(m.hosts))
	for _, mh := range m.hosts {
		out = append(out, mh.status)
	}
	return out
}

func (m *desktopRemoteManager) ResolveHostKey(hostID string, accept bool) error {
	m.mu.Lock()
	mh := m.hosts[hostID]
	var ch chan bool
	if mh != nil {
		ch = mh.fpAnswer
	}
	m.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("no pending host key confirmation for %q", hostID)
	}
	select {
	case ch <- accept:
		return nil
	default:
		return fmt.Errorf("host key confirmation already resolved for %q", hostID)
	}
}

func (m *desktopRemoteManager) ResolveSecret(hostID, promptID, secret string, accept bool) error {
	m.mu.Lock()
	mh := m.hosts[hostID]
	var ch chan remoteSecretAnswer
	if mh != nil && mh.secretPromptID == promptID {
		ch = mh.secretAnswer
	}
	m.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("no pending SSH credential prompt for %q", hostID)
	}
	select {
	case ch <- remoteSecretAnswer{secret: secret, accept: accept}:
		return nil
	default:
		return fmt.Errorf("SSH credential prompt already resolved for %q", hostID)
	}
}

// hostKeyPrompt returns a HostKeyPrompt that surfaces the fingerprint as a
// pending_hostkey status and blocks on the answer channel until the UI calls
// ConfirmRemoteHostKey.
func (m *desktopRemoteManager) hostKeyPrompt(hostID string, generation *managedHost) remote.HostKeyPrompt {
	return func(ctx context.Context, q remote.HostKeyQuestion) (bool, error) {
		// The frontend presents one global TOFU dialog. Serialize prompts so two
		// simultaneous first-seen hosts cannot overwrite one another in the UI.
		select {
		case m.promptGate <- struct{}{}:
			defer func() { <-m.promptGate }()
		case <-ctx.Done():
			return false, ctx.Err()
		}
		answer := make(chan bool, 1)
		m.mu.Lock()
		mh := m.hosts[hostID]
		if mh != generation {
			m.mu.Unlock()
			return false, fmt.Errorf("host %q connection was replaced", hostID)
		}
		mh.fpAnswer = answer
		fp := &RemoteFingerprintView{HostID: hostID, Address: q.Address, KeyType: q.KeyType, SHA256: q.Fingerprint}
		mh.status = RemoteConnectionStatusView{HostID: hostID, State: "pending_hostkey", Fingerprint: fp}
		status := mh.status
		if m.sink != nil {
			m.sink.onStatus(status)
		}
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			if m.hosts[hostID] == generation && generation.fpAnswer == answer {
				generation.fpAnswer = nil
			}
			m.mu.Unlock()
		}()

		select {
		case ok := <-answer:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(2 * time.Minute):
			return false, fmt.Errorf("host key confirmation timed out")
		}
	}
}

// secretPrompt surfaces a password/passphrase request as a global desktop
// dialog. Prompt metadata may be emitted, but the entered secret only crosses
// the one-shot answer channel and AuthOptions' in-memory reconnect cache.
func (m *desktopRemoteManager) secretPrompt(hostID string, generation *managedHost) remote.SecretPrompt {
	return func(ctx context.Context, kind remote.SecretKind, host, identityFile string) (string, error) {
		select {
		case m.promptGate <- struct{}{}:
			defer func() { <-m.promptGate }()
		case <-ctx.Done():
			return "", ctx.Err()
		}

		answer := make(chan remoteSecretAnswer, 1)
		m.mu.Lock()
		mh := m.hosts[hostID]
		if mh != generation {
			m.mu.Unlock()
			return "", fmt.Errorf("host %q connection was replaced", hostID)
		}
		m.promptSeq++
		promptID := fmt.Sprintf("ssh-secret-%d", m.promptSeq)
		mh.secretAnswer = answer
		mh.secretPromptID = promptID
		identity := ""
		if strings.TrimSpace(identityFile) != "" {
			identity = filepath.Base(identityFile)
		}
		prompt := &RemoteSecretPromptView{PromptID: promptID, HostID: hostID, Host: host, Kind: kind.String(), Identity: identity}
		mh.status = RemoteConnectionStatusView{HostID: hostID, State: "pending_secret", SecretPrompt: prompt}
		status := mh.status
		if m.sink != nil {
			m.sink.onStatus(status)
		}
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			if m.hosts[hostID] == generation && generation.secretAnswer == answer {
				generation.secretAnswer = nil
				generation.secretPromptID = ""
			}
			m.mu.Unlock()
		}()

		select {
		case response := <-answer:
			if !response.accept {
				return "", fmt.Errorf("remote: %s prompt canceled", kind)
			}
			return response.secret, nil
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Minute):
			return "", fmt.Errorf("remote: %s prompt timed out", kind)
		}
	}
}

func (m *desktopRemoteManager) onClientStatus(hostID string, generation *managedHost, ev remote.StatusEvent) {
	if ev.Status == remote.StatusIdle {
		return
	}
	view := RemoteConnectionStatusView{
		HostID:  hostID,
		State:   statusString(ev.Status),
		Attempt: ev.Attempt,
	}
	if ev.Err != nil {
		applyRemoteConnectionError(&view, ev.Err)
	}
	m.mu.Lock()
	mh := m.hosts[hostID]
	if mh != generation {
		m.mu.Unlock()
		return
	}
	// Preserve a pending modal that a separate prompt goroutine set.
	if (mh.status.State == "pending_hostkey" || mh.status.State == "pending_secret") && view.State == "connecting" {
		m.mu.Unlock()
		return
	}
	mh.status = view
	if m.sink != nil {
		m.sink.onStatus(view)
	}
	m.mu.Unlock()
}

func (m *desktopRemoteManager) isCurrent(hostID string, generation *managedHost) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts[hostID] == generation
}

func (m *desktopRemoteManager) client(hostID string) desktopSSHClient {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mh := m.hosts[hostID]; mh != nil {
		return mh.client
	}
	return nil
}

func (m *desktopRemoteManager) fs(ctx context.Context, hostID string) (desktopSSHClient, error) {
	c := m.client(hostID)
	if c == nil {
		return nil, fmt.Errorf("host %q is not connected", hostID)
	}
	return c, nil
}

func (m *desktopRemoteManager) ListDir(ctx context.Context, hostID, path string) ([]RemoteDirEntry, error) {
	c, err := m.fs(ctx, hostID)
	if err != nil {
		return nil, err
	}
	fsys, err := c.SFTP()
	if err != nil {
		return nil, err
	}
	entries, err := fsys.List(ctx, path)
	if err != nil {
		return nil, err
	}
	out := make([]RemoteDirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, RemoteDirEntry{
			Name: e.Name, Path: e.Path, IsDir: e.IsDir,
			Size: e.Size, MtimeUnix: e.ModTime, Symlink: e.Symlink,
		})
	}
	return out, nil
}

func (m *desktopRemoteManager) ReadFile(ctx context.Context, hostID, path string) (RemoteFilePreview, error) {
	c, err := m.fs(ctx, hostID)
	if err != nil {
		return RemoteFilePreview{}, err
	}
	fsys, err := c.SFTP()
	if err != nil {
		return RemoteFilePreview{}, err
	}
	st, err := fsys.Stat(ctx, path)
	if err != nil {
		return RemoteFilePreview{Path: path, Err: err.Error()}, nil
	}
	data, truncated, kind, err := fsys.ReadFile(ctx, path, 0)
	if err != nil {
		return RemoteFilePreview{Path: path, Err: err.Error()}, nil
	}
	binary := kind != 0 // sftpfs.KindText == 0
	prev := RemoteFilePreview{
		Path: path, Size: st.Size, MtimeUnix: st.ModTime,
		Truncated: truncated, Binary: binary,
	}
	if !binary {
		prev.Body = string(data)
	}
	return prev, nil
}

func (m *desktopRemoteManager) WriteFile(ctx context.Context, hostID, path, body string, expectMtime int64) (RemoteWriteResult, error) {
	c, err := m.fs(ctx, hostID)
	if err != nil {
		return RemoteWriteResult{}, err
	}
	fsys, err := c.SFTP()
	if err != nil {
		return RemoteWriteResult{}, err
	}
	// Optimistic-concurrency check: if the caller passed an expected mtime and
	// the remote file moved, report a conflict instead of overwriting.
	if expectMtime > 0 {
		if st, serr := fsys.Stat(ctx, path); serr == nil && st.ModTime != expectMtime {
			return RemoteWriteResult{Conflict: true}, nil
		}
	}
	if err := fsys.WriteFileAtomic(ctx, path, []byte(body), 0o644); err != nil {
		return RemoteWriteResult{}, err
	}
	st, _ := fsys.Stat(ctx, path)
	return RemoteWriteResult{OK: true, NewMtimeUnix: st.ModTime}, nil
}

func (m *desktopRemoteManager) Mkdir(ctx context.Context, hostID, path string) error {
	c, err := m.fs(ctx, hostID)
	if err != nil {
		return err
	}
	fsys, err := c.SFTP()
	if err != nil {
		return err
	}
	return fsys.MkdirAll(ctx, path)
}

func (m *desktopRemoteManager) Rename(ctx context.Context, hostID, oldPath, newPath string) error {
	c, err := m.fs(ctx, hostID)
	if err != nil {
		return err
	}
	fsys, err := c.SFTP()
	if err != nil {
		return err
	}
	return fsys.Rename(ctx, oldPath, newPath)
}

func (m *desktopRemoteManager) Delete(ctx context.Context, hostID, path string, recursive bool) error {
	c, err := m.fs(ctx, hostID)
	if err != nil {
		return err
	}
	fsys, err := c.SFTP()
	if err != nil {
		return err
	}
	return fsys.Remove(ctx, path, recursive)
}

func (m *desktopRemoteManager) Forwards(hostID string) []RemoteForwardView {
	c := m.client(hostID)
	if c == nil {
		return nil
	}
	return forwardEntriesToViews(hostID, c.Forwards().List())
}

func (m *desktopRemoteManager) AddForward(hostID string, in RemoteForwardInput) (RemoteForwardView, error) {
	c := m.client(hostID)
	if c == nil {
		return RemoteForwardView{}, fmt.Errorf("host %q is not connected", hostID)
	}
	if in.LocalPort <= 0 || in.LocalPort > 65535 || in.RemotePort <= 0 || in.RemotePort > 65535 || strings.TrimSpace(in.RemoteHost) == "" {
		return RemoteForwardView{}, fmt.Errorf("forward requires a remote host and ports between 1 and 65535")
	}
	spec := forward.Spec{
		Name:       in.Label,
		Direction:  forward.Local,
		BindAddr:   net.JoinHostPort("127.0.0.1", fmt.Sprint(in.LocalPort)),
		TargetAddr: net.JoinHostPort(strings.TrimSpace(in.RemoteHost), fmt.Sprint(in.RemotePort)),
	}
	if _, err := c.Forwards().Add(spec); err != nil {
		return RemoteForwardView{}, err
	}
	m.emitForwards(hostID)
	view := RemoteForwardView{
		ID: spec.DefaultName(), HostID: hostID, LocalPort: in.LocalPort,
		RemoteHost: in.RemoteHost, RemotePort: in.RemotePort, Label: in.Label, State: "active",
	}
	return view, nil
}

func (m *desktopRemoteManager) RemoveForward(hostID, forwardID string) error {
	c := m.client(hostID)
	if c == nil {
		return fmt.Errorf("host %q is not connected", hostID)
	}
	if err := c.Forwards().Remove(forwardID); err != nil {
		return err
	}
	m.emitForwards(hostID)
	return nil
}

func (m *desktopRemoteManager) emitForwards(hostID string) {
	mh := m.managed(hostID)
	if mh != nil {
		m.emitForwardsFor(hostID, mh)
	}
}

func (m *desktopRemoteManager) emitForwardsFor(hostID string, generation *managedHost) {
	entries := generation.client.Forwards().List()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hosts[hostID] != generation {
		return
	}
	if m.sink != nil {
		m.sink.onForwards(hostID, forwardEntriesToViews(hostID, entries))
	}
}

const serveForwardName = "serve"

func (m *desktopRemoteManager) EnsureServer(ctx context.Context, hostID, workspace string) (RemoteServerView, string, error) {
	mh := m.managed(hostID)
	if mh == nil || mh.client == nil {
		return RemoteServerView{}, "", fmt.Errorf("host %q is not connected", hostID)
	}
	// Serialize per-host so two concurrent EnsureServer calls cannot both miss
	// the state and launch duplicate/orphan serve processes.
	mh.serveMu.Lock()
	defer mh.serveMu.Unlock()
	m.mu.Lock()
	if m.hosts[hostID] != mh {
		m.mu.Unlock()
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	previousServer := mh.server
	m.mu.Unlock()
	c := mh.client
	opCtx, cancel := managedOperationContext(ctx, mh)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return RemoteServerView{}, "", err
	}
	entry, _ := cfg.RemoteHost(hostID)
	starting := RemoteServerView{HostID: hostID, Workspace: workspace, State: "starting"}
	if !m.publishServerIfCurrent(hostID, mh, starting, "") {
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	res, err := m.ensureServe(opCtx, c, bootstrap.Options{
		Workspace:   workspace,
		Install:     entry.ServeInstallMode(),
		LocalBinary: m.localBinary(),
		LocalGOOS:   runtime.GOOS,
		LocalGOARCH: runtime.GOARCH,
		MinVersion:  bootstrap.MinServeVersion,
		Progress: func(step, detail string) {
			view := RemoteServerView{HostID: hostID, Workspace: workspace, State: step, Message: detail}
			m.publishServerIfCurrent(hostID, mh, view, "")
		},
	})
	if err != nil {
		view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: err.Error()}
		m.publishServerIfCurrent(hostID, mh, view, "")
		return view, "", err
	}
	if !m.isCurrent(hostID, mh) {
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	if res.Reused && previousServer.State == "ready" && previousServer.Workspace == workspace &&
		hasUsableServeForward(c.Forwards().List(), res.State.Addr, previousServer.LocalURL) {
		if !m.publishServerIfCurrent(hostID, mh, previousServer, res.Token) {
			return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
		}
		return previousServer, res.Token, nil
	}
	// Start the replacement before retiring the old tunnel. If binding fails,
	// the previous ready server stays usable instead of leaving a dead gap.
	bound, ferr := c.Forwards().Replace(forward.Spec{
		Name: serveForwardName, Direction: forward.Local, BindAddr: "127.0.0.1:0", TargetAddr: res.State.Addr,
	})
	if ferr != nil {
		if !res.Reused {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = m.stopServe(cleanupCtx, c, workspace)
			cleanupCancel()
		}
		view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: ferr.Error()}
		m.publishServerIfCurrent(hostID, mh, view, "")
		return view, "", ferr
	}
	localURL := fmt.Sprintf("http://%s/", bound)
	view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "ready", LocalURL: localURL}
	if !m.publishServerIfCurrent(hostID, mh, view, res.Token) {
		_ = c.Forwards().Remove(serveForwardName)
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	return view, res.Token, nil
}

func (m *desktopRemoteManager) StopServer(hostID string) error {
	mh := m.managed(hostID)
	if mh == nil || mh.client == nil {
		return fmt.Errorf("host %q is not connected", hostID)
	}
	mh.serveMu.Lock()
	defer mh.serveMu.Unlock()
	if !m.isCurrent(hostID, mh) {
		return fmt.Errorf("host %q connection was replaced", hostID)
	}
	c := mh.client
	m.mu.Lock()
	ws := mh.server.Workspace
	m.mu.Unlock()
	if strings.TrimSpace(ws) == "" {
		return fmt.Errorf("host %q has no managed server workspace", hostID)
	}
	opCtx, cancel := managedOperationContext(context.Background(), mh)
	defer cancel()
	if err := m.stopServe(opCtx, c, ws); err != nil {
		return err
	}
	// Tear down the local serve tunnel so a stale forward can't linger.
	_ = c.Forwards().Remove(serveForwardName)
	view := RemoteServerView{HostID: hostID, Workspace: ws, State: "stopped"}
	m.publishServerIfCurrent(hostID, mh, view, "")
	return nil
}

// managed returns the managed host record for hostID, or nil.
func (m *desktopRemoteManager) managed(hostID string) *managedHost {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts[hostID]
}

func (m *desktopRemoteManager) ServerStatus(hostID string) RemoteServerView {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mh := m.hosts[hostID]; mh != nil {
		return mh.server
	}
	return RemoteServerView{HostID: hostID, State: "stopped"}
}

func (m *desktopRemoteManager) ServerLogs(ctx context.Context, hostID string, tailLines int) (string, error) {
	m.mu.Lock()
	mh := m.hosts[hostID]
	ws := ""
	if mh != nil {
		ws = mh.server.Workspace
	}
	m.mu.Unlock()
	if mh == nil || mh.client == nil {
		return "", fmt.Errorf("host %q is not connected", hostID)
	}
	if strings.TrimSpace(ws) == "" {
		return "", fmt.Errorf("host %q has no managed server workspace", hostID)
	}
	opCtx, cancel := managedOperationContext(ctx, mh)
	defer cancel()
	var sb strings.Builder
	if err := m.serveLogs(opCtx, mh.client, ws, tailLines, &sb); err != nil {
		return "", err
	}
	if !m.isCurrent(hostID, mh) {
		return "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	return sb.String(), nil
}

func (m *desktopRemoteManager) Close() error {
	m.mu.Lock()
	hosts := m.hosts
	m.hosts = map[string]*managedHost{}
	answers := make([]chan bool, 0, len(hosts))
	secretAnswers := make([]chan remoteSecretAnswer, 0, len(hosts))
	for _, mh := range hosts {
		if mh.fpAnswer != nil {
			answers = append(answers, mh.fpAnswer)
			mh.fpAnswer = nil
		}
		if mh.secretAnswer != nil {
			secretAnswers = append(secretAnswers, mh.secretAnswer)
			mh.secretAnswer = nil
		}
	}
	m.mu.Unlock()
	for _, answer := range answers {
		select {
		case answer <- false:
		default:
		}
	}
	for _, answer := range secretAnswers {
		select {
		case answer <- remoteSecretAnswer{}:
		default:
		}
	}
	for _, mh := range hosts {
		closeManagedHost(mh)
	}
	return nil
}

func managedOperationContext(parent context.Context, mh *managedHost) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	stop := func() bool { return false }
	if mh != nil && mh.ctx != nil {
		stop = context.AfterFunc(mh.ctx, cancel)
	}
	return ctx, func() {
		stop()
		cancel()
	}
}

func (m *desktopRemoteManager) publishServerIfCurrent(hostID string, generation *managedHost, view RemoteServerView, token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hosts[hostID] != generation {
		return false
	}
	generation.server = view
	generation.token = token
	if m.sink != nil {
		m.sink.onServer(view)
	}
	return true
}

func hasUsableServeForward(entries []forward.Entry, targetAddr, localURL string) bool {
	for _, entry := range entries {
		if entry.Spec.Name == serveForwardName && entry.Up && entry.Spec.TargetAddr == targetAddr && entry.BoundAddr != "" {
			return localURL == fmt.Sprintf("http://%s/", entry.BoundAddr)
		}
	}
	return false
}

func desktopCLIBinaryPath() string {
	name := "reasonix"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
	}
	if found, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, found)
	}
	for _, candidate := range candidates {
		st, err := os.Stat(candidate)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if runtime.GOOS != "windows" && st.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

func desktopNormalizeBind(bind string) string {
	bind = strings.TrimSpace(bind)
	if !strings.Contains(bind, ":") {
		return net.JoinHostPort("127.0.0.1", bind)
	}
	return bind
}

// ── helpers ──

func preserveRemoteHostHiddenFields(entry *config.RemoteHostEntry, existing config.RemoteHostEntry) {
	entry.PassphraseEnv = existing.PassphraseEnv
	entry.PasswordEnv = existing.PasswordEnv
	entry.Forwards = append([]config.RemoteForwardEntry(nil), existing.Forwards...)
}

// Importing an already-managed SSH alias refreshes only its OpenSSH lookup
// fields. Reasonix-specific workspace and bootstrap policy remain user-owned.
func preserveRemoteHostImportSettings(entry *config.RemoteHostEntry, existing config.RemoteHostEntry) {
	entry.Workspace = existing.Workspace
	entry.ServeInstall = existing.ServeInstall
}

// applyRemoteCredentialInput maps plaintext received from the one-shot Wails
// call into Reasonix-owned credential slots. Blank fields preserve the current
// reference; explicit clear flags remove only slots that this desktop created.
func applyRemoteCredentialInput(entry *config.RemoteHostEntry, in RemoteHostInput) (changes []config.CredentialChange, removalCandidates []string) {
	if in.ClearPassword {
		if config.IsGeneratedRemoteCredential(entry.Name, entry.PasswordEnv) {
			removalCandidates = append(removalCandidates, entry.PasswordEnv)
		}
		entry.PasswordEnv = ""
	}
	if in.Password != "" {
		entry.PasswordEnv = config.RemotePasswordCredentialEnvName(entry.Name)
		changes = append(changes, config.CredentialChange{Key: entry.PasswordEnv, Value: in.Password})
	}

	if in.ClearPassphrase {
		if config.IsGeneratedRemoteCredential(entry.Name, entry.PassphraseEnv) {
			removalCandidates = append(removalCandidates, entry.PassphraseEnv)
		}
		entry.PassphraseEnv = ""
	}
	if in.KeyPassphrase != "" {
		entry.PassphraseEnv = config.RemotePassphraseCredentialEnvName(entry.Name)
		changes = append(changes, config.CredentialChange{Key: entry.PassphraseEnv, Value: in.KeyPassphrase})
	}
	return changes, removalCandidates
}

func hostEntryToView(h config.RemoteHostEntry) RemoteHostView {
	return RemoteHostView{
		ID: h.Name, Label: h.Name, Host: h.Host, Port: h.Port, User: h.User,
		IdentityFile: h.IdentityFile, ProxyJump: h.ProxyJump,
		DefaultWorkspace: h.Workspace, ServeInstall: h.ServeInstallMode(), UseSSHConfig: h.UseSSHConfig,
		PasswordSet:      config.ResolveCredential(h.PasswordEnv).Set,
		KeyPassphraseSet: config.ResolveCredential(h.PassphraseEnv).Set,
	}
}

func inputToHostEntry(in RemoteHostInput) config.RemoteHostEntry {
	name := strings.TrimSpace(in.Label)
	return config.RemoteHostEntry{
		Name: name, Host: in.Host, Port: in.Port, User: in.User,
		IdentityFile: in.IdentityFile, ProxyJump: in.ProxyJump,
		Workspace: in.DefaultWorkspace, ServeInstall: in.ServeInstall, UseSSHConfig: in.UseSSHConfig,
	}
}

func forwardEntriesToViews(hostID string, entries []forward.Entry) []RemoteForwardView {
	out := make([]RemoteForwardView, 0, len(entries))
	for _, e := range entries {
		state := "active"
		if !e.Up {
			state = "error"
		}
		v := RemoteForwardView{
			ID: e.Spec.Name, HostID: hostID, Label: e.Spec.Name, State: state,
		}
		if e.LastErr != nil {
			v.Error = e.LastErr.Error()
		}
		out = append(out, v)
	}
	return out
}

func statusString(s remote.Status) string {
	switch s {
	case remote.StatusConnecting:
		return "connecting"
	case remote.StatusConnected:
		return "connected"
	case remote.StatusReconnecting:
		return "reconnecting"
	case remote.StatusDegraded:
		return "degraded"
	case remote.StatusStopped:
		return "stopped"
	default:
		return "stopped"
	}
}
