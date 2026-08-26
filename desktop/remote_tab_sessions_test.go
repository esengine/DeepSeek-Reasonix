package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestRemoteTabReconnectDoesNotLogEnsureServerSecrets(t *testing.T) {
	const secret = "sk-sensitive-reconnect-error"
	kernel := &fakeRemoteKernel{ensureErr: errors.New("remote bootstrap failed: " + secret)}
	a := &App{
		remoteRuntime: kernel,
		remoteTabs: map[string]*remoteTab{
			"remote-1": {
				id:    "remote-1",
				ref:   RemoteTabRef{HostID: "box", Workspace: "~/app"},
				state: "reconnecting",
			},
		},
	}

	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })

	a.reattachRemoteTab("remote-1")
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("reconnect log exposed EnsureServer error: %q", logs.String())
	}
	if !strings.Contains(logs.String(), "EnsureServer NOT-READY") {
		t.Fatalf("reconnect failure was not logged structurally: %q", logs.String())
	}
	a.remoteTabMu.Lock()
	state := a.remoteTabs["remote-1"].state
	a.remoteTabMu.Unlock()
	if kernel.ensureCalls != remoteTabReattachAttempts || state != "serve_down" {
		t.Fatalf("reattach exhaustion calls/state = %d/%q", kernel.ensureCalls, state)
	}
}

func TestCompleteRemoteTabTurnMakesFreshSessionNonReusable(t *testing.T) {
	a := &App{remoteTabs: map[string]*remoteTab{
		"remote": {
			id:            "remote",
			gen:           7,
			session:       remoteTabSessionState{reset: true},
			pendingEvents: map[string]json.RawMessage{"approval_request:1": json.RawMessage(`{"kind":"approval_request"}`)},
		},
	}}
	a.completeRemoteTabTurn("remote", 6)
	if !a.remoteTabs["remote"].session.reset {
		t.Fatal("a stale stream generation cleared the reusable blank marker")
	}
	a.completeRemoteTabTurn("remote", 7)
	if a.remoteTabs["remote"].session.reset {
		t.Fatal("a completed turn remained eligible for blank-session reuse")
	}
	if len(a.remoteTabs["remote"].pendingEvents) != 0 {
		t.Fatal("turn completion did not clear pending prompt replay")
	}
}

func TestMigrateBlankRemoteSessionTitleOverride(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := setRemoteSessionTitleOverride("box", "~/app", "", "My first topic"); err != nil {
		t.Fatal(err)
	}
	if err := setRemoteSessionPinned("box", "~/app", "", true); err != nil {
		t.Fatal(err)
	}
	got, err := migrateRemoteSessionTitleOverride("box", "~/app", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "My first topic" || remoteSessionTitleOverride("box", "~/app", "session-1") != got {
		t.Fatalf("migrated title = %q, named preference = %q", got, remoteSessionTitleOverride("box", "~/app", "session-1"))
	}
	if blank := remoteSessionTitleOverride("box", "~/app", ""); blank != "" {
		t.Fatalf("blank preference survived migration: %q", blank)
	}
	if !remoteSessionPinned("box", "~/app", "session-1") || remoteSessionPinned("box", "~/app", "") {
		t.Fatal("blank-session pin did not move to the durable session identity")
	}
}

func TestSetRemoteTabModelFailureKeepsPreviousModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{
		Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY",
	})
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev", CredentialMode: "local-proxy"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	// Clear the stored key after the tab seeds its default. The proxy switch
	// must then fail while preserving that model.
	if _, err := config.SetCredential("DEEPSEEK_API_KEY", ""); err != nil {
		t.Fatalf("clear credential: %v", err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "")
	if err := a.SetModelForTab(meta.ID, "deepseek/deepseek-v4-pro"); err == nil {
		t.Fatal("SetModelForTab must fail without the local key")
	}
	a.remoteTabMu.Lock()
	model := ""
	if tab := a.remoteTabs[meta.ID]; tab != nil {
		model = tab.model
	}
	a.remoteTabMu.Unlock()
	if model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("tab.model = %q, want the untouched previous model", model)
	}
}

