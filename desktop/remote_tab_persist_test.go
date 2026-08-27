package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func seedClassicBridgeTestHost(t *testing.T, hostID string) {
	seedBridgeTestHost(t, hostID)
	if err := editUserConfig(func(c *config.Config) error { return c.SetDesktopLayoutStyle("classic") }); err != nil {
		t.Fatal(err)
	}
}

func readPersistedTabsFile(t *testing.T) desktopTabsFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.ReasonixHomeDir(), tabsFileName))
	if err != nil {
		t.Fatalf("read tabs file: %v", err)
	}
	var f desktopTabsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse tabs file: %v", err)
	}
	return f
}

func seedLocalTab(a *App, id string) {
	a.mu.Lock()
	if a.tabs == nil {
		a.tabs = map[string]*WorkspaceTab{}
	}
	a.tabs[id] = &WorkspaceTab{ID: id, Scope: "global"}
	a.tabOrder = append(a.tabOrder, id)
	a.mu.Unlock()
}

func TestRemoveRemoteHostReplacesSoleRemoteSurfaceWithLocalBlank(t *testing.T) {
	seedBridgeTestHost(t, "box")
	a := NewApp()
	a.remoteRuntime = &fakeRemoteKernel{}
	t.Cleanup(func() { a.shutdown(context.Background()) })
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"remote-only": {
			id: "remote-only", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"},
			state: "disconnected", session: remoteTabSessionState{newSession: true},
		},
	}
	a.remoteTabLayout.order = []string{"remote-only"}
	a.remoteTabLayout.stripOrder = []string{"remote-only"}
	a.remoteTabLayout.activeID = "remote-only"
	a.remoteTabMu.Unlock()

	if err := a.RemoveRemoteHost("box"); err != nil {
		t.Fatal(err)
	}
	tabs := a.ListTabs()
	if len(tabs) != 1 || tabs[0].Remote != nil || !tabs[0].Active {
		t.Fatalf("tabs after deleting sole remote host = %+v, want one active local blank", tabs)
	}
	a.remoteTabMu.Lock()
	remoteCount := len(a.remoteTabs)
	a.remoteTabMu.Unlock()
	if remoteCount != 0 {
		t.Fatalf("deleted host retained %d remote tabs", remoteCount)
	}
}

// TestRemoteTabOpenPersistRoundTrip: an open remote tab lands in
// desktop-tabs.json; closing removes it again.
func TestRemoteTabOpenPersistRoundTrip(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "Prior chat", Current: true}})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedClassicBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)

	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "s1", SessionPath: "/remote/sessions/s1.jsonl", SessionTitle: "Prior chat"})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, meta.ID, "ready")

	f := readPersistedTabsFile(t)
	if len(f.RemoteTabs) != 1 || len(f.RemoteTabOrder) != 1 {
		t.Fatalf("persisted remote section = %+v / %v, want one entry", f.RemoteTabs, f.RemoteTabOrder)
	}
	entry := f.RemoteTabs[0]
	if entry.ID != meta.ID || entry.HostID != "box" || entry.Workspace != "~/app" {
		t.Fatalf("persisted entry = %+v, want id/host/workspace for %s", entry, meta.ID)
	}
	if entry.SessionName != "s1" || entry.SessionPath != "/remote/sessions/s1.jsonl" {
		t.Fatalf("persisted session = %q at %q, want s1", entry.SessionName, entry.SessionPath)
	}
	if f.RemoteTabOrder[0] != entry.ID {
		t.Fatalf("persisted remote order = %v, want the entry id first", f.RemoteTabOrder)
	}
	if f.ActiveTab != meta.ID {
		t.Fatalf("persisted active tab = %q, want the active remote id", f.ActiveTab)
	}

	if err := a.CloseRemoteTab(meta.ID); err != nil {
		t.Fatal(err)
	}
	if f = readPersistedTabsFile(t); len(f.RemoteTabs) != 0 {
		t.Fatalf("closed tab still persisted: %+v", f.RemoteTabs)
	}
}

