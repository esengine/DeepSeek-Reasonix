package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
)

// TestSessionLeaseHelpersConcurrentAccess hammers the sessionLeaseMu helpers
// (ensure/take/adopt/release/key) from concurrent goroutines. Run with -race:
// any residual raw access to tab.sessionLease shows up as a data race, and the
// final acquire asserts no lease leaked through an interleaving.
func TestSessionLeaseHelpersConcurrentAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "lease-helper-hammer.jsonl")
	key := sessionRuntimeKey(path)
	tabA := &WorkspaceTab{ID: "a"}
	tabB := &WorkspaceTab{ID: "b"}

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = tabA.ensureSessionLease(path)
			_ = tabA.sessionLeaseRuntimeKey()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			// The applyRuntimeTab transfer shape: move A's lease to B and back.
			tabB.adoptSessionLease(tabA.takeSessionLease())
			tabA.adoptSessionLease(tabB.takeSessionLease())
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tabA.releaseSessionLease()
		}
	}()
	wg.Wait()

	tabA.releaseSessionLease()
	tabB.releaseSessionLease()
	lease, err := agent.TryAcquireSessionLease(key)
	if err != nil {
		t.Fatalf("lease leaked through concurrent helper interleavings: %v", err)
	}
	lease.Release()
}

// TestDetachRuntimeForReplacementTransfersLease asserts the detach clone takes
// lease ownership through the locked helpers and the visible tab keeps none.
func TestDetachRuntimeForReplacementTransfersLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "detach-transfer.jsonl")
	key := sessionRuntimeKey(path)
	tab := &WorkspaceTab{ID: "tab", Scope: "global", SessionPath: path}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	if err := tab.ensureSessionLease(path); err != nil {
		t.Fatalf("ensureSessionLease: %v", err)
	}
	t.Cleanup(func() {
		tab.releaseSessionLease()
		for _, d := range app.detachedSessions {
			d.releaseSessionLease()
		}
	})

	if !app.detachRuntimeForReplacement(tab) {
		t.Fatal("detachRuntimeForReplacement failed for a live tab")
	}
	detached := app.detachedSessions[key]
	if detached == nil {
		t.Fatal("detached runtime was not registered")
	}
	if got := detached.sessionLeaseRuntimeKey(); got != key {
		t.Fatalf("detached clone lease key = %q, want %q", got, key)
	}
	if got := tab.sessionLeaseRuntimeKey(); got != "" {
		t.Fatalf("visible tab still holds lease key %q after detach", got)
	}
}

// TestDetachRuntimeForReplacementSkipsRemovedTab: a tab that DeleteSession /
// CloseTab already unlinked must not be re-published into detachedSessions
// (the "session resurrects" class, #4384).
func TestDetachRuntimeForReplacementSkipsRemovedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "detach-removed.jsonl")

	removed := &WorkspaceTab{ID: "removed", SessionPath: path, removed: true}
	app := &App{tabs: map[string]*WorkspaceTab{"removed": removed}}
	if app.detachRuntimeForReplacement(removed) {
		t.Fatal("removed tab was detached into the background registry")
	}
	if len(app.detachedSessions) != 0 {
		t.Fatal("removed tab left an entry in detachedSessions")
	}

	orphan := &WorkspaceTab{ID: "orphan", SessionPath: path}
	if app.detachRuntimeForReplacement(orphan) {
		t.Fatal("tab absent from a.tabs was detached into the background registry")
	}
	if len(app.detachedSessions) != 0 {
		t.Fatal("orphan tab left an entry in detachedSessions")
	}
}

