package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/store"
)

// remoteTabModelSeq stamps every remote-tab model assignment; the credential
// proxy uses the stamps to resolve "most recently set" without relying on map
// iteration order.
var remoteTabModelSeq atomic.Uint64

// ── View structs mirrored in frontend/src/lib/types.ts ──

// RemoteTabRef marks a tab as remote and binds it to a host+workspace pair.
type RemoteTabRef struct {
	HostID    string `json:"hostId"`
	Workspace string `json:"workspace"`
}

type RemoteProjectView struct {
	HostID    string `json:"hostId"`
	Workspace string `json:"workspace"`
	Title     string `json:"title,omitempty"`
	Color     string `json:"color,omitempty"`
	// Merged marks that an overlapping pin already existed and the returned
	// Workspace is that existing group's canonical path — no new pin was added.
	Merged bool `json:"merged,omitempty"`
}

// RemoteTabOpenOptions mirrors the frontend opts bag: NewSession lands the
// tab in a fresh serve session; SessionName resumes a listed one.
type RemoteTabOpenOptions struct {
	NewSession  bool   `json:"newSession,omitempty"`
	SessionName string `json:"sessionName,omitempty"`
}

// RemoteTabStateView is the payload on the remote-tab:{id}:state channel.
// State: connecting | ready | reconnecting | serve_down | error.
type RemoteTabStateView struct {
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// remoteTab is one open remote project tab.
type remoteTab struct {
	sessionMu sync.Mutex
	id        string
	ref       RemoteTabRef
	state     string
	err       string
	session   remoteTabSessionState
	hostLabel string
	// topicTitle starts as the workspace name and adopts the generated title.
	topicTitle           string
	titleRefreshInFlight bool
	// model is the desktop-owned current model ref for this remote tab.
	// modelSeq orders concurrent writes for deterministic proxy registration.
	model    string
	modelSeq uint64

	// Bridge fields are protected by App.remoteTabMu. gen fences old pumps;
	// client preserves cookies and token permits a new handshake.
	client *http.Client
	base   string
	token  string
	gen    uint64
	cancel context.CancelFunc
	// attachedGen marks a pump that survived the open/session-entry barrier.
	// It stays internal so ListTabs never exposes a transient non-wire state.
	attachedGen uint64
	// Pending approval/ask frames are retained while the frontend surface is
	// inactive. RemoteTabSnapshot replays them when that surface mounts again.
	pendingEvents map[string]json.RawMessage

	// Transient runtime state is projected into TabMeta even while this tab is
	// inactive, matching the local tab strip's running/prompt/job indicators.
	runtime remoteTabRuntimeState
}

type remoteTabRuntimeState struct {
	// revision orders asynchronous /status snapshots against newer requests
	// and SSE-derived runtime mutations within the same connection generation.
	revision        uint64
	running         bool
	turnStartedAt   int64
	pendingPrompt   bool
	backgroundJobs  int
	cancelRequested bool
	cancellable     bool
}

type remoteTabSessionState struct {
	newSession bool
	name       string
	reset      bool
	// instanceID identifies the Serve process that owns this session. A
	// changed id requires explicit /new or /resume re-entry before ready.
	instanceID string
}

type remoteTabLayoutState struct {
	order      []string
	stripOrder []string
	activeID   string
}

// ── Registry CRUD (user config, same lock discipline as remote hosts) ──

func (a *App) ListRemoteProjects() ([]RemoteProjectView, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]RemoteProjectView, 0, len(cfg.Remote.Projects))
	for _, p := range cfg.Remote.Projects {
		out = append(out, remoteProjectEntryToView(p))
	}
	return out, nil
}

func (a *App) AddRemoteProject(hostID, workspace string) (RemoteProjectView, error) {
	hostID = strings.TrimSpace(hostID)
	workspace = strings.TrimSpace(workspace)
	var view RemoteProjectView
	err := editUserConfig(func(c *config.Config) error {
		// Overlapping pins on one host collapse into the existing group.
		// This avoids duplicate serves over the same files and returns the
		// canonical workspace with Merged set.
		if merged, ok := resolveOverlappingWorkspace(c.Remote.Projects, hostID, workspace); ok {
			workspace = merged
			stored, retained := c.RemoteProject(hostID, merged)
			if !retained {
				return fmt.Errorf("remote project was not retained")
			}
			view = remoteProjectEntryToView(stored)
			view.Merged = true
			return nil
		}
		entry := config.RemoteProjectEntry{
			HostID:    hostID,
			Workspace: workspace,
		}
		if err := c.UpsertRemoteProject(entry); err != nil {
			return err
		}
		stored, ok := c.RemoteProject(entry.HostID, entry.Workspace)
		if !ok {
			return fmt.Errorf("remote project was not retained")
		}
		view = remoteProjectEntryToView(stored)
		return nil
	})
	if err != nil {
		return RemoteProjectView{}, err
	}
	return view, nil
}

