package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/evidence"
)

// activationEventRecorder captures "topic:activation" events through the
// synchronous test hook, in emission order.
type activationEventRecorder struct {
	ch chan TopicActivationEvent
}

func newActivationEventRecorder(app *App) *activationEventRecorder {
	r := &activationEventRecorder{ch: make(chan TopicActivationEvent, 64)}
	app.activationEventHook = func(ev TopicActivationEvent) { r.ch <- ev }
	return r
}

// next returns the next event, failing the test if none arrives.
func (r *activationEventRecorder) next(t *testing.T) TopicActivationEvent {
	t.Helper()
	select {
	case ev := <-r.ch:
		return ev
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for a topic activation event")
		return TopicActivationEvent{}
	}
}

// waitFor drains events until pred matches and returns that event.
func (r *activationEventRecorder) waitFor(t *testing.T, pred func(TopicActivationEvent) bool) TopicActivationEvent {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case ev := <-r.ch:
			if pred(ev) {
				return ev
			}
		case <-deadline:
			t.Fatal("timed out waiting for the expected topic activation event")
			return TopicActivationEvent{}
		}
	}
}

// drainEmpty asserts no further events are pending.
func (r *activationEventRecorder) drainEmpty(t *testing.T) {
	t.Helper()
	for {
		select {
		case ev := <-r.ch:
			t.Fatalf("unexpected extra topic activation event: %+v", ev)
		default:
			return
		}
	}
}

// tabBuildGate blocks every tab controller build at its entry until the test
// releases that tab, letting activation tests force out-of-order build
// completion without sleeps.
type tabBuildGate struct {
	mu      sync.Mutex
	gates   map[string]chan struct{}
	entered chan string
}

func newTabBuildGate(app *App) *tabBuildGate {
	g := &tabBuildGate{
		gates:   map[string]chan struct{}{},
		entered: make(chan string, 64),
	}
	app.tabBuildStartHook = func(tabID string) {
		g.mu.Lock()
		ch := g.gates[tabID]
		if ch == nil {
			ch = make(chan struct{})
			g.gates[tabID] = ch
		}
		g.mu.Unlock()
		g.entered <- tabID
		<-ch
	}
	return g
}

// waitEntered fails the test unless a build for tabID reaches the gate.
func (g *tabBuildGate) waitEntered(t *testing.T, tabID string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case got := <-g.entered:
			if got == tabID {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for tab %q build to reach the gate", tabID)
		}
	}
}

func (g *tabBuildGate) release(tabID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ch := g.gates[tabID]; ch != nil {
		close(ch)
		delete(g.gates, tabID)
	}
}

// releaseAll unblocks every gated build; safe to call from test cleanup.
func (g *tabBuildGate) releaseAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, ch := range g.gates {
		close(ch)
		delete(g.gates, id)
	}
}

func activationEventFor(requestID, phase string) func(TopicActivationEvent) bool {
	return func(ev TopicActivationEvent) bool {
		return ev.RequestID == requestID && ev.Phase == phase
	}
}

// flushActivationCompletions gives superseded completion goroutines a
// deterministic sync point: they must pass singleSurfaceMu before doing
// anything observable, so round-tripping the mutex behind them (blocked
// Lockers queue FIFO) proves their guarded-off generation check has run.
func flushActivationCompletions(app *App) {
	done := make(chan struct{})
	go func() {
		app.singleSurfaceMu.Lock()
		defer app.singleSurfaceMu.Unlock()
		close(done)
	}()
	<-done
}