// TestTabEventSinkEmitConcurrentRebind: Emit keeps running on the controller
// goroutine while detach/reattach rebinds the sink's tab routing. Run with
// -race; before setBinding existed the tabID write raced every Emit.
func TestTabEventSinkEmitConcurrentRebind(t *testing.T) {
	sink := &tabEventSink{tabID: "before"}
	const iterations = 500
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			sink.Emit(event.Event{Kind: event.Notice, Text: "hammer"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			sink.setBinding(fmt.Sprintf("tab-%d", i%2), nil)
			sink.clearContext()
		}
	}()
	wg.Wait()
	if tabID, _ := sink.binding(); tabID == "" {
		t.Fatal("sink lost its tab binding")
	}
}

// TestTabBuildSupersededByRebindGeneration: a session rebind bumps
// buildGeneration to strand any in-flight async build, so its swap (and every
// mid-build field write) is rejected; synchronous rebuilds pass generation 0
// and rely on runtimeRebuildMu instead.
func TestTabBuildSupersededByRebindGeneration(t *testing.T) {
	tab := &WorkspaceTab{ID: "tab"}
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	app.mu.Lock()
	tab.buildGeneration = 3
	app.mu.Unlock()
	if app.tabBuildSuperseded(tab, 3) {
		t.Fatal("build with the current generation must not be superseded")
	}
	app.mu.Lock()
	tab.buildGeneration++ // the rebind-side invalidation
	app.mu.Unlock()
	if !app.tabBuildSuperseded(tab, 3) {
		t.Fatal("stale-generation build must be superseded after a rebind bump")
	}
	if app.tabBuildSuperseded(tab, 0) {
		t.Fatal("synchronous rebuild (generation 0) must not be superseded by generation bumps")
	}
	app.mu.Lock()
	tab.removed = true
	app.mu.Unlock()
	if !app.tabBuildSuperseded(tab, 0) {
		t.Fatal("removed tab must supersede every build, including synchronous ones")
	}
}

// TestReleaseSessionLeaseForKeyOnlyMatchesOwnKey: superseded builds may only
// release the lease bound to their own path; a mismatched key (the rebind's
// replacement session) must be left untouched.
func TestReleaseSessionLeaseForKeyOnlyMatchesOwnKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	pathA := filepath.Join(dir, "lease-key-a.jsonl")
	pathB := filepath.Join(dir, "lease-key-b.jsonl")
	tab := &WorkspaceTab{ID: "tab"}
	t.Cleanup(tab.releaseSessionLease)
	if err := tab.ensureSessionLease(pathB); err != nil {
		t.Fatalf("ensureSessionLease: %v", err)
	}

	tab.releaseSessionLeaseForKey(sessionRuntimeKey(pathA))
	if _, err := agent.TryAcquireSessionLease(sessionRuntimeKey(pathB)); err == nil {
		t.Fatal("mismatched key released the tab's live lease")
	}

	tab.releaseSessionLeaseForKey(sessionRuntimeKey(pathB))
	lease, err := agent.TryAcquireSessionLease(sessionRuntimeKey(pathB))
	if err != nil {
		t.Fatalf("matching key did not release the lease: %v", err)
	}
	lease.Release()
}