// resolveOverlappingWorkspace finds the existing pin on the same host that the
// requested workspace should merge into: an exact match wins, then the
// nearest ancestor pin, then the shallowest descendant pin. Remote paths are
// POSIX; "~" and unresolvable relatives simply never overlap (safe default).
func resolveOverlappingWorkspace(existing []config.RemoteProjectEntry, hostID, workspace string) (string, bool) {
	target := cleanRemoteWorkspace(workspace)
	if target == "" {
		return "", false
	}
	ancestor, ancestorDepth := "", -1
	descendant, descendantDepth := "", 1<<30
	for _, p := range existing {
		if p.HostID != hostID {
			continue
		}
		cand := cleanRemoteWorkspace(p.Workspace)
		if cand == "" {
			continue
		}
		switch {
		case cand == target:
			return p.Workspace, true
		case isRemoteSubpath(cand, target): // existing pin is an ancestor of the request
			if d := pathDepth(cand); ancestor == "" || d > ancestorDepth {
				ancestor, ancestorDepth = p.Workspace, d
			}
		case isRemoteSubpath(target, cand): // existing pin is a descendant of the request
			if d := pathDepth(cand); descendant == "" || d < descendantDepth {
				descendant, descendantDepth = p.Workspace, d
			}
		}
	}
	if ancestor != "" {
		return ancestor, true
	}
	return descendant, descendant != ""
}

func cleanRemoteWorkspace(ws string) string {
	ws = strings.TrimSpace(ws)
	if ws == "" || ws == "~" {
		return ws
	}
	return path.Clean(strings.TrimRight(ws, "/"))
}

// isRemoteSubpath reports parent/child nesting between two cleaned POSIX
// paths; equal paths are deliberately not subpaths of each other.
func isRemoteSubpath(parent, child string) bool {
	if parent == "/" {
		return strings.HasPrefix(child, "/") && child != "/"
	}
	return strings.HasPrefix(child, parent+"/")
}

func pathDepth(cleaned string) int {
	if cleaned == "" || cleaned == "/" {
		return 0
	}
	return strings.Count(cleaned, "/")
}

func (a *App) RemoveRemoteProject(hostID, workspace string) error {
	return editUserConfig(func(c *config.Config) error {
		c.RemoveRemoteProject(strings.TrimSpace(hostID), strings.TrimSpace(workspace))
		return nil
	})
}

func remoteProjectEntryToView(p config.RemoteProjectEntry) RemoteProjectView {
	return RemoteProjectView{HostID: p.HostID, Workspace: p.Workspace, Title: p.Title}
}

// remoteProjectNodes lists pinned remote workspaces as project group shells
// for the tree snapshot. Read failures degrade to "no remote projects" at the
// caller — a broken config must not take the whole tree down.
func (a *App) remoteProjectNodes() ([]ProjectNode, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	out := make([]ProjectNode, 0, len(cfg.Remote.Projects))
	for _, p := range cfg.Remote.Projects {
		label := strings.TrimSpace(p.Title)
		if label == "" {
			label = remoteWorkspaceName(p.Workspace)
		}
		out = append(out, ProjectNode{
			Key:   "project_remote_" + store.RemoteWorkspaceSlug(p.HostID+":"+p.Workspace),
			Kind:  "project",
			Label: label,
			// Root participates in tree selection and drag identity. Qualify it
			// with the host so identical paths on two hosts never alias.
			Root:   "remote-project:" + p.HostID + ":" + p.Workspace,
			Remote: &RemoteTabRef{HostID: p.HostID, Workspace: p.Workspace},
		})
	}
	return out, nil
}

// ── Remote project tabs ──

type remoteTabOpenRegistration struct {
	reuseID    string
	reuseBlank bool
	revive     bool
	retired    []context.CancelFunc
}