// TestRemoteTabRestoreBuildsDisconnectedShells: restore rebuilds shells
// without connecting anything; invalid and local-colliding ids are skipped.
// Restored shells stay in the strip but must NOT become the startup active
// surface — first open would otherwise land on the disconnected placeholder.
func TestRemoteTabRestoreBuildsDisconnectedShells(t *testing.T) {
	seedBridgeTestHost(t, "box")
	a := &App{}
	seedLocalTab(a, "local-1")
	f := desktopTabsFile{
		RemoteTabs: []desktopRemoteTabEntry{
			{ID: "r-1", HostID: "box", Workspace: "~/app", TopicTitle: "Fix bug", SessionName: "s1", SessionPath: "/remote/sessions/s1.jsonl"},
			{ID: "r-2", HostID: "box", Workspace: "~/web", SessionPath: "/remote/sessions/blank.jsonl", SessionReset: true},
			{ID: "", HostID: "box", Workspace: "~/skip"},
			{ID: "local-1", HostID: "box", Workspace: "~/dup"},
		},
		RemoteTabOrder: []string{"r-2", "r-1"},
		ActiveTab:      "r-1",
	}
	a.restoreRemoteTabShells(f)

	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if len(a.remoteTabs) != 2 || a.remoteTabs["r-1"] == nil || a.remoteTabs["r-2"] == nil {
		t.Fatalf("restored shells = %+v", a.remoteTabs)
	}
	for id, tab := range a.remoteTabs {
		if tab.state != "disconnected" {
			t.Fatalf("shell %s state = %q, want disconnected", id, tab.state)
		}
		if tab.client != nil || tab.cancel != nil {
			t.Fatalf("shell %s connected during restore", id)
		}
	}
	if got := a.remoteTabs["r-1"].topicTitle; got != "Fix bug" {
		t.Fatalf("restored title = %q, want the persisted one", got)
	}
	if tab := a.remoteTabs["r-1"]; tab.session.newSession || tab.session.name != "s1" || tab.session.path != "/remote/sessions/s1.jsonl" {
		t.Fatalf("restored session identity = %+v", tab.session)
	}
	if tab := a.remoteTabs["r-2"]; !tab.session.newSession || !tab.session.reset || tab.session.path != "/remote/sessions/blank.jsonl" {
		t.Fatalf("restored blank session identity = %+v", tab.session)
	}
	if got := strings.Join(a.remoteTabLayout.order, ","); got != "r-2,r-1" {
		t.Fatalf("restored remote order = %q, want r-2,r-1", got)
	}
	if a.remoteTabLayout.activeID != "" {
		t.Fatalf("remoteActiveTabID = %q, want local startup surface", a.remoteTabLayout.activeID)
	}
}

func TestRemoteTabBlankSessionPersistsResetState(t *testing.T) {
	tab := &remoteTab{
		id: "blank", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"},
		session: remoteTabSessionState{newSession: true, path: "/remote/sessions/blank.jsonl", reset: true},
		routing: remoteTabSessionRouting{currentPath: "/remote/sessions/blank.jsonl", running: map[string]bool{}},
	}
	a := &App{remoteTabs: map[string]*remoteTab{tab.id: tab}, remoteTabLayout: remoteTabLayoutState{order: []string{tab.id}}}
	entries, _, _, _ := a.remoteTabsFileEntries(nil)
	if len(entries) != 1 || !entries[0].SessionReset || entries[0].SessionPath != tab.session.path {
		t.Fatalf("persisted blank entry = %+v", entries)
	}
}

// TestActivateDisconnectedShellReconnects resumes the persisted session in
// the existing shell instead of replacing it with a blank conversation.
func TestActivateDisconnectedShellReconnects(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{{Name: "s1", Path: "/remote/sessions/s1.jsonl"}})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"shell-1": {id: "shell-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "disconnected", session: remoteTabSessionState{name: "s1", path: "/remote/sessions/s1.jsonl"}, hostLabel: "box", topicTitle: "Prior chat"},
	}
	a.remoteTabLayout.order = []string{"shell-1"}
	a.remoteTabMu.Unlock()

	if err := a.SetActiveTab("shell-1"); err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, "shell-1", "ready")
	newCalled, resumePath, _ := fs.snapshot()
	if newCalled != 0 || resumePath != "/remote/sessions/s1.jsonl" {
		t.Fatalf("revive called new=%d resume=%q, want persisted s1", newCalled, resumePath)
	}
	a.remoteTabMu.Lock()
	active := a.remoteTabLayout.activeID
	a.remoteTabMu.Unlock()
	if active != "shell-1" {
		t.Fatalf("remoteActiveTabID = %q, want shell-1", active)
	}
}