// TestAbandonSupersededBuildPreservesNewBuildOwnership reproduces the rebind
// interleaving from the #5968 review: a stale async build cleans up after a
// rebind's replacement build already published its own SharedHostKey and
// session lease on the same live tab. The stale build must drop only its own
// host reference and lease, never the tab's.
func TestAbandonSupersededBuildPreservesNewBuildOwnership(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	oldPath := filepath.Join(dir, "superseded-old.jsonl")
	newPath := filepath.Join(dir, "superseded-new.jsonl")
	newKey := sessionRuntimeKey(newPath)

	app := &App{tabs: map[string]*WorkspaceTab{}}
	// The stale build and the replacement build each hold one reference on
	// the same workspace-root host (the common same-root rebind).
	app.acquireSharedHost("root")
	app.acquireSharedHost("root")

	tab := &WorkspaceTab{ID: "tab", SharedHostKey: "root"}
	app.tabs["tab"] = tab
	t.Cleanup(tab.releaseSessionLease)
	if err := tab.ensureSessionLease(newPath); err != nil {
		t.Fatalf("ensureSessionLease(new): %v", err)
	}

	// Stale build abandons with ITS key material: the old lease path and the
	// root key it acquired.
	app.abandonSupersededBuild(tab, nil, "root", sessionRuntimeKey(oldPath))

	app.mu.RLock()
	hostKey := tab.SharedHostKey
	app.mu.RUnlock()
	if hostKey != "root" {
		t.Fatalf("stale build cleared the tab's SharedHostKey: %q", hostKey)
	}
	app.sharedHostsMu.Lock()
	entry := app.sharedHosts["root"]
	refs := 0
	if entry != nil {
		refs = entry.refs
	}
	app.sharedHostsMu.Unlock()
	if refs != 1 {
		t.Fatalf("shared host refs = %d after stale-build cleanup, want 1 (the live build's reference)", refs)
	}
	if got := tab.sessionLeaseRuntimeKey(); got != newKey {
		t.Fatalf("stale build released the replacement build's lease: key = %q, want %q", got, newKey)
	}

	// Removal shape: the abandoned build's own key still on the tab is the
	// last reference and must be released.
	app.abandonSupersededBuild(tab, nil, "", newKey)
	lease, err := agent.TryAcquireSessionLease(newKey)
	if err != nil {
		t.Fatalf("matching-key abandon did not release the lease: %v", err)
	}
	lease.Release()
}