func TestRemoteProxyModelCatalogCannotCrossProviderProtocols(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OPENAI_TEST_KEY", "sk-openai")
	setDesktopTestCredential(t, "ANTHROPIC_TEST_KEY", "sk-anthropic")
	cfg := config.Default()
	cfg.DefaultModel = "chat/gpt-test"
	cfg.Providers = []config.ProviderEntry{
		{Name: "chat", Kind: "openai", BaseURL: "https://chat.example/v1", Models: []string{"gpt-test", "gpt-next"}, Default: "gpt-test", APIKeyEnv: "OPENAI_TEST_KEY"},
		{Name: "claude", Kind: "anthropic", BaseURL: "https://claude.example", Models: []string{"claude-test"}, Default: "claude-test", APIKeyEnv: "ANTHROPIC_TEST_KEY"},
	}
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", CredentialMode: "local-proxy"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	a := &App{remoteTabs: map[string]*remoteTab{
		"remote": {id: "remote", ref: RemoteTabRef{HostID: "box", Workspace: "~/app"}, state: "ready", model: "chat/gpt-test"},
	}}
	models := a.ModelsForTab("remote")
	if len(models) != 2 || slices.ContainsFunc(models, func(model ModelInfo) bool { return model.Provider == "claude" }) {
		t.Fatalf("local-proxy catalog crossed protocols: %+v", models)
	}
	err := a.SetRemoteTabModel("remote", "claude/claude-test")
	if err == nil || !strings.Contains(err.Error(), "must be restarted to change protocol") {
		t.Fatalf("cross-protocol switch error = %v", err)
	}
}

// TestRemoteTabSnapshotMergesServeMembers: all six GETs merge in parallel;
// only /history is required — its failure errors, optional members degrade.
func TestRemoteTabSnapshotMergesServeMembers(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	snap, err := a.RemoteTabSnapshot(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]json.RawMessage{
		"history": snap.History, "context": snap.Context, "todos": snap.Todos,
		"checkpoints": snap.Checkpoints, "models": snap.Models, "status": snap.Status,
	} {
		if len(raw) == 0 {
			t.Fatalf("snapshot member %s is empty", name)
		}
	}
	before := len(fs.recorded())
	if _, err := a.RemoteTabStatus(meta.ID); err != nil {
		t.Fatal(err)
	}
	statusCalls := fs.recorded()[before:]
	if len(statusCalls) != 1 || !strings.HasPrefix(statusCalls[0], "GET /status") {
		t.Fatalf("status-only binding fetched extra snapshot members: %v", statusCalls)
	}

	fs.mu.Lock()
	fs.failHistory = true
	fs.mu.Unlock()
	if _, err := a.RemoteTabSnapshot(meta.ID); err == nil {
		t.Fatal("snapshot with failing /history must error")
	}
}

// TestRemoteProjectSessionsWithoutOpenTab pins the read-only one-shot path:
// listing sessions for a workspace with no live tab reuses the registry's
// ready registration, handshakes, and maps entries to the frontend view —
// without ever ensuring a serve.
func TestRemoteProjectSessionsWithoutOpenTab(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/x.jsonl", Title: "First", Turns: 2},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}

	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "s1" || sessions[0].Title != "First" || sessions[0].Turns != 2 {
		t.Fatalf("sessions = %+v, want the mapped s1 entry", sessions)
	}
	found := false
	for _, c := range fs.recorded() {
		if c == "GET /sessions " {
			found = true
		}
	}
	if !found {
		t.Fatalf("GET /sessions not reached: %v", fs.recorded())
	}
	if kernel.ensureCalls != 0 {
		t.Fatalf("listing woke the serve: %d EnsureServer calls", kernel.ensureCalls)
	}
}