func TestSetActiveRemoteTabPersistsAndUnknownKeepsSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	a := &App{}
	seedLocalTab(a, "local-1")
	a.mu.Lock()
	a.activeTabID = "local-1"
	a.mu.Unlock()
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"remote-1": {id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready"},
	}
	a.remoteTabLayout.order = []string{"remote-1"}
	a.remoteTabMu.Unlock()

	if err := a.SetActiveTab("remote-1"); err != nil {
		t.Fatal(err)
	}
	if got := readPersistedTabsFile(t).ActiveTab; got != "remote-1" {
		t.Fatalf("persisted active tab = %q, want remote-1", got)
	}
	if err := a.SetActiveTab("missing"); err == nil {
		t.Fatal("unknown tab activation succeeded")
	}
	a.remoteTabMu.Lock()
	active := a.remoteTabLayout.activeID
	a.remoteTabMu.Unlock()
	if active != "remote-1" {
		t.Fatalf("unknown activation cleared remote selection: %q", active)
	}
}

func TestSetActiveRemoteTabBlocksWhenCurrentSessionCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, _ := appWithTab(t, path)
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"remote-1": {id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready"},
	}
	a.remoteTabLayout.order = []string{"remote-1"}
	a.remoteTabMu.Unlock()

	err := a.SetActiveTab("remote-1")
	if err == nil || !strings.Contains(err.Error(), "save current session before switching tabs") {
		t.Fatalf("SetActiveTab(remote) error = %v, want persistence failure", err)
	}
	a.remoteTabMu.Lock()
	active := a.remoteTabLayout.activeID
	a.remoteTabMu.Unlock()
	if active != "" {
		t.Fatalf("remote active tab = %q, want local selection preserved", active)
	}
}

func TestSetActiveLocalTabKeepsRemoteSelectionWhenSessionCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, _ := appWithTab(t, path)
	seedLocalTab(a, "target")
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"remote-1": {id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready"},
	}
	a.remoteTabLayout.activeID = "remote-1"
	a.remoteTabLayout.order = []string{"remote-1"}
	a.remoteTabMu.Unlock()

	err := a.SetActiveTab("target")
	if err == nil || !strings.Contains(err.Error(), "save current session before switching tabs") {
		t.Fatalf("SetActiveTab(local) error = %v, want persistence failure", err)
	}
	a.remoteTabMu.Lock()
	active := a.remoteTabLayout.activeID
	a.remoteTabMu.Unlock()
	if active != "remote-1" {
		t.Fatalf("remote active tab = %q, want original remote selection", active)
	}
	if a.activeTabID != "test_tab" {
		t.Fatalf("local active tab = %q, want original local tab", a.activeTabID)
	}
}

func TestOpenRemoteProjectTabBlocksBeforeMutationWhenLocalSessionCannotPersist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	seedBridgeTestHost(t, "box")
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, _ := appWithTab(t, path)

	_, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err == nil || !strings.Contains(err.Error(), "save current session before switching tabs") {
		t.Fatalf("OpenRemoteProjectTab error = %v, want persistence failure", err)
	}
	a.remoteTabMu.Lock()
	remoteCount := len(a.remoteTabs)
	a.remoteTabMu.Unlock()
	if remoteCount != 0 {
		t.Fatalf("remote tab count = %d, want no mutation after failed save", remoteCount)
	}
}

// TestOpenRemoteProjectTabRevivesShell: the tree-group path (ensure-open)
// reconnects a disconnected shell in place instead of only activating it.
func TestOpenRemoteProjectTabRevivesShell(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"shell-1": {id: "shell-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "disconnected", session: remoteTabSessionState{newSession: true}, hostLabel: "box", topicTitle: "app"},
	}
	a.remoteTabLayout.order = []string{"shell-1"}
	a.remoteTabMu.Unlock()

	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "shell-1" {
		t.Fatalf("revived tab id = %q, want the shell id shell-1", meta.ID)
	}
	waitForTabState(t, a, "shell-1", "ready")
}