func TestStartTopicActivationRapidSwitchOutOfOrderBuilds(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	installNoopRuntimeEvents(app)
	events := newActivationEventRecorder(app)
	gate := newTabBuildGate(app)
	t.Cleanup(func() {
		gate.releaseAll()
		app.shutdown(context.Background())
	})

	ticketA, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-a", RequestID: "req-a"})
	if err != nil {
		t.Fatalf("StartTopicActivation A: %v", err)
	}
	if ticketA.RequestID != "req-a" || ticketA.TabID == "" || ticketA.Meta.ID != ticketA.TabID {
		t.Fatalf("ticket A = %+v, want echoed requestId and consistent tab meta", ticketA)
	}
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-a", TabID: ticketA.TabID, Phase: "starting"}) {
		t.Fatalf("first event = %+v, want starting req-a", got)
	}
	gate.waitEntered(t, ticketA.TabID)

	ticketB, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-b", RequestID: "req-b"})
	if err != nil {
		t.Fatalf("StartTopicActivation B: %v", err)
	}
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-a", TabID: ticketA.TabID, Phase: "cancelled"}) {
		t.Fatalf("event = %+v, want cancelled req-a", got)
	}
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-b", TabID: ticketB.TabID, Phase: "starting"}) {
		t.Fatalf("event = %+v, want starting req-b", got)
	}
	gate.waitEntered(t, ticketB.TabID)

	ticketC, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-c", RequestID: "req-c"})
	if err != nil {
		t.Fatalf("StartTopicActivation C: %v", err)
	}
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-b", TabID: ticketB.TabID, Phase: "cancelled"}) {
		t.Fatalf("event = %+v, want cancelled req-b", got)
	}
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-c", TabID: ticketC.TabID, Phase: "starting"}) {
		t.Fatalf("event = %+v, want starting req-c", got)
	}
	gate.waitEntered(t, ticketC.TabID)

	// Forced out-of-order completion: C builds first and wins; A and B then
	// abandon through the superseded-build path. All gates must be released
	// before waiting on C's ready: the winner's prune needs the runtime
	// admission write side, which the gated loser builds hold read-side.
	gate.release(ticketC.TabID)
	gate.release(ticketA.TabID)
	gate.release(ticketB.TabID)

	ready := events.waitFor(t, activationEventFor("req-c", "ready"))
	if ready.TabID != ticketC.TabID {
		t.Fatalf("ready event tab = %q, want %q", ready.TabID, ticketC.TabID)
	}
	flushActivationCompletions(app)
	events.drainEmpty(t)

	tabs := app.ListTabs()
	if len(tabs) != 1 || tabs[0].ID != ticketC.TabID || !tabs[0].Active {
		t.Fatalf("ListTabs = %+v, want only the active winner tab %q", tabs, ticketC.TabID)
	}
	app.mu.RLock()
	detached := len(app.detachedSessions)
	runtimes := len(app.runtimeByID)
	app.mu.RUnlock()
	if detached != 0 {
		t.Fatalf("detached sessions = %d, want 0 (building tabs must be torn down, not detached)", detached)
	}
	if runtimes != 1 {
		t.Fatalf("runtime registry entries = %d, want 1 (only the winner)", runtimes)
	}

	// Loser session leases must be gone; the winner's lease stays held.
	for _, ticket := range []TopicActivationTicket{ticketA, ticketB} {
		lease, err := agent.TryAcquireSessionLease(sessionRuntimeKey(ticket.Meta.SessionPath))
		if err != nil {
			t.Fatalf("session lease for superseded tab %q is still held: %v", ticket.TabID, err)
		}
		lease.Release()
	}
	if lease, err := agent.TryAcquireSessionLease(sessionRuntimeKey(ticketC.Meta.SessionPath)); err == nil {
		lease.Release()
		t.Fatal("winner session lease was not held after activation")
	}
}

func TestStartTopicActivationSyncBuildAndReuseFastPath(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp() // a.ctx == nil: builds run synchronously inside the call
	app.readyHook = func() {}
	installNoopRuntimeEvents(app)
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	ticket, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-sync", RequestID: "req-1"})
	if err != nil {
		t.Fatalf("StartTopicActivation: %v", err)
	}
	if got := events.next(t); got.Phase != "starting" || got.RequestID != "req-1" {
		t.Fatalf("event = %+v, want starting req-1", got)
	}
	ready := events.waitFor(t, activationEventFor("req-1", "ready"))
	if ready.TabID != ticket.TabID {
		t.Fatalf("ready tab = %q, want %q", ready.TabID, ticket.TabID)
	}

	app.mu.RLock()
	tab := app.tabs[ticket.TabID]
	var ctrlAfterFirst control.SessionAPI
	if tab != nil {
		ctrlAfterFirst = tab.Ctrl
	}
	app.mu.RUnlock()
	if tab == nil || ctrlAfterFirst == nil {
		t.Fatal("tab missing or controller not built after ready")
	}

	// Reuse/fast path: reactivating the same topic reuses the tab, starts no
	// new build, and still emits exactly one ready after pruning.
	ticket2, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-sync", RequestID: "req-2"})
	if err != nil {
		t.Fatalf("StartTopicActivation reuse: %v", err)
	}
	if ticket2.TabID != ticket.TabID {
		t.Fatalf("reuse ticket tab = %q, want reused %q", ticket2.TabID, ticket.TabID)
	}
	if got := events.next(t); got.Phase != "starting" || got.RequestID != "req-2" {
		t.Fatalf("event = %+v, want starting req-2", got)
	}
	events.waitFor(t, activationEventFor("req-2", "ready"))
	flushActivationCompletions(app)
	events.drainEmpty(t)

	app.mu.RLock()
	sameTab := app.tabs[ticket.TabID] == tab
	sameCtrl := tab.Ctrl == ctrlAfterFirst
	tabCount := len(app.tabs)
	app.mu.RUnlock()
	if !sameTab || !sameCtrl {
		t.Fatal("reuse activation rebuilt or replaced the tab/controller")
	}
	if tabCount != 1 {
		t.Fatalf("tab count = %d, want 1", tabCount)
	}
}