// TestRemoteProjectSessionsNeverWakesServe: a query path must never
// cold-start a serve — no ready registration means an error, and EnsureServer
// must not even be attempted (the old behavior here starved tab bootstraps
// on the per-host serve lock).
func TestRemoteProjectSessionsNeverWakesServe(t *testing.T) {
	kernel := &fakeRemoteKernel{
		statuses: []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		// No ready registration: ServeSnapshot reports nothing.
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}

	if _, err := a.RemoteProjectSessions("box", "~/app"); err == nil {
		t.Fatal("listing must report the serve as not running")
	}
	if kernel.ensureCalls != 0 {
		t.Fatalf("listing woke the serve: %d EnsureServer calls", kernel.ensureCalls)
	}
}

// TestResolveOverlappingWorkspace pins the merge rules for overlapping pins:
// exact match wins, then the nearest ancestor, then the shallowest
// descendant; disjoint paths never merge.
func TestResolveOverlappingWorkspace(t *testing.T) {
	entries := []config.RemoteProjectEntry{
		{HostID: "box", Workspace: "/srv/app"},
		{HostID: "box", Workspace: "/srv/app/sub"},
		{HostID: "other", Workspace: "/srv/app"},
	}
	for _, tc := range []struct {
		ws   string
		want string
		ok   bool
	}{
		{ws: "/srv/app", want: "/srv/app", ok: true},              // exact
		{ws: "/srv/app/", want: "/srv/app", ok: true},             // trailing slash normalizes to exact
		{ws: "/srv/app/sub/deep", want: "/srv/app/sub", ok: true}, // nearest ancestor
		{ws: "/srv", want: "/srv/app", ok: true},                  // ancestor request merges into shallowest descendant
		{ws: "/srv/other", want: "", ok: false},                   // sibling never merges
		{ws: "", want: "", ok: false},                             // empty never merges
	} {
		got, ok := resolveOverlappingWorkspace(entries, "box", tc.ws)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("resolveOverlappingWorkspace(%q) = (%q, %v), want (%q, %v)", tc.ws, got, ok, tc.want, tc.ok)
		}
	}
	// Host scoping: the same path on another host must not capture the merge.
	if got, ok := resolveOverlappingWorkspace(entries, "other", "/srv/app/sub/x"); !ok || got != "/srv/app" {
		t.Fatalf("cross-host overlap merged: (%q, %v)", got, ok)
	}
}

// TestRemoteTabCommandSurfacesServeErrorBody: the serve's error text (the
// session-in-use close hint) rides through to the caller.
func TestRemoteTabCommandSurfacesServeErrorBody(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	fs.mu.Lock()
	fs.failNext = "session in use; close the remote tab first"
	fs.mu.Unlock()
	err := a.SubmitRemoteTab(meta.ID, "hello")
	if err == nil || !strings.Contains(err.Error(), "close the remote tab first") {
		t.Fatalf("err = %v, want the serve error body surfaced", err)
	}
}

