package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/config"
	"reasonix/internal/jobs"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/remote/forward"
	"reasonix/internal/store"
)

const remoteProviderReloadTimeout = jobs.DefaultTeardownGrace + 15*time.Second

// SwitchCredentialProxyModel stages an immutable desktop proxy route and a
// matching remote provider credential, then asks Serve to perform its ordinary
// active-work-gated controller switch. The outgoing controller keeps its old
// virtual token and route throughout; if Serve refuses or cannot rebuild, the
// on-disk provider is rolled back and the live controller remains coherent.
func (m *desktopRemoteManager) SwitchCredentialProxyModel(ctx context.Context, hostID, workspace, currentRef, nextRef, expectedPath string) error {
	hostID, workspace = strings.TrimSpace(hostID), strings.TrimSpace(workspace)
	currentRef, nextRef = strings.TrimSpace(currentRef), strings.TrimSpace(nextRef)
	if hostID == "" || workspace == "" || currentRef == "" || nextRef == "" {
		return fmt.Errorf("credential proxy model switch: host, workspace, current model, and next model are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mh := m.managed(hostID)
	if mh == nil || mh.client == nil {
		return fmt.Errorf("host %q is not connected", hostID)
	}
	mh.serveMu.Lock()
	defer mh.serveMu.Unlock()
	if !m.isCurrent(hostID, mh) {
		return fmt.Errorf("host %q connection was replaced", hostID)
	}
	m.mu.Lock()
	serve := mh.serves[workspace]
	m.mu.Unlock()
	if serve == nil || serve.view.State != "ready" || serve.view.LocalURL == "" || serve.token == "" {
		return fmt.Errorf("workspace %q has no ready Reasonix Serve", workspace)
	}
	app, ok := m.sink.(*App)
	if !ok || app == nil {
		return fmt.Errorf("credential proxy: app unavailable")
	}
	oldRoute, err := app.applyCredentialProxyModel(hostID, workspace, currentRef)
	if err != nil {
		return err
	}
	newRoute, err := app.applyCredentialProxyModel(hostID, workspace, nextRef)
	if err != nil {
		return err
	}
	remotePort, err := ensureCredentialProxyForward(mh.client, hostID, newRoute.port)
	if err != nil {
		return fmt.Errorf("credential proxy: reverse tunnel: %w", err)
	}
	oldOptions := credentialProxyBootstrapOptions(workspace, remotePort, oldRoute)
	newOptions := credentialProxyBootstrapOptions(workspace, remotePort, newRoute)
	if _, err := bootstrap.EnsureCredentialProvider(ctx, mh.client, newOptions); err != nil {
		return err
	}
	rollback := func(cause error) error {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, rollbackErr := bootstrap.EnsureCredentialProvider(rollbackCtx, mh.client, oldOptions)
		if rollbackErr != nil {
			return errors.Join(cause, fmt.Errorf("restore previous credential proxy model: %w", rollbackErr))
		}
		return cause
	}
	client, err := newServeHTTPClient(serve.view.LocalURL)
	if err != nil {
		return rollback(err)
	}
	if err := serveHandshake(ctx, client, serve.view.LocalURL, serve.token); err != nil {
		return rollback(err)
	}
	body, _ := json.Marshal(map[string]string{"ref": newOptions.Provider + "/" + newOptions.Model})
	if err := servePostForSession(ctx, client, serveURL(serve.view.LocalURL, "/model"), body, expectedPath); err != nil {
		return rollback(err)
	}
	return nil
}

func credentialProxyBootstrapOptions(workspace string, remotePort int, info credentialProxyRouteInfo) *bootstrap.CredentialProxyOptions {
	slug := store.RemoteWorkspaceSlug(workspace)
	suffix := slug[len(slug)-16:]
	return &bootstrap.CredentialProxyOptions{
		BaseURL:  fmt.Sprintf("http://127.0.0.1:%d", remotePort),
		Token:    info.token,
		TokenEnv: "REASONIX_PROXY_TOKEN_" + strings.ToUpper(suffix),
		Provider: credentialProxyProviderName + "-" + suffix,
		Model:    info.model,
		Kind:     info.kind,
	}
}

// serveForwardName derives the per-workspace local tunnel name from the same
// collision-proof slug the remote state files use (store.RemoteWorkspaceSlug),
// so one host holds one independent forward per workspace.
func serveForwardName(workspace string) string {
	return "serve-" + store.RemoteWorkspaceSlug(workspace)
}

func (m *desktopRemoteManager) EnsureServer(ctx context.Context, hostID, workspace string) (RemoteServerView, string, error) {
	hostID, workspace = strings.TrimSpace(hostID), strings.TrimSpace(workspace)
	if hostID == "" || workspace == "" {
		return RemoteServerView{}, "", fmt.Errorf("remote serve: host and workspace are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
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
	var previousServer RemoteServerView
	previousToken := ""
	previousAddr := ""
	if e := mh.serves[workspace]; e != nil {
		previousServer, previousToken, previousAddr = e.view, e.token, e.addr
	}
	m.mu.Unlock()
	c := mh.client
	if m.readyServeReusable(ctx, c, mh, hostID, workspace, previousServer, previousToken, previousAddr) {
		m.startCredentialWatchdogIfEnabled(mh, hostID, workspace)
		return previousServer, previousToken, nil
	}
	opCtx, cancel := managedOperationContext(ctx, mh)
	defer cancel()

	entry, err := configuredRemoteHost(hostID)
	if err != nil {
		return RemoteServerView{}, "", err
	}
	starting := RemoteServerView{HostID: hostID, Workspace: workspace, State: "starting"}
	if !m.publishServerIfCurrent(hostID, mh, starting, "", "") {
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	// Local-proxy credential mode: start the desktop key holder, open the
	// reverse tunnel, and hand the bootstrap the virtual token + provider
	// entry to install on the remote. The real key never leaves this machine.
	credOpts, err := m.credentialOptions(c, hostID, workspace, entry)
	if err != nil {
		view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: err.Error()}
		m.publishFailedServeStart(hostID, mh, previousServer, previousToken, previousAddr, view)
		return view, "", err
	}
	res, err := m.ensureServe(opCtx, c, bootstrap.Options{
		Workspace:       workspace,
		Install:         entry.ServeInstallMode(),
		LocalBinary:     m.localBinary(),
		LocalGOOS:       runtime.GOOS,
		LocalGOARCH:     runtime.GOARCH,
		ProductVersion:  version,
		FetchBinary:     m.fetchRemoteBinary,
		MinVersion:      bootstrap.MinServeVersion,
		CredentialProxy: credOpts,
		Progress: func(step, detail string) {
			view := RemoteServerView{HostID: hostID, Workspace: workspace, State: step, Message: detail}
			m.publishServerIfCurrent(hostID, mh, view, "", "")
		},
	})
	if err != nil {
		view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: err.Error()}
		m.publishFailedServeStart(hostID, mh, previousServer, previousToken, previousAddr, view)
		return view, "", err
	}
	if !m.isCurrent(hostID, mh) {
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	if res.Reused && previousServer.State == "ready" && previousServer.Workspace == workspace &&
		hasUsableServeForward(c.Forwards().List(), serveForwardName(workspace), res.State.Addr, previousServer.LocalURL) {
		previousServer.InstanceID = remoteServeInstanceID(res.State)
		if !m.publishServerIfCurrent(hostID, mh, previousServer, res.Token, res.State.Addr) {
			return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
		}
		return m.finishCredentialServe(opCtx, c, mh, hostID, workspace, previousServer, res.Token, res, entry.CredentialProxyEnabled())
	}
	// Start the replacement before retiring the old tunnel. If binding fails,
	// the previous ready server stays usable instead of leaving a dead gap.
	bound, ferr := c.Forwards().Replace(forward.Spec{
		Name: serveForwardName(workspace), Direction: forward.Local, BindAddr: "127.0.0.1:0", TargetAddr: res.State.Addr,
	})
	if ferr != nil {
		if !res.Reused {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = m.stopServe(cleanupCtx, c, workspace)
			cleanupCancel()
		}
		view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: ferr.Error()}
		m.publishFailedServeStart(hostID, mh, previousServer, previousToken, previousAddr, view)
		return view, "", ferr
	}
	localURL := fmt.Sprintf("http://%s/", bound)
	view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "ready", LocalURL: localURL, InstanceID: remoteServeInstanceID(res.State)}
	if !m.publishServerIfCurrent(hostID, mh, view, res.Token, res.State.Addr) {
		_ = c.Forwards().Remove(serveForwardName(workspace))
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	return m.finishCredentialServe(opCtx, c, mh, hostID, workspace, view, res.Token, res, entry.CredentialProxyEnabled())
}

func remoteServeInstanceID(state bootstrap.ServeState) string {
	if state.PID == 0 && state.StartedAt == 0 && strings.TrimSpace(state.Addr) == "" {
		return ""
	}
	return fmt.Sprintf("%d:%d:%s", state.PID, state.StartedAt, strings.TrimSpace(state.Addr))
}

func configuredRemoteHost(hostID string) (config.RemoteHostEntry, error) {
	cfg, err := config.Load()
	if err != nil {
		return config.RemoteHostEntry{}, err
	}
	entry, ok := cfg.RemoteHost(hostID)
	if !ok {
		return config.RemoteHostEntry{}, fmt.Errorf("remote host %q is no longer configured", hostID)
	}
	return entry, nil
}

func (m *desktopRemoteManager) credentialOptions(c desktopSSHClient, hostID, workspace string, entry config.RemoteHostEntry) (*bootstrap.CredentialProxyOptions, error) {
	if !entry.CredentialProxyEnabled() {
		return nil, nil
	}
	return m.credentialProxySetup(c, hostID, workspace)
}

// readyServeReusable validates the serve forward and credential channel.
// Config read failures fail closed into the full ensure path.
func (m *desktopRemoteManager) readyServeReusable(ctx context.Context, c desktopSSHClient, mh *managedHost, hostID, workspace string, view RemoteServerView, token, addr string) bool {
	if view.State != "ready" || view.LocalURL == "" || token == "" || addr == "" ||
		!hasUsableServeForward(c.Forwards().List(), serveForwardName(workspace), addr, view.LocalURL) ||
		!serveTunnelAlive(ctx, view.LocalURL) {
		return false
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	host, ok := cfg.RemoteHost(hostID)
	if !ok || !host.CredentialProxyEnabled() {
		return true
	}
	port, hasForward := credentialForwardPort(c, hostID)
	healedPort := int(mh.credPort.Load())
	if !hasForward || healedPort != port || probeReverseTunnel(c, port) != nil {
		log.Printf("[remote] EnsureServer: FAST-REUSE blocked (credential channel) host=%s ws=%s port=%d has=%v healedPort=%d", hostID, workspace, port, hasForward, healedPort)
		return false
	}
	return true
}

// healCredentialChannel runs after ensure when the forward is live. A reconnect
// can move its remote port, so heal every tracked workspace config before any
// Serve reloads providers, then verify the channel before recording the port.
func (m *desktopRemoteManager) healCredentialChannel(ctx context.Context, c desktopSSHClient, mh *managedHost, hostID, workspace, base, token string, res bootstrap.Result) error {
	port, has := credentialForwardPort(c, hostID)
	if !has {
		return fmt.Errorf("credential proxy: reverse tunnel is not available")
	}
	if int(mh.credPort.Load()) != port || res.CredentialConfigChanged {
		log.Printf("[remote] EnsureServer: cred port drift host=%s old=%d new=%d configChanged=%v -> reloading serve providers", hostID, mh.credPort.Load(), port, res.CredentialConfigChanged)
		workspaces := m.trackedCredentialWorkspaces(hostID, workspace)
		// base+token covers the Serve ensured by this round even if it has not
		// reached the registry yet; tracked peers are reloaded alongside it.
		if err := healCredentialConfigsBeforeReload(ctx, workspaces,
			func(workspace string) (*bootstrap.CredentialProxyOptions, error) {
				return m.credentialProxySetup(c, hostID, workspace)
			},
			func(ctx context.Context, opts *bootstrap.CredentialProxyOptions) error {
				_, err := bootstrap.HealCredentialProvider(ctx, c, opts)
				return err
			},
			func() bool { return m.reloadServeProviders(ctx, mh, hostID, workspace, base, token) },
		); err != nil {
			return err
		}
	}
	if perr := probeReverseTunnel(c, port); perr != nil {
		log.Printf("[remote] EnsureServer: reverse probe FAILED host=%s ws=%s port=%d err=%v", hostID, workspace, port, perr)
		return fmt.Errorf("credential proxy: reverse tunnel health check: %w", perr)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hosts[hostID] != mh {
		return fmt.Errorf("host %q connection was replaced", hostID)
	}
	mh.credPort.Store(int64(port))
	return nil
}

// serveTunnelAlive probes the local serve forward: any HTTP response —
// the remote serve behind it are answering.
func serveTunnelAlive(ctx context.Context, localURL string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, serveURL(localURL, "/"), nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func (m *desktopRemoteManager) StopServer(hostID, workspace string) error {
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
	_, tracked := mh.serves[workspace]
	m.mu.Unlock()
	if !tracked {
		return fmt.Errorf("host %q has no managed server for workspace %q", hostID, workspace)
	}
	opCtx, cancel := managedOperationContext(context.Background(), mh)
	defer cancel()
	if err := m.stopServe(opCtx, c, workspace); err != nil {
		return err
	}
	// Tear down the local serve tunnel so a stale forward can't linger.
	_ = c.Forwards().Remove(serveForwardName(workspace))
	view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "stopped"}
	m.publishServerIfCurrent(hostID, mh, view, "", "")
	return nil
}

// managed returns the managed host record for hostID, or nil.
func (m *desktopRemoteManager) managed(hostID string) *managedHost {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts[hostID]
}

func (m *desktopRemoteManager) ServerStatus(hostID, workspace string) RemoteServerView {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mh := m.hosts[hostID]; mh != nil {
		if e := mh.serves[workspace]; e != nil {
			return e.view
		}
	}
	return RemoteServerView{HostID: hostID, Workspace: workspace, State: "stopped"}
}

// ServeSnapshot is the read-only resolution for callers that want to talk to
// an already-running serve without waking one: it returns the registry's view
// and token only when the recorded state is ready with a usable URL. Query
// paths (session listing) must go through this — a full EnsureServer from a
// poll serializes behind tab bootstraps on the per-host lock and starves them.
func (m *desktopRemoteManager) ServeSnapshot(hostID, workspace string) (RemoteServerView, string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mh := m.hosts[hostID]
	if mh == nil {
		return RemoteServerView{}, "", false
	}
	e := mh.serves[workspace]
	if e == nil || e.view.State != "ready" || e.view.LocalURL == "" || e.token == "" {
		return RemoteServerView{}, "", false
	}
	return e.view, e.token, true
}

func (m *desktopRemoteManager) ServerLogs(ctx context.Context, hostID, workspace string, tailLines int) (string, error) {
	m.mu.Lock()
	mh := m.hosts[hostID]
	tracked := false
	if mh != nil {
		_, tracked = mh.serves[workspace]
	}
	m.mu.Unlock()
	if mh == nil || mh.client == nil {
		return "", fmt.Errorf("host %q is not connected", hostID)
	}
	if !tracked {
		return "", fmt.Errorf("host %q has no managed server for workspace %q", hostID, workspace)
	}
	opCtx, cancel := managedOperationContext(ctx, mh)
	defer cancel()
	var sb strings.Builder
	if err := m.serveLogs(opCtx, mh.client, workspace, tailLines, &sb); err != nil {
		return "", err
	}
	if !m.isCurrent(hostID, mh) {
		return "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	return sb.String(), nil
}

// credentialProxySetup prepares local-proxy credential mode for one
// workspace: starts the desktop key holder, registers every tracked workspace
// against the desktop's current default model, and opens the reverse tunnel the
// remote serve will call through. The returned options are ready to hand to
// the bootstrap (BaseURL points at the tunnel's remote loopback port).
func (m *desktopRemoteManager) credentialProxySetup(c desktopSSHClient, hostID, workspace string) (*bootstrap.CredentialProxyOptions, error) {
	app, ok := m.sink.(*App)
	if !ok || app == nil {
		return nil, fmt.Errorf("credential proxy: app unavailable")
	}
	info, err := m.registerTrackedCredentialRoutes(app, hostID, workspace)
	if err != nil {
		return nil, err
	}
	remotePort, err := ensureCredentialProxyForward(c, hostID, info.port)
	if err != nil {
		return nil, fmt.Errorf("credential proxy: reverse tunnel: %w", err)
	}
	slug := store.RemoteWorkspaceSlug(workspace)
	suffix := slug[len(slug)-16:]
	return &bootstrap.CredentialProxyOptions{
		BaseURL:  fmt.Sprintf("http://127.0.0.1:%d", remotePort),
		Token:    info.token,
		TokenEnv: "REASONIX_PROXY_TOKEN_" + strings.ToUpper(suffix),
		Provider: credentialProxyProviderName + "-" + suffix,
		Model:    info.model,
		Kind:     info.kind,
	}, nil
}

func (m *desktopRemoteManager) registerTrackedCredentialRoutes(app *App, hostID, workspace string) (credentialProxyRouteInfo, error) {
	workspaces := m.trackedCredentialWorkspaces(hostID, workspace)
	var info credentialProxyRouteInfo
	for index, trackedWorkspace := range workspaces {
		registered, err := app.registerCredentialProxyRoute(hostID, trackedWorkspace)
		if err != nil {
			return credentialProxyRouteInfo{}, err
		}
		if index == 0 {
			info = registered
		}
	}
	return info, nil
}

func (m *desktopRemoteManager) trackedCredentialWorkspaces(hostID, workspace string) []string {
	workspaces := []string{workspace}
	m.mu.Lock()
	if managed := m.hosts[hostID]; managed != nil {
		peers := make([]string, 0, len(managed.serves))
		for peer := range managed.serves {
			if peer != workspace {
				peers = append(peers, peer)
			}
		}
		sort.Strings(peers)
		workspaces = append(workspaces, peers...)
	}
	m.mu.Unlock()
	return workspaces
}

// ensureCredentialProxyForward opens (idempotently) the reverse tunnel: the
// REMOTE binds an ephemeral loopback port (avoids conflicts with stale
// listeners from half-dead sessions) and forwards back through SSH to the
// desktop proxy. Returns the actually bound remote port.
func ensureCredentialProxyForward(c desktopSSHClient, hostID string, desktopPort int) (int, error) {
	name := "cred-proxy:" + hostID
	target := fmt.Sprintf("127.0.0.1:%d", desktopPort)
	for _, f := range c.Forwards().List() {
		if f.Spec.Name == name && f.Spec.TargetAddr == target && f.Up {
			if port, ok := portOfAddr(f.BoundAddr); ok {
				return port, nil
			}
		}
	}
	bound, err := c.Forwards().Replace(forward.Spec{
		Name:       name,
		Direction:  forward.Remote,
		BindAddr:   "127.0.0.1:0",
		TargetAddr: target,
	})
	if err != nil {
		return 0, err
	}
	port, ok := portOfAddr(bound)
	if !ok {
		return 0, fmt.Errorf("reverse tunnel bound unexpected address %q", bound)
	}
	return port, nil
}

// credentialForwardPort reports the remote-side port of the host's reverse
// credential forward, when one is up. This is the port the remote serve's
// provider config must point at.
func credentialForwardPort(c desktopSSHClient, hostID string) (int, bool) {
	name := "cred-proxy:" + hostID
	for _, f := range c.Forwards().List() {
		if f.Spec.Name == name && f.Up {
			if port, ok := portOfAddr(f.BoundAddr); ok {
				return port, true
			}
		}
	}
	return 0, false
}

// probeReverseTunnel verifies the reverse credential channel end to end. The
// desktop cannot dial the remote-side loopback listener itself, so the probe
// rides the same SSH connection as a direct-tcpip channel — the remote sshd
// connects to its own 127.0.0.1:<port>, which forwards back through the
// tunnel to the desktop credential proxy. Any HTTP response to /healthz
// proves the whole chain; a refused dial or timeout means the channel the
// serve depends on is dead.
func probeReverseTunnel(c desktopSSHClient, port int) error {
	type sshDialer interface{ SSH() (*ssh.Client, error) }
	d, ok := c.(sshDialer)
	if !ok {
		return errors.New("reverse probe: ssh client does not expose a raw connection")
	}
	cl, err := d.SSH()
	if err != nil {
		return fmt.Errorf("reverse probe: no ssh connection: %w", err)
	}
	conn, err := cl.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("reverse probe: remote dial 127.0.0.1:%d: %w", port, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n")); err != nil {
		return fmt.Errorf("reverse probe: write: %w", err)
	}
	statusLine, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("reverse probe: read: %w", err)
	}
	if !strings.Contains(statusLine, "HTTP/") {
		return fmt.Errorf("reverse probe: non-HTTP response %q", strings.TrimSpace(statusLine))
	}
	return nil
}

// reloadServeProviders asks every running serve on the host to rebuild its
// providers: POST /providers/reload rebinds the controller with the CURRENT
// model, re-reading the healed config. Needed after a reverse credential
// tunnel rebind — a reused serve otherwise keeps dialing the dead old port.
// workspace+extraBase+extraToken target the serve this ensure round just
// produced (the registry may not list it yet); registry entries cover the
// host's other workspaces, which share the same healed config. Returns false
// when any serve could not reload (busy turn, or a serve too old to know the
// endpoint): callers keep their heal gate closed so the next ensure retries.
func (m *desktopRemoteManager) reloadServeProviders(ctx context.Context, generation *managedHost, hostID, workspace, extraBase, extraToken string) bool {
	type target struct{ base, token string }
	m.mu.Lock()
	mh := m.hosts[hostID]
	if mh == nil || mh != generation {
		m.mu.Unlock()
		return false
	}
	targets := make(map[string]target, len(mh.serves)+1)
	for ws, e := range mh.serves {
		if e != nil && e.view.LocalURL != "" && e.token != "" {
			targets[ws] = target{e.view.LocalURL, e.token}
		}
	}
	m.mu.Unlock()
	if extraBase != "" && extraToken != "" && workspace != "" {
		// The just-ensured serve takes precedence under its REAL workspace
		// key; drop any registry entry pointing at the same base so it is
		// not reloaded twice.
		targets[workspace] = target{extraBase, extraToken}
		for ws, t := range targets {
			if ws != workspace && t.base == extraBase {
				delete(targets, ws)
			}
		}
	}
	allOK := true
	for ws, t := range targets {
		jar, err := cookiejar.New(nil)
		if err != nil {
			allOK = false
			continue
		}
		client := &http.Client{Jar: jar}
		callCtx, cancel := context.WithTimeout(ctx, remoteProviderReloadTimeout)
		err = serveHandshake(callCtx, client, t.base, t.token)
		if err == nil {
			err = servePost(callCtx, client, serveURL(t.base, "/providers/reload"), nil)
		}
		cancel()
		if err != nil && strings.Contains(err.Error(), "status 409") {
			err = m.cancelThenReload(ctx, client, t.base, t.token)
		}
		if err != nil {
			log.Printf("[remote] reloadServeProviders: FAILED host=%s ws=%s err=%v", hostID, ws, err)
			allOK = false
			// Legacy serves lack this route; replace them asynchronously because
			// EnsureServer currently holds serveMu.
			if (strings.Contains(err.Error(), "status 404") || strings.Contains(err.Error(), "status 405")) && m.markCredFallback(hostID, ws) {
				log.Printf("[remote] reloadServeProviders: legacy serve -> replacing host=%s ws=%s", hostID, ws)
				go func(hostID, ws string) {
					if err := m.StopServer(hostID, ws); err != nil {
						log.Printf("[remote] reloadServeProviders: stop legacy serve failed host=%s ws=%s err=%v", hostID, ws, err)
					}
					if _, _, err := m.EnsureServer(context.Background(), hostID, ws); err != nil {
						// EnsureServer errors can carry credential-configuration context.
						// Keep logs useful for correlation without persisting that detail.
						log.Printf("[remote] reloadServeProviders: restart legacy serve failed host=%s ws=%s", hostID, ws)
					}
				}(hostID, ws)
			}
			continue
		}
	}
	return allOK
}

// cancelThenReload breaks the credential-heal deadlock: after tunnel drift a
// busy turn is already doomed against the stale port, so cancel it and retry
// the provider rebuild with bounded backoff.
func (m *desktopRemoteManager) cancelThenReload(ctx context.Context, client *http.Client, base, token string) error {
	log.Printf("[remote] reloadServeProviders: busy turn blocks reload -> canceling turn base=%s", base)
	cancelCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	cancelErr := servePost(cancelCtx, client, serveURL(base, "/cancel"), nil)
	cancel()
	if cancelErr != nil {
		log.Printf("[remote] reloadServeProviders: cancel busy turn FAILED err=%v", cancelErr)
	}
	jobsCtx, jobsCancel := context.WithTimeout(ctx, 5*time.Second)
	jobsErr := cancelServeBackgroundJobs(jobsCtx, client, base)
	jobsCancel()
	if jobsErr != nil {
		log.Printf("[remote] reloadServeProviders: cancel background jobs FAILED err=%v", jobsErr)
	}
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		timer := time.NewTimer(time.Duration(attempt) * time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
		callCtx, callCancel := context.WithTimeout(ctx, remoteProviderReloadTimeout)
		err = serveHandshake(callCtx, client, base, token)
		if err == nil {
			err = servePost(callCtx, client, serveURL(base, "/providers/reload"), nil)
		}
		callCancel()
		if err == nil || !strings.Contains(err.Error(), "status 409") {
			return err
		}
	}
	return err
}

func cancelServeBackgroundJobs(ctx context.Context, client *http.Client, base string) error {
	status, err := serveGet(ctx, client, serveURL(base, "/status?runtime=1"))
	if err != nil {
		return err
	}
	var payload struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(status, &payload); err != nil {
		return fmt.Errorf("decode serve jobs: %w", err)
	}
	ids := make([]string, 0, len(payload.Jobs))
	for _, job := range payload.Jobs {
		if id := strings.TrimSpace(job.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		return err
	}
	return servePost(ctx, client, serveURL(base, "/jobs/cancel"), body)
}

// markCredFallback rate-limits the legacy-serve replacement to at most one
// attempt every two minutes per host and workspace.
func (m *desktopRemoteManager) markCredFallback(hostID, workspace string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	mh := m.hosts[hostID]
	if mh == nil {
		return false
	}
	now := time.Now().Unix()
	if mh.credFallbackAt == nil {
		mh.credFallbackAt = map[string]int64{}
	}
	last := mh.credFallbackAt[workspace]
	if last != 0 && now-last < 120 {
		return false
	}
	mh.credFallbackAt[workspace] = now
	return true
}

func portOfAddr(addr string) (int, bool) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return 0, false
	}
	return port, true
}