// activationStubController is the minimal SessionAPI surface exercised by
// prune/detach/attach flows around a tab with active runtime work.
type activationStubController struct {
	control.SessionAPI
	sessionPath string
	closed      atomic.Bool
}

func (c *activationStubController) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{Running: true}
}
func (c *activationStubController) SessionPath() string      { return c.sessionPath }
func (c *activationStubController) SetSessionPath(p string)  { c.sessionPath = p }
func (c *activationStubController) SessionDir() string       { return "" }
func (c *activationStubController) Snapshot() error          { return nil }
func (c *activationStubController) Cancel()                  {}
func (c *activationStubController) Close()                   { c.closed.Store(true) }
func (c *activationStubController) Label() string            { return "stub-model" }
func (c *activationStubController) ReplayPendingPrompts()    {}
func (c *activationStubController) PlanMode() bool           { return false }
func (c *activationStubController) AutoApproveTools() bool   { return false }
func (c *activationStubController) ToolApprovalMode() string { return "" }
func (c *activationStubController) Goal() string             { return "" }
func (c *activationStubController) GoalStatus() string       { return "" }
func (c *activationStubController) Turn() int                { return 0 }
func (c *activationStubController) GoalRuntime() control.GoalRuntimeView {
	return control.GoalRuntimeView{}
}
func (c *activationStubController) Todos() []evidence.TodoItem { return nil }
func (c *activationStubController) SnapshotForShutdown() error { return nil }

