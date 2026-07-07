package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }

func (stubProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	close(ch)
	return ch, nil
}

func controllerWithContent(t *testing.T, path string) *control.Controller {
	t.Helper()
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "remember this turn"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "acknowledged"})
	ag := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	return control.New(control.Options{Executor: ag, SessionDir: filepath.Dir(path), SessionPath: path, Sink: event.Discard})
}

func waitForFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session file %q never contained %q", path, want)
}

func waitForAutosaveIdle(t *testing.T, tab *WorkspaceTab) {
	t.Helper()
	waitForAutosaveIdleWithin(t, tab, 2*time.Second)
}

func waitForAutosaveIdleWithin(t *testing.T, tab *WorkspaceTab, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tab.saveMu.Lock()
		idle := !tab.saving && !tab.saveAgain
		tab.saveMu.Unlock()
		if idle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("autosave loop did not become idle")
}

func appWithTab(t *testing.T, path string) (*App, *WorkspaceTab) {
	t.Helper()
	ctrl := controllerWithContent(t, path)
	tab := &WorkspaceTab{
		ID:            "test_tab",
		Ctrl:          ctrl,
		Scope:         "global",
		WorkspaceRoot: "",
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: nil}
	a := &App{
		tabs:        map[string]*WorkspaceTab{"test_tab": tab},
		activeTabID: "test_tab",
	}
	tab.sink.app = a
	return a, tab
}

// TestTurnDonePersistsSession proves a completed turn is written to disk without
// any explicit Snapshot call — the desktop autosave the data-loss fix adds. A
// nil sink ctx (no webview) must not disable persistence.
func TestTurnDonePersistsSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	_, tab := appWithTab(t, path)

	tab.sink.Emit(event.Event{Kind: event.TurnDone})

	waitForFile(t, path, "remember this turn")
	waitForAutosaveIdle(t, tab)
}

// TestNonTurnDoneDoesNotPersist confirms only TurnDone triggers a save, so the
// per-token event storm doesn't thrash the disk.
func TestNonTurnDoneDoesNotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	a, tab := appWithTab(t, path)
	_ = a

	tab.sink.Emit(event.Event{Kind: event.Text, Text: "tok"})

	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a non-TurnDone event wrote the session file (err=%v)", err)
	}
}

// TestScheduleSnapshotCoalesces hammers the scheduler concurrently to prove the
// single-flight loop neither panics nor drops the final write.
func TestScheduleSnapshotCoalesces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	a, tab := appWithTab(t, path)
	_ = a

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tab.sink.Emit(event.Event{Kind: event.TurnDone})
		}()
	}
	wg.Wait()

	waitForFile(t, path, "acknowledged")
	waitForAutosaveIdle(t, tab)
}

func TestAutosaveFailureRetriesAndRecoversOnNextTurnDone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, tab := appWithTab(t, path)
	_ = a

	tab.sink.Emit(event.Event{Kind: event.TurnDone})
	waitForAutosaveIdleWithin(t, tab, 5*time.Second)

	tab.saveMu.Lock()
	failures := tab.saveFailures
	tab.saveMu.Unlock()
	if failures == 0 {
		t.Fatal("autosave failure should be recorded and retried")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("blocked session path should still be the directory, info=%v err=%v", info, err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove blocked dir: %v", err)
	}
	tab.sink.Emit(event.Event{Kind: event.TurnDone})
	waitForFile(t, path, "remember this turn")
	waitForAutosaveIdle(t, tab)

	tab.saveMu.Lock()
	failures = tab.saveFailures
	tab.saveMu.Unlock()
	if failures != 0 {
		t.Fatalf("autosave failures after recovery = %d, want 0", failures)
	}
}