// TestCloseRemoteTabIsIdempotent: closing removes the registry entry, stops
// the pump, and a second close is a no-op.
func TestCloseRemoteTabIsIdempotent(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedClassicBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	if err := a.CloseRemoteTab(meta.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.CloseRemoteTab(meta.ID); err != nil {
		t.Fatalf("second close: %v", err)
	}
	a.remoteTabMu.Lock()
	_, present := a.remoteTabs[meta.ID]
	a.remoteTabMu.Unlock()
	if present {
		t.Fatal("closed tab still in the registry")
	}
	if err := a.SubmitRemoteTab(meta.ID, "hi"); err == nil {
		t.Fatal("commands on a closed tab must fail")
	}
}

// TestRemoteTabFollowsHostReconnect pins the SSH-driven lifecycle: a
// transient drop suspends the pump and flags reconnecting, the regained
// connection re-attaches a fresh pump to the still-running serve, and a
// terminal failure parks the tab in error.
func TestRemoteTabFollowsHostReconnect(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	events := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: events.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	firstConns := fs.eventsCount()

	a.remoteTabsHostStatus("box", "reconnecting", "")
	waitForTabState(t, a, meta.ID, "reconnecting")
	if events.count("remote-tab:"+meta.ID+":state ") == 0 {
		t.Fatal("host disconnect changed backend state without emitting a tab-state event")
	}
	if err := a.SubmitRemoteTab(meta.ID, "hi"); err == nil {
		t.Fatal("commands during reconnecting must fail")
	}

	a.remoteTabsHostStatus("box", "connected", "")
	waitForTabState(t, a, meta.ID, "ready")
	if fs.eventsCount() <= firstConns {
		t.Fatalf("re-attach did not open a new event stream: %d then %d", firstConns, fs.eventsCount())
	}
	if err := a.SubmitRemoteTab(meta.ID, "back online"); err != nil {
		t.Fatalf("submit after reconnect: %v", err)
	}

	a.remoteTabsHostStatus("box", "stopped", "ssh: auth failed")
	waitForTabState(t, a, meta.ID, "error")
	a.remoteTabMu.Lock()
	tabErr := a.remoteTabs[meta.ID].err
	a.remoteTabMu.Unlock()
	if !strings.Contains(tabErr, "ssh: auth failed") {
		t.Fatalf("tab error = %q, want the host failure text", tabErr)
	}
}

func TestCloseActiveRemoteTabSelectsAdjacentLocalTab(t *testing.T) {
	a := &App{}
	seedLocalTab(a, "local-a")
	seedLocalTab(a, "local-b")
	a.mu.Lock()
	a.activeTabID = "local-a"
	a.mu.Unlock()
	a.remoteTabMu.Lock()
	a.remoteTabs = map[string]*remoteTab{"remote": {id: "remote", state: "ready"}}
	a.remoteTabLayout = remoteTabLayoutState{activeID: "remote", order: []string{"remote"}, stripOrder: []string{"local-a", "remote", "local-b"}}
	a.remoteTabMu.Unlock()
	if err := a.CloseRemoteTab("remote"); err != nil {
		t.Fatal(err)
	}
	a.mu.RLock()
	active := a.activeTabID
	a.mu.RUnlock()
	if active != "local-b" {
		t.Fatalf("active local tab = %q, want adjacent local-b", active)
	}
}

func TestStopRemoteServerParksTabsWithoutRestart(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:   []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView: RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL}, ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})
	before := kernel.ensureCalls
	if err := a.StopRemoteServer("box", "~/app"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	a.remoteTabMu.Lock()
	state, client := a.remoteTabs[meta.ID].state, a.remoteTabs[meta.ID].client
	a.remoteTabMu.Unlock()
	if state != "serve_down" || client != nil {
		t.Fatalf("stopped tab state/client = %q/%v", state, client)
	}
	if kernel.ensureCalls != before {
		t.Fatalf("explicit stop restarted Serve: EnsureServer calls %d -> %d", before, kernel.ensureCalls)
	}
}

// TestListTabsIncludesRemoteEntries pins the strip integration: open remote
// tabs appear in ListTabs, a highlighted remote tab deactivates the local
// entries, and SetActiveTab routes by registry membership.
func TestListTabsIncludesRemoteEntries(t *testing.T) {
	fs := newFakeServe(t, "s3cret", nil)
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedClassicBridgeTestHost(t, "box")
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	tabs := a.ListTabs()
	var remote TabMeta
	found := false
	for _, tab := range tabs {
		if tab.ID == meta.ID {
			remote, found = tab, true
		}
	}
	if !found {
		t.Fatalf("remote tab missing from ListTabs: %+v", tabs)
	}
	if remote.Remote == nil || remote.Remote.HostID != "box" {
		t.Fatalf("remote meta ref = %+v", remote.Remote)
	}
	if !remote.Active {
		t.Fatal("freshly opened remote tab must carry the strip highlight")
	}

	if err := a.SetActiveTab(meta.ID); err != nil {
		t.Fatalf("SetActiveTab(remote): %v", err)
	}
	a.remoteTabMu.Lock()
	active := a.remoteTabLayout.activeID
	a.remoteTabMu.Unlock()
	if active != meta.ID {
		t.Fatalf("remoteActiveTabID = %q, want %q", active, meta.ID)
	}
	if err := a.CloseTabWithPolicy(meta.ID, "keep_running"); err != nil {
		t.Fatalf("CloseTabWithPolicy(remote): %v", err)
	}
	a.remoteTabMu.Lock()
	_, present := a.remoteTabs[meta.ID]
	a.remoteTabMu.Unlock()
	if present {
		t.Fatal("CloseTabWithPolicy left the remote tab registered")
	}
}