func TestStartTopicActivationFailureDetachesPreviousAndReattaches(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	sessionDir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	pathA := writeTopicSession(t, sessionDir, "a.jsonl", "topic-a", "Topic A", projectRoot)
	pathB := writeTopicSession(t, sessionDir, "b.jsonl", "topic-b", "Topic B", projectRoot)

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	stub := &activationStubController{sessionPath: pathA}
	tabA := &WorkspaceTab{
		ID:            "tab-a",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-a",
		TopicTitle:    "Topic A",
		SessionPath:   pathA,
		Ctrl:          stub,
		Label:         "stub-model",
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	tabA.sink = &tabEventSink{tabID: tabA.ID, app: app}
	installNoopRuntimeEvents(app, tabA.sink)
	if err := tabA.ensureSessionLease(pathA); err != nil {
		t.Fatalf("ensureSessionLease A: %v", err)
	}
	app.tabs[tabA.ID] = tabA
	app.tabOrder = []string{tabA.ID}
	app.activeTabID = tabA.ID

	// An external holder makes topic B's build fail with the lease-held error.
	externalLease, err := agent.TryAcquireSessionLease(sessionRuntimeKey(pathB))
	if err != nil {
		t.Fatalf("external lease for B: %v", err)
	}
	defer externalLease.Release()

	ticketB, err := app.StartTopicActivation(TopicActivationRequest{
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-b",
		RequestID:     "req-b",
	})
	if err != nil {
		t.Fatalf("StartTopicActivation B: %v", err)
	}
	failed := events.waitFor(t, activationEventFor("req-b", "failed"))
	if failed.TabID != ticketB.TabID {
		t.Fatalf("failed event tab = %q, want %q", failed.TabID, ticketB.TabID)
	}
	if !strings.Contains(failed.Error, "already open in another Reasonix window") {
		t.Fatalf("failed error = %q, want the sanitized lease-busy message", failed.Error)
	}
	if strings.Contains(failed.Error, pathB) {
		t.Fatalf("failed error leaks the session path: %q", failed.Error)
	}

	// Failure preserves the previous runtime: A had active work, so the prune
	// detached it instead of tearing it down — controller open, lease held.
	app.mu.RLock()
	detached := app.detachedSessions[sessionRuntimeKey(pathA)]
	app.mu.RUnlock()
	if detached != tabA {
		t.Fatal("tab A with active work was not detached after the failed switch")
	}
	if stub.closed.Load() {
		t.Fatal("detached tab A controller was closed")
	}
	if lease, err := agent.TryAcquireSessionLease(sessionRuntimeKey(pathA)); err == nil {
		lease.Release()
		t.Fatal("tab A session lease was released by the failed switch")
	}

	// Send routing stays tab-scoped: the pruned tab ID resolves nowhere, the
	// visible (failed) tab has no controller, and nothing crosses into either.
	if tab, ctrl := app.tabAndCtrlByID(tabA.ID); tab != nil || ctrl != nil {
		t.Fatal("pruned tab A still resolves through send routing")
	}
	app.CancelTab(tabA.ID) // must be a no-op, must not touch anything else

	// Switching back reattaches the detached runtime instead of rebuilding it.
	ticketA2, err := app.StartTopicActivation(TopicActivationRequest{
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-a",
		RequestID:     "req-a2",
	})
	if err != nil {
		t.Fatalf("StartTopicActivation back to A: %v", err)
	}
	events.waitFor(t, activationEventFor("req-a2", "ready"))

	app.mu.RLock()
	reattached := app.tabs[ticketA2.TabID]
	detachedLeft := len(app.detachedSessions)
	app.mu.RUnlock()
	if reattached == nil || reattached.Ctrl != stub {
		t.Fatal("switching back did not reattach the detached stub runtime")
	}
	if stub.closed.Load() {
		t.Fatal("stub controller was closed during reattach")
	}
	if detachedLeft != 0 {
		t.Fatalf("detached sessions after reattach = %d, want 0", detachedLeft)
	}
	flushActivationCompletions(app)
	events.drainEmpty(t)
}

func TestActivateTopicSupersedesPendingTicketedActivation(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	readyCh := make(chan struct{}, 8)
	app.readyHook = func() { readyCh <- struct{}{} }
	installNoopRuntimeEvents(app)
	events := newActivationEventRecorder(app)
	gate := newTabBuildGate(app)
	t.Cleanup(func() {
		gate.releaseAll()
		app.shutdown(context.Background())
	})

	ticketA, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-a", RequestID: "req-a"})
	if err != nil {
		t.Fatalf("StartTopicActivation A: %v", err)
	}
	if got := events.next(t); got.Phase != "starting" {
		t.Fatalf("event = %+v, want starting", got)
	}
	gate.waitEntered(t, ticketA.TabID)
	app.mu.RLock()
	tabA := app.tabs[ticketA.TabID]
	var tabABuildDone chan struct{}
	if tabA != nil {
		// Snapshot the build-done channel now: closeTabBuildDone nils the
		// field after closing it, so reading it after the fact would block on
		// a nil channel forever.
		tabABuildDone = tabA.buildDone
	}
	app.mu.RUnlock()
	if tabA == nil {
		t.Fatal("tab A missing after activation start")
	}

	// A legacy ActivateTopic call supersedes the pending ticketed activation
	// and keeps its own synchronous contract: it returns after the prune.
	type legacyResult struct {
		meta TabMeta
		err  error
	}
	legacyDone := make(chan legacyResult, 1)
	go func() {
		meta, err := app.ActivateTopic("global", "", "topic-b", "")
		legacyDone <- legacyResult{meta: meta, err: err}
	}()
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-a", TabID: ticketA.TabID, Phase: "cancelled"}) {
		t.Fatalf("event = %+v, want cancelled req-a", got)
	}
	// The legacy prune queues the runtime-admission WRITE lock. Go's RWMutex
	// blocks new readers behind a queued writer, so tab B's gated build may
	// not be able to enter the gate until the prune has run — release A first
	// (its build abandons via the superseded path), which unblocks the prune
	// in the interleaving where B's build is still stuck behind the writer.
	app.mu.RLock()
	var tabBID string
	for id := range app.tabs {
		if id != ticketA.TabID {
			tabBID = id
		}
	}
	app.mu.RUnlock()
	if tabBID == "" {
		t.Fatal("legacy activation did not open tab B")
	}
	gate.release(ticketA.TabID)
	gate.waitEntered(t, tabBID)
	gate.release(tabBID)

	var legacy legacyResult
	select {
	case legacy = <-legacyDone:
	case <-time.After(15 * time.Second):
		t.Fatal("legacy ActivateTopic did not return")
	}
	if legacy.err != nil {
		t.Fatalf("legacy ActivateTopic: %v", legacy.err)
	}
	if legacy.meta.ID != tabBID {
		t.Fatalf("legacy meta tab = %q, want %q", legacy.meta.ID, tabBID)
	}
	<-readyCh // B's build published and emitted agent:ready

	assertTabIDs(t, app.ListTabs(), tabBID)
	<-tabABuildDone // A's abandoned build terminated
	flushActivationCompletions(app)
	// The legacy path emits no activation events of its own, and the
	// superseded completion stays silent.
	events.drainEmpty(t)
}