func TestDesktopSnapshotConflictRecoveryUpdatesTabAndProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)

	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	originalPath := filepath.Join(dir, "session.jsonl")
	originalTopic := "topic_original"
	if err := setTopicTitle("", originalTopic, "Original"); err != nil {
		t.Fatalf("set original topic title: %v", err)
	}
	current := agent.NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(originalPath); err != nil {
		t.Fatalf("Save current: %v", err)
	}
	if err := agent.SaveBranchMeta(originalPath, agent.BranchMeta{
		Scope:         "global",
		TopicID:       originalTopic,
		TopicTitle:    "Original",
		Preview:       "first",
		Turns:         2,
		SchemaVersion: agent.BranchMetaCountsVersion,
	}); err != nil {
		t.Fatalf("SaveBranchMeta original: %v", err)
	}

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	staleExec := agent.New(stubProvider{}, tool.NewRegistry(), staleSess, agent.Options{}, event.Discard)
	app := &App{
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{},
		activeTabID:      "recovery_tab",
	}
	tab := &WorkspaceTab{
		ID:            "recovery_tab",
		Scope:         "global",
		WorkspaceRoot: root,
		TopicID:       originalTopic,
		TopicTitle:    "Original",
		SessionPath:   originalPath,
		Ready:         true,
		model:         "test-model",
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	tab.Ctrl = control.New(control.Options{
		Executor:            staleExec,
		SessionDir:          dir,
		SessionPath:         originalPath,
		Label:               "test",
		Sink:                tab.sink,
		SessionRecoveryMeta: app.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:  app.handleTabSessionRecovered(tab),
	})
	app.tabs[tab.ID] = tab

	if err := tab.Ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	recoveryPath := tab.Ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == originalPath {
		t.Fatalf("recovery path = %q, want distinct path", recoveryPath)
	}
	if tab.SessionPath != recoveryPath {
		t.Fatalf("tab session path = %q, want recovery path %q", tab.SessionPath, recoveryPath)
	}
	if tab.TopicID != originalTopic {
		t.Fatalf("tab topic ID = %q, want original topic %q", tab.TopicID, originalTopic)
	}
	meta, ok, err := agent.LoadBranchMeta(recoveryPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta recovery ok=%v err=%v", ok, err)
	}
	if !meta.Recovered || meta.TopicID != tab.TopicID || meta.TopicTitle != tab.TopicTitle {
		t.Fatalf("recovery meta = %+v, tab topic=%q/%q", meta, tab.TopicID, tab.TopicTitle)
	}
	tabMeta := app.tabMeta(tab, true)
	if !tabMeta.Recovered || tabMeta.RecoveryDigest != meta.RecoveryDigest || tabMeta.RecoveryParentID != string(meta.ParentID) {
		t.Fatalf("tab recovery meta = %+v, want digest %q parent %q", tabMeta, meta.RecoveryDigest, meta.ParentID)
	}
	nodes := app.ListProjectTree()
	foundOriginal := false
	var walk func([]ProjectNode)
	walk = func(list []ProjectNode) {
		for _, node := range list {
			if node.Recovered {
				t.Fatalf("project tree should hide recovery metadata, got node %+v", node)
			}
			if node.TopicID == originalTopic {
				foundOriginal = true
			}
			walk(node.Children)
		}
	}
	walk(nodes)
	if !foundOriginal {
		t.Fatalf("project tree did not include original topic %q: %#v", originalTopic, nodes)
	}
}

func TestDesktopSnapshotConflictRecoveryRequiresRecoveryLease(t *testing.T) {
	isolateDesktopUserDirs(t)

	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	originalPath := filepath.Join(dir, "session.jsonl")
	current := agent.NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(originalPath); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	recovery, err := staleSess.SaveRecoveryBranch(agent.RecoveryBranchOptions{
		OriginalPath: originalPath,
		BranchMeta: agent.BranchMeta{
			Name:       agent.RecoveryBranchDefaultName,
			Scope:      "global",
			TopicID:    "topic_recovery",
			TopicTitle: "Recovery",
		},
	})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	lease, err := agent.TryAcquireSessionLease(recovery.Path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease recovery: %v", err)
	}
	defer lease.Release()

	staleExec := agent.New(stubProvider{}, tool.NewRegistry(), staleSess, agent.Options{}, event.Discard)
	runtimeEvents := make(chan runtimeEventEnvelope, 4)
	app := &App{
		ctx:              context.Background(),
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{},
		activeTabID:      "recovery_tab",
	}
	app.runtimeEvents.emit = func(ctx context.Context, name string, payload ...interface{}) {
		runtimeEvents <- runtimeEventEnvelope{
			ctx:     ctx,
			name:    name,
			payload: append([]interface{}(nil), payload...),
		}
	}
	tab := &WorkspaceTab{
		ID:            "recovery_tab",
		Scope:         "global",
		WorkspaceRoot: root,
		TopicID:       "topic_original",
		TopicTitle:    "Original",
		SessionPath:   originalPath,
		Ready:         true,
		model:         "test-model",
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	tab.Ctrl = control.New(control.Options{
		Executor:            staleExec,
		SessionDir:          dir,
		SessionPath:         originalPath,
		Label:               "test",
		Sink:                tab.sink,
		SessionRecoveryMeta: app.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:  app.handleTabSessionRecovered(tab),
	})
	app.tabs[tab.ID] = tab

	err = tab.Ctrl.Snapshot()
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("Snapshot err = %v, want ErrSessionLeaseHeld", err)
	}
	if got := tab.Ctrl.SessionPath(); got != originalPath {
		t.Fatalf("controller session path = %q, want original %q", got, originalPath)
	}
	if tab.SessionPath != originalPath {
		t.Fatalf("tab session path = %q, want original %q", tab.SessionPath, originalPath)
	}
	if tab.TopicID != "topic_original" {
		t.Fatalf("tab topic ID = %q, want original topic", tab.TopicID)
	}

	deadline := time.After(time.Second)
	for {
		select {
		case emitted := <-runtimeEvents:
			if emitted.name != "session:recovery-failed" {
				continue
			}
			if len(emitted.payload) != 1 {
				t.Fatalf("session:recovery-failed payload count = %d, want 1", len(emitted.payload))
			}
			failed, ok := emitted.payload[0].(sessionRecoveryFailedEvent)
			if !ok {
				t.Fatalf("session:recovery-failed payload type = %T, want sessionRecoveryFailedEvent", emitted.payload[0])
			}
			if failed.Reason != "lease_held" {
				t.Fatalf("session:recovery-failed reason = %q, want lease_held", failed.Reason)
			}
			return
		case <-deadline:
			t.Fatal("session:recovery-failed event was not emitted")
		}
	}
}