func TestOpenRemoteProjectTabAppliesSingleSurfacePolicy(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{statuses: []RemoteConnectionStatusView{{HostID: "box", State: "connected"}, {HostID: "box-two", State: "connected"}}, ensureView: RemoteServerView{State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret"}
	seedBridgeTestHost(t, "box")
	if err := editUserConfig(func(c *config.Config) error {
		if err := c.SetDesktopLayoutStyle("workbench"); err != nil {
			return err
		}
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "box-two", Host: "127.0.0.1", Port: 22, User: "dev"})
	}); err != nil {
		t.Fatal(err)
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	seedLocalTab(a, "local")
	a.mu.Lock()
	a.activeTabID = "local"
	a.mu.Unlock()

	first, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, first.ID, "ready")
	second, err := a.OpenRemoteProjectTab("box-two", "~/other", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	waitForTabState(t, a, second.ID, "ready")

	tabs := a.ListTabs()
	if len(tabs) != 1 || tabs[0].ID != second.ID || !tabs[0].Active {
		t.Fatalf("single-surface tabs = %+v, want only active remote %q", tabs, second.ID)
	}
	a.mu.RLock()
	localCount := len(a.tabs)
	a.mu.RUnlock()
	a.remoteTabMu.Lock()
	remoteCount := len(a.remoteTabs)
	a.remoteTabMu.Unlock()
	if localCount != 0 || remoteCount != 1 {
		t.Fatalf("single-surface registry counts local=%d remote=%d", localCount, remoteCount)
	}
	if err := a.CloseRemoteTab(second.ID); err == nil || !strings.Contains(err.Error(), "cannot close the last tab") {
		t.Fatalf("close final remote surface error = %v", err)
	}
	a.remoteTabMu.Lock()
	_, stillVisible := a.remoteTabs[second.ID]
	a.remoteTabMu.Unlock()
	if !stillVisible {
		t.Fatal("failed final-surface close removed the remote tab")
	}
}

// TestReorderTabsMixedPersistsBothOrders: the full strip order partitions into
// local and remote orders; both persist; unknown remote ids reject the whole
// reorder without mutating either side.
func TestReorderTabsMixedPersistsBothOrders(t *testing.T) {
	seedBridgeTestHost(t, "box")
	a := &App{}
	seedLocalTab(a, "l1")
	seedLocalTab(a, "l2")
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{"r1": {id: "r1", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}}}
	a.remoteTabLayout.order = []string{"r1"}
	a.remoteTabMu.Unlock()

	if err := a.ReorderTabs([]string{"r1", "l2", "l1"}); err != nil {
		t.Fatal(err)
	}
	a.mu.RLock()
	order := append([]string(nil), a.tabOrder...)
	a.mu.RUnlock()
	if len(order) != 2 || order[0] != "l2" || order[1] != "l1" {
		t.Fatalf("local order = %v, want [l2 l1]", order)
	}
	f := readPersistedTabsFile(t)
	if len(f.RemoteTabOrder) != 1 || f.RemoteTabOrder[0] != "r1" {
		t.Fatalf("persisted remote order = %v, want [r1]", f.RemoteTabOrder)
	}
	if got := strings.Join(f.TabOrder, ","); got != "r1,l2,l1" {
		t.Fatalf("persisted mixed strip order = %q, want r1,l2,l1", got)
	}

	if err := a.ReorderTabs([]string{"l1", "l2", "ghost"}); err == nil {
		t.Fatal("reorder accepted an unknown remote id")
	}
	a.mu.RLock()
	order = append([]string(nil), a.tabOrder...)
	a.mu.RUnlock()
	if len(order) != 2 || order[0] != "l2" || order[1] != "l1" {
		t.Fatalf("local order after rejected reorder = %v, want unchanged [l2 l1]", order)
	}
}

func TestReconcileTabStripOrderPreservesMixedOrderAndRepairsMembership(t *testing.T) {
	got := reconcileTabStripOrder(
		[]string{"remote-1", "gone", "local-2", "remote-1"},
		[]string{"local-1", "local-2"},
		[]string{"remote-1", "remote-2"},
	)
	if joined := strings.Join(got, ","); joined != "remote-1,local-2,local-1,remote-2" {
		t.Fatalf("reconciled strip order = %q", joined)
	}
}