func TestSetActiveTabSupersedesPendingPublication(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	readyCh := make(chan struct{}, 8)
	app.readyHook = func() { readyCh <- struct{}{} }
	installNoopRuntimeEvents(app)
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	// Establish a settled visible tab through the legacy path (ungated).
	metaB, err := app.ActivateTopic("global", "", "topic-b", "")
	if err != nil {
		t.Fatalf("ActivateTopic B: %v", err)
	}
	select {
	case <-readyCh:
	case <-time.After(15 * time.Second):
		t.Fatal("tab B build did not finish")
	}

	gate := newTabBuildGate(app)
	t.Cleanup(gate.releaseAll)
	ticketA, err := app.StartTopicActivation(TopicActivationRequest{Scope: "global", TopicID: "topic-a", RequestID: "req-a"})
	if err != nil {
		t.Fatalf("StartTopicActivation A: %v", err)
	}
	if got := events.next(t); got.Phase != "starting" {
		t.Fatalf("event = %+v, want starting", got)
	}
	gate.waitEntered(t, ticketA.TabID)
	app.mu.RLock()
	tabA := app.tabs[ticketA.TabID]
	var tabABuildDone chan struct{}
	if tabA != nil {
		// Snapshot before the build terminates: closeTabBuildDone nils the
		// field after closing the channel.
		tabABuildDone = tabA.buildDone
	}
	app.mu.RUnlock()
	if tabA == nil || tabABuildDone == nil {
		t.Fatal("tab A missing or has no in-flight build")
	}

	// The user clicks tab B directly: the pending activation's publication
	// (prune + ready) is superseded, but its build is not cancelled — tab A
	// stays open and may legitimately become ready.
	if err := app.SetActiveTab(metaB.ID); err != nil {
		t.Fatalf("SetActiveTab B: %v", err)
	}
	if got := events.next(t); got != (TopicActivationEvent{RequestID: "req-a", TabID: ticketA.TabID, Phase: "cancelled"}) {
		t.Fatalf("event = %+v, want cancelled req-a", got)
	}
	gate.release(ticketA.TabID)
	select {
	case <-readyCh: // A's build still completes and publishes
	case <-time.After(15 * time.Second):
		t.Fatal("tab A build did not finish after SetActiveTab")
	}

	app.mu.RLock()
	tabAReady := tabA.Ready
	tabCount := len(app.tabs)
	active := app.activeTabID
	app.mu.RUnlock()
	if tabCount != 2 {
		t.Fatalf("tab count = %d, want 2 (SetActiveTab must not prune)", tabCount)
	}
	if active != metaB.ID {
		t.Fatalf("active tab = %q, want %q", active, metaB.ID)
	}
	if !tabAReady {
		t.Fatal("tab A build was cancelled or unpublished by SetActiveTab")
	}
	<-tabABuildDone
	flushActivationCompletions(app)
	events.drainEmpty(t)
}