// registerRemoteTabOpen serializes reuse, error-shell retirement, and insert.
func (a *App) registerRemoteTabOpen(tab *remoteTab, hostLabel string, opts RemoteTabOpenOptions) remoteTabOpenRegistration {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if a.remoteTabs == nil {
		a.remoteTabs = map[string]*remoteTab{}
	}
	var result remoteTabOpenRegistration
	for _, existing := range a.remoteTabs {
		if existing.ref != tab.ref || existing.state == "error" {
			continue
		}
		result.reuseID = existing.id
		result.reuseBlank = existing.session.reset
		result.revive = existing.state == "disconnected" || existing.state == "serve_down"
		existing.hostLabel = hostLabel
		if (result.revive || existing.client == nil) && (opts.NewSession || strings.TrimSpace(opts.SessionName) != "") {
			existing.session.newSession = opts.NewSession
			existing.session.name = strings.TrimSpace(opts.SessionName)
		}
		if result.revive {
			existing.state = "connecting"
		}
		return result
	}
	for id, existing := range a.remoteTabs {
		if existing.ref != tab.ref || existing.state != "error" {
			continue
		}
		if existing.cancel != nil {
			result.retired = append(result.retired, existing.cancel)
		}
		delete(a.remoteTabs, id)
		a.remoteTabLayout.order = removeRemoteTabOrderID(a.remoteTabLayout.order, id)
	}
	tab.modelSeq = remoteTabModelSeq.Add(1)
	a.remoteTabs[tab.id] = tab
	a.remoteTabLayout.order = append(a.remoteTabLayout.order, tab.id)
	return result
}

// OpenRemoteProjectTab registers the project (idempotent), opens an in-app
// tab for the remote workspace, and returns its meta immediately. The remote
// Serve bootstrap runs in the background: a first run downloads/installs the
// CLI and can take minutes, so the surface follows progress through
// remote-tab:{id}:state events instead of this promise.
func (a *App) OpenRemoteProjectTab(hostID, workspace string, opts RemoteTabOpenOptions) (TabMeta, error) {
	singleSurface := a.singleSurfaceLayoutEnabled()
	if singleSurface {
		a.singleSurfaceMu.Lock()
		defer a.singleSurfaceMu.Unlock()
	}
	a.tabSelectionMu.Lock()
	defer a.tabSelectionMu.Unlock()

	hostID = strings.TrimSpace(hostID)
	workspace = strings.TrimSpace(workspace)
	if hostID == "" || workspace == "" {
		return TabMeta{}, fmt.Errorf("remote project tab: host and workspace are required")
	}
	cfg, err := config.Load()
	if err != nil {
		return TabMeta{}, err
	}
	host, ok := cfg.RemoteHost(hostID)
	if !ok {
		return TabMeta{}, fmt.Errorf("remote host %q is not configured", hostID)
	}
	if err := a.snapshotActiveLocalBeforeRemote(); err != nil {
		return TabMeta{}, err
	}
	// The pin registry collapses overlapping paths into the existing group:
	// whatever nested path the caller asked for, the tab must land on the
	// canonical workspace so tabs and serves stay one-per-group.
	proj, err := a.AddRemoteProject(hostID, workspace)
	if err != nil {
		return TabMeta{}, err
	}
	workspace = proj.Workspace

	ref := RemoteTabRef{HostID: hostID, Workspace: workspace}
	tabID := newTabID()
	model := ""
	if host.CredentialProxyEnabled() {
		model = resolveNewSessionModel(cfg)
	}
	tab := &remoteTab{id: tabID, ref: ref, state: "connecting", session: remoteTabSessionState{newSession: opts.NewSession, name: opts.SessionName}, hostLabel: host.Name, topicTitle: remoteWorkspaceName(workspace), model: model}

	// Reuse-or-insert is atomic so concurrent opens cannot create two sessions.
	registration := a.registerRemoteTabOpen(tab, host.Name, opts)
	for _, cancel := range registration.retired {
		cancel()
	}
	if registration.reuseID != "" {
		_, ok := a.remoteTabMetaSnapshot(registration.reuseID)
		if !ok {
			return TabMeta{}, fmt.Errorf("remote tab %q closed while opening", registration.reuseID)
		}
		if singleSurface {
			_, err = a.keepOnlyRemoteVisibleTab(registration.reuseID)
			if err != nil {
				return TabMeta{}, err
			}
		}

		// Apply the requested session transition only after the visible-surface
		// transaction succeeds. A snapshot failure must leave the remote Serve's
		// current conversation untouched so retrying the navigation is safe.
		if registration.revive {
			a.emitRemoteTabState(registration.reuseID, "connecting", "")
			a.goSafe("remoteTabServe", func() { a.bootstrapRemoteTab(registration.reuseID, hostID, workspace) })
		} else if name := strings.TrimSpace(opts.SessionName); name != "" {
			a.resumeRemoteTabSession(registration.reuseID, name)
		} else {
			// Reuse the pending blank like EnsureBlankTab does locally; only
			// reset again once the current session earned content.
			if opts.NewSession && !registration.reuseBlank {
				if err := a.resetRemoteTabSession(registration.reuseID); err != nil {
					return TabMeta{}, err
				}
			}
		}
		meta, ok := a.remoteTabMetaSnapshot(registration.reuseID)
		if !ok {
			return TabMeta{}, fmt.Errorf("remote tab %q closed while opening", registration.reuseID)
		}
		a.activateRemoteTab(registration.reuseID, meta)
		a.saveTabsFromRemote()
		return meta, nil
	}

	a.emitRemoteTabState(tabID, "connecting", "")
	meta, ok := a.remoteTabMetaSnapshot(tabID)
	if !ok {
		return TabMeta{}, fmt.Errorf("remote tab %q closed while opening", tabID)
	}
	if singleSurface {
		meta, err = a.keepOnlyRemoteVisibleTab(tabID)
		if err != nil {
			_ = a.closeRemoteTabRegistration(tabID, true)
			return TabMeta{}, err
		}
	}
	a.activateRemoteTab(tabID, meta)
	a.goSafe("remoteTabServe", func() { a.bootstrapRemoteTab(tabID, hostID, workspace) })
	// Persist after activation so the file records the highlighted remote id.
	a.saveTabsFromRemote()
	return meta, nil
}