// TestRemoteTabTitleAdoptsServeSession pins the title pipeline: the serve's
// LLM-generated title for the current session replaces the workspace-name
// default and reaches the chrome through the metadata-only update channel.
func TestRemoteTabTitleAdoptsServeSession(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/x.jsonl", Title: "Fix the login bug", Turns: 1, Current: true},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	// Resume the seeded session so it stays current; /new would abandon it.
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "s1"})
	log.mu.Lock()
	log.events = nil
	log.mu.Unlock()

	a.refreshRemoteTabTitle(meta.ID)

	a.remoteTabMu.Lock()
	title := a.remoteTabs[meta.ID].topicTitle
	a.remoteTabMu.Unlock()
	if title != "Fix the login bug" {
		t.Fatalf("topicTitle = %q, want the serve title", title)
	}
	found := false
	for _, e := range log.recorded() {
		if strings.HasPrefix(e, "remote-tab:updated ") && strings.Contains(e, meta.ID) && strings.Contains(e, "Fix the login bug") {
			found = true
		}
	}
	if !found {
		t.Fatalf("title refresh not pushed to the chrome: %v", log.recorded())
	}
	for _, tab := range a.ListTabs() {
		if tab.ID == meta.ID && tab.TopicTitle != "Fix the login bug" {
			t.Fatalf("ListTabs title = %q", tab.TopicTitle)
		}
	}
}

// TestRemoteTabNewSessionResetsServeSession: a NewSession open on an
// existing tab POSTs /new (the old session stays in the history list) and
// re-emits ready so the frontend re-syncs its snapshot.
func TestRemoteTabNewSessionResetsServeSession(t *testing.T) {
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/remote/sessions/s1.jsonl", Title: "Serve title", Turns: 1, Current: true},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	seedBridgeTestHost(t, "box")
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{NewSession: true})

	// The bootstrap already entered a fresh session; the listing carries the
	// desktop-view blank (the serve abandoned s1 and lists no current row).
	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) == 0 || sessions[0].Name != "" || !sessions[0].Current || sessions[0].Title != "新的会话" {
		t.Fatalf("sessions = %+v, want the synthetic blank leading the listing", sessions)
	}
	if err := a.RenameRemoteProjectSession("box", "~/app", "", "空白会话标题"); err != nil {
		t.Fatal(err)
	}
	sessions, err = a.RemoteProjectSessions("box", "~/app")
	if err != nil || len(sessions) == 0 || sessions[0].Title != "空白会话标题" {
		t.Fatalf("renamed blank session = %+v, err=%v", sessions, err)
	}

	// A further new-session open reuses the blank: no extra POST /new — the
	// same contract as the local reusable-blank tab.
	newBefore, _, _ := fs.snapshot()
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true}); err != nil {
		t.Fatal(err)
	}
	if newAfter, _, _ := fs.snapshot(); newAfter != newBefore {
		t.Fatalf("POST /new called %d times after reuse, want %d", newAfter, newBefore)
	}

	// Resuming a listed session clears the blank and restores it as current.
	if _, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{SessionName: "s1"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		sessions, err = a.RemoteProjectSessions("box", "~/app")
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) > 0 && sessions[0].Name == "s1" && sessions[0].Current {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resume did not restore s1 as current: %+v", sessions)
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.remoteTabMu.Lock()
	reset := a.remoteTabs[meta.ID].session.reset
	title := a.remoteTabs[meta.ID].topicTitle
	a.remoteTabMu.Unlock()
	if reset {
		t.Fatal("sessionReset must clear after a resume")
	}
	if title != "Serve title" {
		t.Fatalf("topicTitle after resume = %q, want the serve title", title)
	}
}