// TestRebindInvalidatesInFlightAsyncBuildBeforeSnapshot reproduces the #5968
// review scenario: an async startTabControllerBuild is stalled right after
// binding its session lease (holding its controller, lease, and shared-host
// reference), a rebind to a different session starts meanwhile, and the
// stalled build then tries to finish. The rebind must bump the generation
// BEFORE snapshotting tab.Ctrl, so the stale build can only fall into its
// superseded branches: the rebound controller must survive un-overwritten,
// the stale build's lease and host reference must be released, and the
// replacement build's ownership must stay intact.
func TestRebindInvalidatesInFlightAsyncBuildBeforeSnapshot(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	oldPath := filepath.Join(dir, "rebind-race-old.jsonl")
	newPath := filepath.Join(dir, "rebind-race-new.jsonl")
	writeHistoryTestSession(t, oldPath, "old prompt")
	writeHistoryTestSession(t, newPath, "new prompt")
	oldKey := sessionRuntimeKey(oldPath)
	newKey := sessionRuntimeKey(newPath)

	app := NewApp()
	app.readyHook = func() {}
	tab := &WorkspaceTab{
		ID:            "tab",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   oldPath,
		sink:          &tabEventSink{tabID: "tab", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		app.mu.RLock()
		ctrl := tab.Ctrl
		app.mu.RUnlock()
		if ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	// Stall the async build inside its lease bind: at that point it already
	// holds its controller, its lease, and its shared-host reference.
	stalled := make(chan struct{})
	releaseHook := make(chan struct{})
	var once sync.Once
	sessionLeaseAcquireHookForTest = func() {
		once.Do(func() {
			close(stalled)
			<-releaseHook
		})
	}
	t.Cleanup(func() { sessionLeaseAcquireHookForTest = nil })
	t.Cleanup(func() {
		select {
		case <-releaseHook:
		default:
			close(releaseHook)
		}
	})

	// startTabControllerBuild only backgrounds the build when a Wails context
	// exists; expand its goroutine branch by hand so a.ctx can stay nil (the
	// nil-ctx emit guards are what every other build test relies on too).
	buildCtx, cancel := context.WithCancel(context.Background())
	app.mu.Lock()
	tab.buildGeneration++
	generation := tab.buildGeneration
	tab.buildCancel = cancel
	app.mu.Unlock()
	buildDone := make(chan struct{})
	go func() {
		defer close(buildDone)
		app.buildTabControllerWithContext(tab, loadedTabSession{}, buildCtx, generation, cancel)
	}()
	select {
	case <-stalled:
	case <-time.After(15 * time.Second):
		t.Fatal("async build did not reach the lease bind")
	}

	loaded, err := agent.LoadSession(newPath)
	if err != nil {
		t.Fatalf("LoadSession(new): %v", err)
	}
	rebindErr := make(chan error, 1)
	go func() {
		rebindErr <- app.rebindTabToLoadedSessionPath(tab, newPath, loaded)
	}()

	// Wait until the rebind's validation section has invalidated the async
	// build (startTabControllerBuild set generation 1; the rebind bumps to 2)
	// while the async build is still stalled pre-swap.
	deadline := time.Now().Add(15 * time.Second)
	for {
		app.mu.RLock()
		generation := tab.buildGeneration
		app.mu.RUnlock()
		if generation >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rebind did not bump the build generation")
		}
		time.Sleep(time.Millisecond)
	}

	close(releaseHook)
	select {
	case err := <-rebindErr:
		if err != nil {
			t.Fatalf("rebindTabToLoadedSessionPath: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("rebind did not finish after releasing the stalled build")
	}
	select {
	case <-buildDone:
	case <-time.After(30 * time.Second):
		t.Fatal("stale async build did not finish its superseded cleanup")
	}

	app.mu.RLock()
	builtCtrl := tab.Ctrl
	sessionPath := tab.SessionPath
	app.mu.RUnlock()
	if builtCtrl == nil {
		t.Fatal("rebind left the tab without a controller")
	}
	if got := builtCtrl.SessionPath(); got != newPath {
		t.Fatalf("stale async build overwrote the rebound controller: session path = %q, want %q", got, newPath)
	}
	if sessionPath != newPath {
		t.Fatalf("tab.SessionPath = %q, want %q", sessionPath, newPath)
	}
	if got := tab.sessionLeaseRuntimeKey(); got != newKey {
		t.Fatalf("tab lease key = %q, want the rebound session %q", got, newKey)
	}
	// The stale build's lease must be gone (released by its superseded
	// cleanup or replaced by the rebound build's adopt) — the old session
	// must be acquirable again.
	lease, err := agent.TryAcquireSessionLease(oldKey)
	if err != nil {
		t.Fatalf("stale build leaked its session lease: %v", err)
	}
	lease.Release()
	// Exactly one shared-host reference may remain: the rebound build's. The
	// stale build must have released its own.
	app.sharedHostsMu.Lock()
	refs := 0
	for _, entry := range app.sharedHosts {
		refs += entry.refs
	}
	app.sharedHostsMu.Unlock()
	if refs != 1 {
		t.Fatalf("shared host refs = %d after stale-build cleanup, want 1 (the rebound build's)", refs)
	}
}

// TestMetaForTabConcurrentWithBuildSwap polls MetaForTab (the frontend's boot
// probe) while a fake build goroutine flips Ready/Label/StartupErr/model under
// a.mu — the write pattern of buildTabControllerWithContext. Run with -race.
func TestMetaForTabConcurrentWithBuildSwap(t *testing.T) {
	isolateDesktopUserDirs(t)
	tab := &WorkspaceTab{ID: "tab", Scope: "project", WorkspaceRoot: t.TempDir()}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"tab": tab},
		tabOrder:    []string{"tab"},
		activeTabID: "tab",
	}

	const iterations = 100
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			app.mu.Lock()
			tab.Ready = !tab.Ready
			tab.Label = fmt.Sprintf("model-%d", i)
			tab.StartupErr = ""
			tab.model = fmt.Sprintf("provider/m%d", i)
			tab.goal = fmt.Sprintf("goal-%d", i)
			tab.tokenMode = "full"
			app.mu.Unlock()
		}
	}()
	for i := 0; i < iterations; i++ {
		meta := app.MetaForTab("tab")
		if meta.EventChannel == "" {
			t.Fatal("MetaForTab returned zero meta for a live tab")
		}
	}
	<-done
}
