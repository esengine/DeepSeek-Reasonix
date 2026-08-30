package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"reasonix/internal/remote/bootstrap"
)

type credentialWatchdog struct {
	mu        sync.Mutex
	cancel    context.CancelFunc
	workspace string
}

func (w *credentialWatchdog) stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.cancel, w.workspace = nil, ""
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// credentialChannelDecision keeps the health policy testable without SSH.
type credentialChannelDecision struct {
	HasForward  bool
	ForwardPort int
	HealedPort  int
	ProbeOK     bool
}

func (d credentialChannelDecision) needsHeal() bool {
	return !d.HasForward || !d.ProbeOK || d.HealedPort <= 0 || d.ForwardPort != d.HealedPort
}

func credentialWatchdogEligibleState(state string) bool {
	return state == "connected" || state == "degraded"
}

func (m *desktopRemoteManager) startCredentialWatchdogIfEnabled(mh *managedHost, hostID, workspace string) {
	entry, err := configuredRemoteHost(hostID)
	if err == nil && entry.CredentialProxyEnabled() {
		m.startCredentialWatchdog(mh, hostID, workspace)
		return
	}
	if mh != nil {
		mh.credWatch.stop()
	}
}

func (m *desktopRemoteManager) finishCredentialServe(ctx context.Context, c desktopSSHClient, mh *managedHost, hostID, workspace string, view RemoteServerView, token string, res bootstrap.Result, enabled bool) (RemoteServerView, string, error) {
	if !enabled {
		return view, token, nil
	}
	if err := m.healCredentialChannel(ctx, c, mh, hostID, workspace, view.LocalURL, token, res); err != nil {
		failed := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: err.Error()}
		m.publishServerIfCurrent(hostID, mh, failed, "", "")
		return failed, "", err
	}
	m.startCredentialWatchdog(mh, hostID, workspace)
	return view, token, nil
}

// startCredentialWatchdog detects reverse-forward drift while a tab is open.
func (m *desktopRemoteManager) startCredentialWatchdog(mh *managedHost, hostID, workspace string) {
	if mh == nil || !m.isCurrent(hostID, mh) {
		return
	}
	mh.credWatch.mu.Lock()
	mh.credWatch.workspace = workspace
	if mh.credWatch.cancel != nil {
		mh.credWatch.mu.Unlock()
		return
	}
	parent := mh.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	mh.credWatch.cancel = cancel
	mh.credWatch.mu.Unlock()
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if !m.isCurrent(hostID, mh) {
				return
			}
			m.checkCredentialChannel(ctx, mh, hostID)
		}
	}()
}

func (m *desktopRemoteManager) checkCredentialChannel(ctx context.Context, mh *managedHost, hostID string) {
	entry, err := configuredRemoteHost(hostID)
	if err != nil || !entry.CredentialProxyEnabled() {
		mh.credWatch.stop()
		return
	}
	m.mu.Lock()
	state := ""
	if m.hosts[hostID] == mh {
		state = mh.status.State
	}
	m.mu.Unlock()
	// A reconnect with one failed forward is deliberately published as
	// degraded while retaining the live SSH client. That is exactly when the
	// credential reverse tunnel may need this watchdog's repair path.
	if !credentialWatchdogEligibleState(state) || mh.client == nil {
		return
	}
	port, has := credentialForwardPort(mh.client, hostID)
	decision := credentialChannelDecision{HasForward: has, ForwardPort: port, HealedPort: int(mh.credPort.Load()), ProbeOK: has && probeReverseTunnel(mh.client, port) == nil}
	if decision.needsHeal() {
		m.healCredentialChannelWatchdog(ctx, mh, hostID)
	}
}