func TestMetaForTabFastPathCachesExpensiveFields(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "CUSTOM_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "custom/vision-pro"
	cfg.Desktop.ProviderAccess = []string{"custom"}
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		APIKeyEnv:    "CUSTOM_KEY",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	repoRoot := t.TempDir()
	runGitIn(t, repoRoot, "init")
	runGitIn(t, repoRoot, "checkout", "-b", "feature/meta-cache")
	plainRoot := t.TempDir()

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	metaEvents := make(chan TabMetaRefreshEvent, 8)
	// NOTE: install the capture AFTER any installNoopRuntimeEvents call — that
	// helper overwrites app.runtimeEvents.emit.
	installNoopRuntimeEvents(app)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		if name != tabMetaRefreshEventChannel || len(payload) == 0 {
			return
		}
		if ev, ok := payload[0].(TabMetaRefreshEvent); ok {
			metaEvents <- ev
		}
	}
	var loads atomic.Int32
	var blockLoad atomic.Bool
	blockLoad.Store(true)
	loadEntered := make(chan struct{})
	var loadOnce sync.Once
	releaseLoad := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			blockLoad.Store(false)
			close(releaseLoad)
		})
	}
	app.configLoadForRootHook = func(string) {
		loads.Add(1)
		loadOnce.Do(func() { close(loadEntered) })
		if blockLoad.Load() {
			<-releaseLoad
		}
	}

	tab := &WorkspaceTab{
		ID:            "meta-tab",
		Scope:         "project",
		WorkspaceRoot: repoRoot,
		Label:         "custom/vision-pro",
		model:         "custom/vision-pro",
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	installNoopRuntimeEvents(nil, tab.sink) // sink only — keep the capture on app.runtimeEvents
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		release()
		app.shutdown(context.Background())
	})

	// The request path never loads config: the first MetaForTab returns empty
	// expensive fields even while the background refresh is parked inside the
	// config-load hook.
	first := app.MetaForTab(tab.ID)
	if first.GitBranch != "" || first.ImageInputEnabled {
		t.Fatalf("first MetaForTab = branch %q image %v, want empty cached values", first.GitBranch, first.ImageInputEnabled)
	}
	select {
	case <-loadEntered:
	case <-time.After(15 * time.Second):
		t.Fatal("background meta refresh never reached the config load")
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("config loads = %d, want exactly 1 (deduped background refresh)", got)
	}
	release()

	var refreshed TabMetaRefreshEvent
	select {
	case refreshed = <-metaEvents:
	case <-time.After(15 * time.Second):
		t.Fatal("no tab:meta event after the background refresh")
	}
	if refreshed.TabID != tab.ID {
		t.Fatalf("tab:meta tab = %q, want %q", refreshed.TabID, tab.ID)
	}
	if refreshed.Meta.GitBranch != "feature/meta-cache" {
		t.Fatalf("refreshed branch = %q, want feature/meta-cache", refreshed.Meta.GitBranch)
	}
	if !refreshed.Meta.ImageInputEnabled {
		t.Fatal("refreshed meta should enable image input for custom/vision-pro")
	}

	// A fresh cache serves subsequent calls without another config load.
	second := app.MetaForTab(tab.ID)
	if second.GitBranch != "feature/meta-cache" || !second.ImageInputEnabled {
		t.Fatalf("cached MetaForTab = branch %q image %v", second.GitBranch, second.ImageInputEnabled)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("config loads after cache hit = %d, want 1", got)
	}

	// A root change invalidates conservatively: the other root's cached values
	// must not be served, and the background refresh repopulates for the new
	// root (not a git repo here, so the branch stays empty).
	app.mu.Lock()
	tab.WorkspaceRoot = plainRoot
	app.mu.Unlock()
	third := app.MetaForTab(tab.ID)
	if third.GitBranch != "" || third.ImageInputEnabled {
		t.Fatalf("MetaForTab after root change = branch %q image %v, want empty", third.GitBranch, third.ImageInputEnabled)
	}
	select {
	case ev := <-metaEvents:
		if ev.Meta.GitBranch != "" {
			t.Fatalf("refreshed branch for non-repo root = %q, want empty", ev.Meta.GitBranch)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("no tab:meta event after the root change")
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("config loads after root change = %d, want 2", got)
	}
}