func TestRemoteTabMetasMarksOnlySelectedTabActive(t *testing.T) {
	a := &App{
		remoteTabs: map[string]*remoteTab{
			"remote-1": {
				id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/one"},
				runtime: remoteTabRuntimeState{
					running: true, turnStartedAt: 123, pendingPrompt: true,
					backgroundJobs: 2, cancelRequested: true, cancellable: true,
				},
			},
			"remote-2": {id: "remote-2", ref: RemoteTabRef{HostID: "box", Workspace: "~/two"}},
		},
		remoteTabLayout: remoteTabLayoutState{
			order:      []string{"remote-1", "remote-2"},
			activeID:   "remote-2",
			stripOrder: []string{"remote-1", "local-1", "remote-2"},
		},
	}
	metas, active, order := a.remoteTabMetas([]string{"local-1"})
	if active != "remote-2" || strings.Join(order, ",") != "remote-1,local-1,remote-2" {
		t.Fatalf("active/order = %q / %v", active, order)
	}
	for _, meta := range metas {
		if meta.Active != (meta.ID == "remote-2") {
			t.Fatalf("meta %s active = %v", meta.ID, meta.Active)
		}
		if meta.ID == "remote-1" && (!meta.Running || meta.TurnStartedAt != 123 || !meta.PendingPrompt || meta.BackgroundJobs != 2 || !meta.CancelRequested || !meta.Cancellable) {
			t.Fatalf("inactive remote runtime meta = %+v", meta)
		}
	}
}

func TestCloseFinalLocalTabAllowsRemainingRemoteSurface(t *testing.T) {
	a := &App{}
	seedLocalTab(a, "local")
	a.remoteTabs = map[string]*remoteTab{
		"remote": {id: "remote", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "disconnected"},
	}
	a.remoteTabLayout.order = []string{"remote"}
	a.remoteTabLayout.stripOrder = []string{"local", "remote"}
	a.remoteTabLayout.activeID = "remote"
	if err := a.CloseTab("local"); err != nil {
		t.Fatalf("CloseTab(local) with remote survivor: %v", err)
	}
	a.mu.RLock()
	localCount := len(a.tabs)
	a.mu.RUnlock()
	if localCount != 0 {
		t.Fatalf("local tab count = %d, want 0", localCount)
	}
	if tabs := a.ListTabs(); len(tabs) != 1 || tabs[0].ID != "remote" || !tabs[0].Active {
		t.Fatalf("remaining surfaces = %+v, want active remote", tabs)
	}
}

func TestRemoteStatusRefreshPublishesInactiveRuntimeMeta(t *testing.T) {
	client := &http.Client{}
	log := &eventLog{}
	a := &App{
		remoteEventHook: log.add,
		remoteTabs: map[string]*remoteTab{
			"remote-1": {id: "remote-1", ref: RemoteTabRef{HostID: "box", Workspace: "~/one"}, client: client, gen: 4},
			"remote-2": {id: "remote-2", ref: RemoteTabRef{HostID: "box", Workspace: "~/two"}},
		},
		remoteTabLayout: remoteTabLayoutState{activeID: "remote-2"},
	}
	statusSeq := a.reserveRemoteTabStatusSequence("remote-1", client, 4)
	a.recordRemoteTabSessionStatus("remote-1", client, 4, statusSeq, json.RawMessage(`{"running":true,"pendingPrompt":true,"backgroundJobs":3,"cancelRequested":true,"cancellable":true}`))
	metas, _, _ := a.remoteTabMetas(nil)
	var got TabMeta
	for _, meta := range metas {
		if meta.ID == "remote-1" {
			got = meta
		}
	}
	if got.Active || !got.Running || !got.PendingPrompt || got.BackgroundJobs != 3 || !got.CancelRequested || !got.Cancellable || got.TurnStartedAt <= 0 {
		t.Fatalf("inactive status projection = %+v", got)
	}
	if log.count("remote-tab:updated ") != 1 {
		t.Fatalf("runtime status update events = %v", log.recorded())
	}
}

func TestRemoteStatusRefreshRejectsOutOfOrderSnapshot(t *testing.T) {
	client := &http.Client{}
	a := &App{remoteTabs: map[string]*remoteTab{
		"remote-1": {id: "remote-1", client: client, gen: 4},
	}}
	older := a.reserveRemoteTabStatusSequence("remote-1", client, 4)
	newer := a.reserveRemoteTabStatusSequence("remote-1", client, 4)
	a.recordRemoteTabSessionStatus("remote-1", client, 4, newer, json.RawMessage(`{"running":false,"pendingPrompt":false}`))
	a.recordRemoteTabSessionStatus("remote-1", client, 4, older, json.RawMessage(`{"running":true,"pendingPrompt":true}`))
	a.remoteTabMu.Lock()
	runtime := a.remoteTabs["remote-1"].runtime
	a.remoteTabMu.Unlock()
	if runtime.running || runtime.pendingPrompt {
		t.Fatalf("older status overwrote newer settled state: %+v", runtime)
	}
}

