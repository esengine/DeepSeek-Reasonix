package main

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/remote/bootstrap"
)

// remoteServeUpdateAvailable reports whether the remote binary is an older
// release than this desktop; dev or unknown versions never prompt.
func remoteServeUpdateAvailable(serveVersion string) bool {
	desktop, derr := bootstrap.ParseVersion(version)
	if derr != nil {
		return false
	}
	remote, rerr := bootstrap.ParseVersion(serveVersion)
	if rerr != nil {
		return false
	}
	return bootstrap.CompareVersions(remote, desktop) < 0
}

// HostConnected reports whether the host still has a live managed
// connection; updates use it to keep a superseding disconnect from being
// undone by tab reattachment.
func (m *desktopRemoteManager) HostConnected(hostID string) bool {
	mh := m.managed(hostID)
	return mh != nil && mh.client != nil
}

// UpdateServer stops the workspace's serve and re-ensures it with
// ForceUpgrade so the remote lands on this desktop's exact release. The App
// layer warns first: in-flight turns on that serve are interrupted.
func (m *desktopRemoteManager) UpdateServer(ctx context.Context, hostID, workspace string) (RemoteServerView, string, error) {
	hostID, workspace = strings.TrimSpace(hostID), strings.TrimSpace(workspace)
	if hostID == "" || workspace == "" {
		return RemoteServerView{}, "", fmt.Errorf("remote serve update: host and workspace are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	mh := m.managed(hostID)
	if mh == nil || mh.client == nil {
		return RemoteServerView{}, "", fmt.Errorf("host %q is not connected", hostID)
	}
	mh.serveMu.Lock()
	defer mh.serveMu.Unlock()
	if !m.isCurrent(hostID, mh) {
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	m.mu.Lock()
	previous, tracked := mh.serves[workspace]
	m.mu.Unlock()
	if !tracked || previous == nil {
		return RemoteServerView{}, "", fmt.Errorf("host %q has no managed server for workspace %q", hostID, workspace)
	}
	opCtx, cancel := managedOperationContext(ctx, mh)
	defer cancel()
	c := mh.client
	updating := RemoteServerView{
		HostID: hostID, Workspace: workspace, State: "updating", Message: "stopping serve",
		ServeVersion: previous.view.ServeVersion, UpdateAvailable: previous.view.UpdateAvailable,
	}
	if !m.publishServerIfCurrent(hostID, mh, updating, previous.token, previous.addr) {
		return RemoteServerView{}, "", fmt.Errorf("host %q connection was replaced", hostID)
	}
	if err := m.stopServe(opCtx, c, workspace); err != nil {
		view := RemoteServerView{HostID: hostID, Workspace: workspace, State: "error", Error: err.Error()}
		// Keep the previous registry token/addr: the serve may still be
		// running, and Stop/Logs must keep reaching it.
		m.publishServerIfCurrent(hostID, mh, view, previous.token, previous.addr)
		return view, "", err
	}
	// Drop the loopback forward so the forced ensure rebuilds it against the
	// replacement serve's address.
	_ = c.Forwards().Remove(serveForwardName(workspace))
	progress := RemoteServerView{
		HostID: hostID, Workspace: workspace, State: "updating", Message: "installing release",
		ServeVersion: previous.view.ServeVersion, UpdateAvailable: previous.view.UpdateAvailable,
	}
	m.publishServerIfCurrent(hostID, mh, progress, "", "")
	view, token, err := m.ensureServerLocked(opCtx, mh, hostID, workspace, true)
	if err != nil {
		return view, "", err
	}
	return view, token, nil
}