// restoreRemoteTabShells rebuilds disconnected registry entries from the
// persisted tab file so remote tabs survive a restart. Shells never connect
// on their own: activating one (SetActiveTab) or opening its project
// bootstraps the reconnect, which lands in a fresh blank session.
func (a *App) restoreRemoteTabShells(f desktopTabsFile) {
	if len(f.RemoteTabs) == 0 {
		return
	}
	// Local ids are snapshotted under a.mu BEFORE taking remoteTabMu — the
	// save path locks in the a.mu → tabsSaveMu → remoteTabMu order, so this
	// function must never hold remoteTabMu while wanting a.mu.
	a.mu.RLock()
	localIDs := make(map[string]bool, len(a.tabs))
	for id := range a.tabs {
		localIDs[id] = true
	}
	a.mu.RUnlock()

	cfg, cfgErr := config.Load()
	a.remoteTabMu.Lock()
	if a.remoteTabs == nil {
		a.remoteTabs = map[string]*remoteTab{}
	}
	a.remoteTabLayout.stripOrder = append([]string(nil), f.TabOrder...)
	restoredIDs := make(map[string]bool, len(f.RemoteTabs))
	for _, entry := range f.RemoteTabs {
		id := strings.TrimSpace(entry.ID)
		hostID := strings.TrimSpace(entry.HostID)
		ws := strings.TrimSpace(entry.Workspace)
		if id == "" || hostID == "" || ws == "" || localIDs[id] || a.remoteTabs[id] != nil {
			continue
		}
		hostLabel, model := hostID, ""
		if cfgErr == nil {
			if host, ok := cfg.RemoteHost(hostID); ok {
				if name := strings.TrimSpace(host.Name); name != "" {
					hostLabel = name
				}
				if host.CredentialProxyEnabled() {
					model = strings.TrimSpace(entry.Model)
				}
			}
		}
		title := strings.TrimSpace(entry.TopicTitle)
		if title == "" {
			title = remoteWorkspaceName(ws)
		}
		restored := &remoteTab{
			id: id, ref: RemoteTabRef{HostID: hostID, Workspace: ws},
			state: "disconnected", session: remoteTabSessionState{newSession: true},
			hostLabel: hostLabel, topicTitle: title, model: model,
		}
		restored.modelSeq = remoteTabModelSeq.Add(1)
		a.remoteTabs[id] = restored
		restoredIDs[id] = true
	}
	seen := make(map[string]bool, len(restoredIDs))
	for _, id := range f.RemoteTabOrder {
		if restoredIDs[id] && !seen[id] {
			a.remoteTabLayout.order = append(a.remoteTabLayout.order, id)
			seen[id] = true
		}
	}
	for _, entry := range f.RemoteTabs {
		id := strings.TrimSpace(entry.ID)
		if restoredIDs[id] && !seen[id] {
			a.remoteTabLayout.order = append(a.remoteTabLayout.order, id)
			seen[id] = true
		}
	}
	if f.ActiveTab != "" && a.remoteTabs[f.ActiveTab] != nil {
		a.remoteTabLayout.activeID = f.ActiveTab
	}
	a.remoteTabMu.Unlock()
}