// healCredentialChannelWatchdog reopens the fast-reuse gate only after the
// forward, remote provider, live Serve providers, and end-to-end probe agree.
func (m *desktopRemoteManager) healCredentialChannelWatchdog(watchCtx context.Context, mh *managedHost, hostID string) {
	mh.credWatch.mu.Lock()
	workspace := mh.credWatch.workspace
	mh.credWatch.mu.Unlock()
	if workspace == "" {
		return
	}
	mh.serveMu.Lock()
	defer mh.serveMu.Unlock()
	if !m.isCurrent(hostID, mh) || mh.client == nil {
		return
	}
	entry, err := configuredRemoteHost(hostID)
	if err != nil || !entry.CredentialProxyEnabled() {
		return
	}
	opCtx, opCancel := managedOperationContext(watchCtx, mh)
	defer opCancel()
	c := mh.client
	workspaces := m.trackedCredentialWorkspaces(hostID, workspace)
	log.Printf("[remote] credential watchdog: channel broken, re-healing host=%s workspaces=%d", hostID, len(workspaces))
	_ = c.Forwards().Remove("cred-proxy:" + hostID)
	healCtx, healCancel := context.WithTimeout(opCtx, credentialProviderHealBudget(len(workspaces)))
	err = healTrackedCredentialProviders(healCtx, workspaces,
		func(workspace string) (*bootstrap.CredentialProxyOptions, error) {
			return m.credentialProxySetup(c, hostID, workspace)
		},
		func(ctx context.Context, opts *bootstrap.CredentialProxyOptions) error {
			_, healErr := bootstrap.HealCredentialProvider(ctx, c, opts)
			return healErr
		},
	)
	healCancel()
	if err != nil {
		log.Printf("[remote] credential watchdog: config heal FAILED host=%s err=%v", hostID, err)
		return
	}
	port, has := credentialForwardPort(c, hostID)
	if !has {
		log.Printf("[remote] credential watchdog: forward missing after setup host=%s", hostID)
		return
	}
	if err := probeReverseTunnel(c, port); err != nil {
		log.Printf("[remote] credential watchdog: probe still FAILED host=%s port=%d err=%v", hostID, port, err)
		return
	}
	if !m.isCurrent(hostID, mh) {
		return
	}
	reloadCtx, reloadCancel := context.WithTimeout(opCtx, credentialProviderReloadBudget(len(workspaces)))
	reloadOK := m.reloadServeProviders(reloadCtx, mh, hostID, workspace, "", "")
	reloadCancel()
	if !reloadOK {
		log.Printf("[remote] credential watchdog: provider reload FAILED host=%s", hostID)
		return
	}
	if !m.isCurrent(hostID, mh) {
		return
	}
	mh.credPort.Store(int64(port))
	log.Printf("[remote] credential watchdog: channel re-healed host=%s port=%d", hostID, port)
}

func credentialProviderReloadBudget(targets int) time.Duration {
	if targets < 1 {
		targets = 1
	}
	return time.Duration(targets) * remoteProviderReloadTimeout
}

func credentialProviderHealBudget(targets int) time.Duration {
	if targets < 1 {
		targets = 1
	}
	return time.Duration(targets) * 30 * time.Second
}

func healTrackedCredentialProviders(ctx context.Context, workspaces []string, setup func(string) (*bootstrap.CredentialProxyOptions, error), heal func(context.Context, *bootstrap.CredentialProxyOptions) error) error {
	for _, workspace := range workspaces {
		opts, err := setup(workspace)
		if err != nil {
			return fmt.Errorf("workspace %q setup: %w", workspace, err)
		}
		if err := heal(ctx, opts); err != nil {
			return fmt.Errorf("workspace %q heal: %w", workspace, err)
		}
	}
	return nil
}

func healCredentialConfigsBeforeReload(ctx context.Context, workspaces []string, setup func(string) (*bootstrap.CredentialProxyOptions, error), heal func(context.Context, *bootstrap.CredentialProxyOptions) error, reload func() bool) error {
	if err := healTrackedCredentialProviders(ctx, workspaces, setup, heal); err != nil {
		return fmt.Errorf("credential proxy: heal tracked provider configs: %w", err)
	}
	if reload == nil || !reload() {
		return fmt.Errorf("credential proxy: serve providers could not reload")
	}
	return nil
}