func TestRemoteStatusRefreshRejectsSnapshotOlderThanTurnDone(t *testing.T) {
	client := &http.Client{}
	a := &App{remoteTabs: map[string]*remoteTab{
		"remote-1": {id: "remote-1", client: client, gen: 4, runtime: remoteTabRuntimeState{running: true}},
	}}
	stale := a.reserveRemoteTabStatusSequence("remote-1", client, 4)
	a.completeRemoteTabTurn("remote-1", 4)
	a.recordRemoteTabSessionStatus("remote-1", client, 4, stale, json.RawMessage(`{"running":true,"pendingPrompt":true}`))
	a.remoteTabMu.Lock()
	runtime := a.remoteTabs["remote-1"].runtime
	a.remoteTabMu.Unlock()
	if runtime.running || runtime.pendingPrompt {
		t.Fatalf("pre-turn_done status revived settled runtime: %+v", runtime)
	}
}

// TestSingleSurfaceTabsFileCollapsesRemote: workbench/creation layouts keep
// exactly one surface across local and remote tabs, preferring the active one.
func TestSingleSurfaceTabsFileCollapsesRemote(t *testing.T) {
	f := desktopTabsFile{
		Tabs:       []desktopTabEntry{{ID: "l1"}, {ID: "l2"}},
		RemoteTabs: []desktopRemoteTabEntry{{ID: "r1", HostID: "h", Workspace: "~/a"}, {ID: "r2", HostID: "h", Workspace: "~/b"}},
		ActiveTab:  "r1",
	}
	out := singleSurfaceTabsFile(f)
	if len(out.Tabs) != 0 || len(out.RemoteTabs) != 1 || out.RemoteTabs[0].ID != "r1" {
		t.Fatalf("single-surface collapse = %+v", out)
	}
	if out.ActiveTab != "r1" {
		t.Fatalf("collapsed ActiveTab = %q, want the remote r1", out.ActiveTab)
	}
}

func TestSingleSurfaceTabsFilePrefersActiveLocalOverRemote(t *testing.T) {
	f := desktopTabsFile{
		Tabs:       []desktopTabEntry{{ID: "l1"}, {ID: "l2"}},
		RemoteTabs: []desktopRemoteTabEntry{{ID: "r1", HostID: "h", Workspace: "~/a"}},
		ActiveTab:  "l2",
	}
	out := singleSurfaceTabsFile(f)
	if len(out.Tabs) != 1 || out.Tabs[0].ID != "l2" || len(out.RemoteTabs) != 0 || out.ActiveTab != "l2" {
		t.Fatalf("single-surface local collapse = %+v", out)
	}
}

// TestSuspendSkipsDisconnectedShells: host status transitions never flip a
// restored shell into a runtime state.
func TestSuspendSkipsDisconnectedShells(t *testing.T) {
	a := &App{}
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{
		"shell": {id: "shell", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "disconnected"},
		"live":  {id: "live", ref: RemoteTabRef{HostID: "box", Workspace: "~/web"}, state: "ready"},
	}
	a.remoteTabMu.Unlock()
	a.suspendRemoteTabPumps("box", "reconnecting", "")
	a.remoteTabMu.Lock()
	defer a.remoteTabMu.Unlock()
	if a.remoteTabs["shell"].state != "disconnected" {
		t.Fatalf("shell state = %q, want disconnected", a.remoteTabs["shell"].state)
	}
	if a.remoteTabs["live"].state != "reconnecting" {
		t.Fatalf("live state = %q, want reconnecting", a.remoteTabs["live"].state)
	}
}

// TestTabsFileWithoutRemoteTabsKeepsLegacyShape: with no remote tabs open the
// persisted file carries no remote keys, so local-only usage stays
// byte-compatible with the pre-remote format.
func TestTabsFileWithoutRemoteTabsKeepsLegacyShape(t *testing.T) {
	seedBridgeTestHost(t, "box")
	a := &App{}
	seedLocalTab(a, "l1")
	a.mu.Lock()
	a.activeTabID = "l1"
	a.mu.Unlock()
	a.saveTabsFromRemote()
	data, err := os.ReadFile(filepath.Join(config.ReasonixHomeDir(), tabsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "remoteTabs") || strings.Contains(string(data), "remoteTabOrder") {
		t.Fatalf("tabs file mentions remote keys with none open:\n%s", data)
	}
}