// removeRemoteTabOrderID drops one id from the remote strip order.
func removeRemoteTabOrderID(order []string, id string) []string {
	out := order[:0]
	for _, existing := range order {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}

// activateRemoteTab highlights the tab in the strip and tells the frontend
// chrome to adopt it.
func (a *App) activateRemoteTab(tabID string, meta TabMeta) {
	a.remoteTabMu.Lock()
	a.remoteTabLayout.activeID = tabID
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent("remote-tab:opened", meta)
}

// snapshotActiveLocalBeforeRemote preserves the same data-loss barrier used
// by local-to-local tab switches. The caller serializes cross-registry tab
// selection with tabSelectionMu.
func (a *App) snapshotActiveLocalBeforeRemote() error {
	a.remoteTabMu.Lock()
	remoteActive := a.remoteTabLayout.activeID != ""
	a.remoteTabMu.Unlock()
	if remoteActive {
		return nil
	}
	a.mu.RLock()
	active := a.tabs[a.activeTabID]
	a.mu.RUnlock()
	return a.snapshotTabForAction(active, "switching tabs")
}

// remoteTabMeta builds the frontend-facing shape of one remote tab; the
// create and reuse paths share it so both return identical metas. RemoteState
// seeds the surface before any state event arrives this run (restored shells).
func remoteTabMetaLocked(tab *remoteTab) TabMeta {
	label := tab.hostLabel
	if strings.TrimSpace(tab.model) != "" {
		label = tab.model
	}
	ref := tab.ref
	return TabMeta{
		ID:              tab.id,
		Scope:           "project",
		WorkspaceRoot:   tab.ref.Workspace,
		WorkspaceName:   remoteWorkspaceName(tab.ref.Workspace),
		TopicTitle:      tab.topicTitle,
		Label:           label,
		Mode:            "normal",
		Active:          true,
		Cwd:             tab.ref.Workspace,
		Remote:          &ref,
		RemoteState:     tab.state,
		Ready:           tab.state == "ready",
		Running:         tab.runtime.running || tab.runtime.pendingPrompt || tab.runtime.backgroundJobs > 0,
		TurnStartedAt:   tab.runtime.turnStartedAt,
		PendingPrompt:   tab.runtime.pendingPrompt,
		BackgroundJobs:  tab.runtime.backgroundJobs,
		CancelRequested: tab.runtime.cancelRequested,
		Cancellable:     tab.runtime.cancellable,
	}
}

func (a *App) remoteTabMetaSnapshot(tabID string) (TabMeta, bool) {
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	tab := a.remoteTabs[tabID]
	if tab == nil {
		return TabMeta{}, false
	}
	return remoteTabMetaLocked(tab), true
}

// bootstrapRemoteTab drives one remote tab to a terminal state: ensure the
// SSH connection, ensure the remote Serve + loopback tunnel, then report
// ready (or the failure) on the tab's state channel.
func (a *App) bootstrapRemoteTab(tabID, hostID, workspace string) {
	// Idempotence guard: a concurrent reattach may have brought this tab to
	// ready while the open call was still in flight — bootstrapping again
	// would re-enter the session and stack a second pump.
	a.remoteTabMu.Lock()
	tabState := ""
	if tab := a.remoteTabs[tabID]; tab != nil {
		tabState = tab.state
	}
	a.remoteTabMu.Unlock()
	if tabState == "ready" {
		return
	}
	rt, err := a.remoteRT()
	if err != nil {
		a.emitRemoteTabState(tabID, "error", err.Error())
		return
	}
	// Connect is idempotent: an already connecting/connected host returns
	// nil, a stopped generation is replaced with a fresh dial.
	if err := rt.Connect(hostID); err != nil {
		a.emitRemoteTabState(tabID, "error", err.Error())
		return
	}
	if err := waitForRemoteHost(rt, hostID, 60*time.Second); err != nil {
		a.emitRemoteTabState(tabID, "error", err.Error())
		return
	}
	ctx := a.bootContext()
	if ctx == nil {
		ctx = context.Background()
	}
	view, token, err := rt.EnsureServer(ctx, hostID, workspace)
	if err != nil {
		a.emitRemoteTabState(tabID, "error", err.Error())
		return
	}
	if view.State != "ready" || view.LocalURL == "" {
		msg := view.Error
		if msg == "" {
			msg = view.Message
		}
		if msg == "" {
			msg = "remote serve did not report a local URL"
		}
		a.emitRemoteTabState(tabID, "serve_down", msg)
		return
	}
	a.remoteTabMu.Lock()
	openTab := a.remoteTabs[tabID]
	if openTab == nil {
		a.remoteTabMu.Unlock()
		return // closed while the bootstrap was in flight
	}
	opts := RemoteTabOpenOptions{NewSession: openTab.session.newSession, SessionName: openTab.session.name}
	a.remoteTabMu.Unlock()
	// ctx outlives the call: the pump derives from it, while the handshake
	// and session entry inside run under a bounded sub-context.
	entered, err := a.attachRemoteTabServe(ctx, tabID, view.LocalURL, token, view.InstanceID, opts)
	if err != nil {
		// Pump failures publish their own reconnecting/error state. Only a
		// pre-attach failure should transition the original connecting shell.
		a.remoteTabMu.Lock()
		current := a.remoteTabs[tabID]
		connecting := current != nil && current.state == "connecting"
		a.remoteTabMu.Unlock()
		if connecting {
			a.emitRemoteTabState(tabID, "error", err.Error())
		}
		return
	}
	a.remoteTabMu.Lock()
	openTab = a.remoteTabs[tabID]
	if openTab == nil {
		a.remoteTabMu.Unlock()
		return
	}
	gen := openTab.gen
	if openTab.attachedGen != gen || openTab.state != "connecting" {
		a.remoteTabMu.Unlock()
		return
	}
	openTab.session.reset = entered && opts.NewSession
	if openTab.session.reset {
		// A bootstrapped fresh session carries the localized default title,
		// same as the live-tab reset path.
		openTab.topicTitle = a.localizedDefaultTopicTitle()
	}
	a.remoteTabMu.Unlock()
	if !a.publishRemoteTabAttachedReady(tabID, gen) {
		return
	}
	// The confirmed /events pump makes the session usable without losing prompts.
	// Saving the explorer default is auxiliary and cannot downgrade a healthy tab.
	_ = a.saveLastRemoteWorkspace(hostID, workspace)
}

// waitForRemoteHost polls the kernel until the host is usable. The frontend
// has waitForRemoteConnection over status events; this is the same contract
// server-side for cold OpenRemoteProjectTab calls.
func waitForRemoteHost(rt remoteKernel, hostID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		for _, s := range rt.Statuses() {
			if s.HostID != hostID {
				continue
			}
			switch s.State {
			case "connected", "degraded":
				return nil
			case "stopped":
				if s.Error != "" {
					return fmt.Errorf("remote host %q: %s", hostID, s.Error)
				}
				return fmt.Errorf("remote host %q stopped", hostID)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("remote host %q: connection timed out", hostID)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (a *App) emitRemoteTabState(tabID, state, errMsg string) {
	a.remoteTabMu.Lock()
	tab := a.remoteTabs[tabID]
	if tab == nil {
		a.remoteTabMu.Unlock()
		return
	}
	tab.state = state
	tab.err = errMsg
	a.remoteTabMu.Unlock()
	a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", tabID), RemoteTabStateView{State: state, Error: errMsg})
}

// remoteWorkspaceName is posix-safe (remote paths on a Windows host must not
// go through filepath).
func remoteWorkspaceName(ws string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(ws), "/")
	if trimmed == "" || trimmed == "~" {
		return "~"
	}
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}