// TestRenameRemoteProjectSession pins the desktop-owned title chain: the
// override wins in the session listing, and a live tab holding that session
// adopts the new title immediately; clearing falls back to the serve title.
func TestRenameRemoteProjectSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev"})
	}); err != nil {
		t.Fatal(err)
	}
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "s1", Path: "/x.jsonl", Title: "Serve title", Turns: 1, Current: true, MtimeMilli: 1700000000000},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	log := &eventLog{}
	a := &App{remoteRuntime: kernel, remoteEventHook: log.add}
	cleanupRemoteTabPumps(t, a)
	meta := openReadyRemoteTab(t, a, RemoteTabOpenOptions{SessionName: "s1"})

	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].LastActivityAt != 1700000000000 || sessions[0].Title != "Serve title" {
		t.Fatalf("sessions = %+v, want serve title + mtime passthrough", sessions)
	}

	if err := a.RenameRemoteProjectSession("box", "~/app", "s1", "我的新标题"); err != nil {
		t.Fatal(err)
	}
	sessions, err = a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if sessions[0].Title != "我的新标题" {
		t.Fatalf("override title = %q", sessions[0].Title)
	}
	a.remoteTabMu.Lock()
	title := a.remoteTabs[meta.ID].topicTitle
	a.remoteTabMu.Unlock()
	if title != "我的新标题" {
		t.Fatalf("live tab title = %q, want the override", title)
	}
	a.refreshRemoteTabTitle(meta.ID)
	a.remoteTabMu.Lock()
	title = a.remoteTabs[meta.ID].topicTitle
	a.remoteTabMu.Unlock()
	if title != "我的新标题" {
		t.Fatalf("automatic refresh replaced the manual title with %q", title)
	}

	if err := a.RenameRemoteProjectSession("box", "~/app", "s1", ""); err != nil {
		t.Fatal(err)
	}
	sessions, _ = a.RemoteProjectSessions("box", "~/app")
	if sessions[0].Title != "Serve title" {
		t.Fatalf("cleared override title = %q, want the serve title", sessions[0].Title)
	}
}

// TestRemoteSessionPinnedOrderingAndProjectTitle pins the desktop-owned
// row pin (pinned-first listing) and the registry-backed project rename.
func TestRemoteSessionPinnedOrderingAndProjectTitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "127.0.0.1", Port: 22, User: "dev"})
	}); err != nil {
		t.Fatal(err)
	}
	fs := newFakeServe(t, "s3cret", []serveSessionEntry{
		{Name: "a", Path: "/a.jsonl", Title: "First", Current: false, MtimeMilli: 1},
		{Name: "b", Path: "/b.jsonl", Title: "Second", Current: true, MtimeMilli: 2},
	})
	kernel := &fakeRemoteKernel{
		statuses:    []RemoteConnectionStatusView{{HostID: "box", State: "connected"}},
		ensureView:  RemoteServerView{HostID: "box", State: "ready", LocalURL: fs.server.URL},
		ensureToken: "s3cret",
	}
	a := &App{remoteRuntime: kernel}
	cleanupRemoteTabPumps(t, a)

	if err := a.SetRemoteSessionPinned("box", "~/app", "a", true); err != nil {
		t.Fatal(err)
	}
	sessions, err := a.RemoteProjectSessions("box", "~/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].Name != "a" || !sessions[0].Pinned || sessions[1].Pinned {
		t.Fatalf("sessions = %+v, want pinned a first", sessions)
	}

	meta, err := a.OpenRemoteProjectTab("box", "~/app", RemoteTabOpenOptions{NewSession: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = meta
	if err := a.SetRemoteProjectTitle("box", "~/app", "云端演示"); err != nil {
		t.Fatal(err)
	}
	for _, node := range a.ListTabs() {
		_ = node
	}
	found := false
	for _, node := range a.GetProjectTreeSnapshot().Projects {
		if node.Remote != nil && node.Remote.HostID == "box" {
			found = true
			if node.Label != "云端演示" {
				t.Fatalf("group label = %q, want the renamed title", node.Label)
			}
		}
	}
	if !found {
		t.Fatal("remote group missing from the snapshot")
	}
}