func TestSetActiveTabBlocksWhenCurrentSessionCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, _ := appWithTab(t, path)
	a.tabs["target_tab"] = &WorkspaceTab{
		ID:          "target_tab",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	a.tabOrder = []string{"test_tab", "target_tab"}

	err := a.SetActiveTab("target_tab")
	if err == nil || !strings.Contains(err.Error(), "save current session before switching tabs") {
		t.Fatalf("SetActiveTab error = %v, want persistence failure", err)
	}
	if a.activeTabID != "test_tab" {
		t.Fatalf("active tab = %q, want original tab after failed save", a.activeTabID)
	}
}

func TestRebindSessionBlocksWhenCurrentSessionCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, tab := appWithTab(t, path)
	target := filepath.Join(t.TempDir(), "target.jsonl")
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "target prompt"})
	if err := sess.Save(target); err != nil {
		t.Fatalf("save target: %v", err)
	}
	loaded, err := agent.LoadSession(target)
	if err != nil {
		t.Fatalf("load target: %v", err)
	}

	err = a.rebindTabToLoadedSessionPath(tab, target, loaded)
	if err == nil || !strings.Contains(err.Error(), "save current session before switching sessions") {
		t.Fatalf("rebind error = %v, want persistence failure", err)
	}
	if tab.Ctrl == nil || tab.Ctrl.SessionPath() != path {
		t.Fatalf("tab controller/path changed after failed save: ctrl=%v path=%q", tab.Ctrl, tab.currentSessionPath())
	}
}

// TestCloseTabNoResurrectionFromAutosave is the regression test for #4384.
// It proves that after CloseTab returns, the per-turn autosave goroutine can no
// longer write the session file — even when it is in flight at the moment the
// tab is closed. Pre-fix, the loop held a raw *WorkspaceTab pointer and a
// captured session path, so its Snapshot() call landed after DeleteSession
// trashed the file, "resurrecting" it.
func TestCloseTabNoResurrectionFromAutosave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	doomed, doomedTab := appWithTab(t, path)
	// CloseTab needs >1 tab and mutates activeTabID, so add a survivor tab.
	survivor := &WorkspaceTab{
		ID:          "survivor_tab",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	survivor.sink = &tabEventSink{tabID: survivor.ID, app: doomed}
	doomed.tabs["survivor_tab"] = survivor
	doomed.activeTabID = "test_tab"

	// Write the session file once via the autosave loop, then wait for idle so
	// the next TurnDone reliably kicks off a fresh loop.
	doomedTab.sink.Emit(event.Event{Kind: event.TurnDone})
	waitForFile(t, path, "acknowledged")
	waitForAutosaveIdle(t, doomedTab)

	// Kick the autosave loop and close the tab in close succession. The loop
	// will be in flight when CloseTab runs — exactly the #4384 window.
	doomedTab.sink.Emit(event.Event{Kind: event.TurnDone})
	if err := doomed.CloseTab("test_tab"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}

	// CloseTab must have returned only after the autosave loop finished. Remove
	// the file the way DeleteSession would (move to trash is just a remove here
	// since we only care that nothing rewrites the original path).
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove session file: %v", err)
	}

	// Give any would-be resurrection a chance to strike. If the autosave loop
	// were still alive (the bug), the file reappears here.
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session file resurrected after CloseTab + delete (stat err=%v) — autosave loop not drained", err)
	}

	// And the controller's session path must be cleared so no future Snapshot
	// can write either.
	if got := doomedTab.Ctrl.SessionPath(); got != "" {
		t.Fatalf("controller session path = %q after CloseTab, want empty so snapshots no-op", got)
	}
}

func TestCloseTabBlocksWhenSessionCannotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.jsonl")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir blocked path: %v", err)
	}
	a, tab := appWithTab(t, path)
	survivor := &WorkspaceTab{
		ID:          "survivor_tab",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	survivor.sink = &tabEventSink{tabID: survivor.ID, app: a}
	a.tabs[survivor.ID] = survivor
	a.tabOrder = []string{tab.ID, survivor.ID}

	err := a.CloseTab(tab.ID)
	if err == nil || !strings.Contains(err.Error(), "save current session before closing tab") {
		t.Fatalf("CloseTab error = %v, want persistence failure", err)
	}
	if _, ok := a.tabs[tab.ID]; !ok {
		t.Fatal("tab was removed even though its session could not be saved")
	}
	if tab.Ctrl == nil || tab.Ctrl.SessionPath() != path {
		t.Fatalf("tab controller/path changed after failed close: ctrl=%v path=%q", tab.Ctrl, tab.currentSessionPath())
	}
}

// TestCloseTabSurvivorKeepsAutosave ensures the survivor tab is untouched: the
// closing/drain logic is per-tab and must not leak to other tabs.
func TestCloseTabSurvivorKeepsAutosave(t *testing.T) {
	doomedPath := filepath.Join(t.TempDir(), "doomed.jsonl")
	survivorPath := filepath.Join(t.TempDir(), "survivor.jsonl")

	a, _ := appWithTab(t, doomedPath)
	survivorCtrl := controllerWithContent(t, survivorPath)
	survivor := &WorkspaceTab{
		ID:          "survivor_tab",
		Ctrl:        survivorCtrl,
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	survivor.sink = &tabEventSink{tabID: survivor.ID, app: a}
	a.tabs["survivor_tab"] = survivor
	a.activeTabID = "test_tab"

	survivor.sink.Emit(event.Event{Kind: event.TurnDone})
	waitForFile(t, survivorPath, "acknowledged")
	waitForAutosaveIdle(t, survivor)

	if err := a.CloseTab("test_tab"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}

	if got := survivor.Ctrl.SessionPath(); got != survivorPath {
		t.Fatalf("survivor session path = %q, want %q", got, survivorPath)
	}
	if survivor.closing {
		t.Fatal("survivor tab was marked closing — closing flag leaked across tabs")
	}
}

func TestDeleteSessionClearsRemovedRuntimeSessionPath(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "delete-open.jsonl")
	ctrl := controllerWithContent(t, path)
	tab := &WorkspaceTab{
		ID:          "delete_open",
		Scope:       "global",
		Ready:       true,
		Ctrl:        ctrl,
		disabledMCP: map[string]ServerView{},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"delete_open": tab},
		activeTabID: "delete_open",
	}
	if err := ctrl.Snapshot(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if err := app.DeleteSession(path); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if got := ctrl.SessionPath(); got != "" {
		t.Fatalf("removed controller session path = %q, want empty before trash move can race Windows file locks", got)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "delete-open.jsonl", "delete-open.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("session should be in trash: %v", err)
	}
}

func TestTrashTopicClearsRemovedRuntimeSessionPath(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_clear_removed_runtime"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Clear removed runtime"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "trash-open-topic.jsonl")
	ctrl := controllerWithContent(t, path)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       topicID,
		TopicTitle:    "Clear removed runtime",
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	tab := &WorkspaceTab{
		ID:            "trash_open",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       topicID,
		TopicTitle:    "Clear removed runtime",
		Ready:         true,
		Ctrl:          ctrl,
		disabledMCP:   map[string]ServerView{},
	}
	survivor := &WorkspaceTab{
		ID:          "survivor",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"trash_open": tab, "survivor": survivor},
		tabOrder:    []string{"trash_open", "survivor"},
		activeTabID: "trash_open",
	}

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic: %v", err)
	}

	if got := ctrl.SessionPath(); got != "" {
		t.Fatalf("removed topic controller session path = %q, want empty before trash move can race Windows file locks", got)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "trash-open-topic.jsonl", "trash-open-topic.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("topic session should be in trash: %v", err)
	}
}
