package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func testAppWithOrderedTabs(t *testing.T, active string, ids ...string) *App {
	t.Helper()
	isolateDesktopUserDirs(t)
	tabs := make(map[string]*WorkspaceTab, len(ids))
	for _, id := range ids {
		tabs[id] = &WorkspaceTab{
			ID:          id,
			Scope:       "global",
			TopicID:     "topic_" + id,
			TopicTitle:  id,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		}
	}
	return &App{tabs: tabs, tabOrder: append([]string(nil), ids...), activeTabID: active}
}

func installNoopRuntimeEvents(app *App, sinks ...*tabEventSink) {
	emit := func(context.Context, string, ...interface{}) {}
	if app != nil {
		app.runtimeEvents.emit = emit
	}
	for _, sink := range sinks {
		if sink != nil {
			sink.runtimeEvents.emit = emit
		}
	}
}

func tabIDs(tabs []TabMeta) []string {
	ids := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		ids = append(ids, tab.ID)
	}
	return ids
}

func assertTabIDs(t *testing.T, got []TabMeta, want ...string) {
	t.Helper()
	gotIDs := tabIDs(got)
	if len(gotIDs) != len(want) {
		t.Fatalf("tab ids = %v, want %v", gotIDs, want)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("tab ids = %v, want %v", gotIDs, want)
		}
	}
}

type resetCountingSession struct {
	control.SessionAPI
	resets int
}

func (s *resetCountingSession) ResetPlannerSession() {
	s.resets++
}

type snapshotObservingSession struct {
	control.SessionAPI
	onSnapshot func()
}

func (s *snapshotObservingSession) Snapshot() error {
	if s.onSnapshot != nil {
		s.onSnapshot()
	}
	return nil
}

func expectAppMutexAvailableDuringSnapshot(t *testing.T, app *App, checks chan<- struct{}) func() {
	t.Helper()
	return func() {
		acquired := make(chan struct{})
		go func() {
			app.mu.Lock()
			app.mu.Unlock() //nolint:staticcheck // probe: lock must be immediately acquirable
			close(acquired)
		}()
		select {
		case <-acquired:
		case <-time.After(500 * time.Millisecond):
			t.Error("Snapshot ran while holding app mutex")
		}
		if checks == nil {
			return
		}
		select {
		case checks <- struct{}{}:
		default:
		}
	}
}

func TestListTabsKeepsExplicitOrderWhenActiveChanges(t *testing.T) {
	app := testAppWithOrderedTabs(t, "b", "a", "b", "c")

	assertTabIDs(t, app.ListTabs(), "a", "b", "c")
	if err := app.SetActiveTab("c"); err != nil {
		t.Fatalf("SetActiveTab: %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "a", "b", "c")
	if got := app.activeTabID; got != "c" {
		t.Fatalf("active tab = %q, want c", got)
	}
}

func TestSetActiveTabDoesNotResetPlannerSession(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrlA := &resetCountingSession{SessionAPI: control.New(control.Options{Label: "a"})}
	ctrlB := &resetCountingSession{SessionAPI: control.New(control.Options{Label: "b"})}
	defer ctrlA.Close()
	defer ctrlB.Close()
	app := testAppWithOrderedTabs(t, "a", "a", "b")
	app.tabs["a"].Ctrl = ctrlA
	app.tabs["b"].Ctrl = ctrlB

	if err := app.SetActiveTab("b"); err != nil {
		t.Fatalf("SetActiveTab: %v", err)
	}
	if ctrlA.resets != 0 || ctrlB.resets != 0 {
		t.Fatalf("planner resets on tab activation = active:%d inactive:%d, want 0", ctrlA.resets, ctrlB.resets)
	}
}

func TestSingleSurfaceTabsFileKeepsActiveEntry(t *testing.T) {
	f := desktopTabsFile{
		Tabs: []desktopTabEntry{
			{ID: "a", Scope: "global", TopicID: "topic-a"},
			{ID: "b", Scope: "project", WorkspaceRoot: "/tmp/project", TopicID: "topic-b"},
			{ID: "c", Scope: "global", TopicID: "topic-c"},
		},
		ActiveTab: "b",
	}

	got := singleSurfaceTabsFile(f)
	if len(got.Tabs) != 1 || got.Tabs[0].ID != "b" || got.ActiveTab != "b" {
		t.Fatalf("single-surface tabs = %+v, want only active b", got)
	}
}

func TestSetDesktopLayoutStyleAppliesPolicyAfterWorkspaceAlias(t *testing.T) {
	app := testAppWithOrderedTabs(t, "b", "a", "b", "c")

	if err := app.SetDesktopLayoutStyle("workspace"); err != nil {
		t.Fatalf("SetDesktopLayoutStyle(workspace): %v", err)
	}

	assertTabIDs(t, app.ListTabs(), "b")
	if got := loadTabsFile(); len(got.Tabs) != 1 || got.Tabs[0].ID != "b" || got.ActiveTab != "b" {
		t.Fatalf("persisted tabs after workspace alias = %+v, want only active b", got)
	}
}

func TestKeepOnlyVisibleTabDetachesRunningHiddenTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "running.jsonl")
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: path, Label: "running", Sink: event.Discard})
	running := &WorkspaceTab{
		ID:            "running",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		SessionPath:   path,
		Ctrl:          ctrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: "running"},
		disabledMCP:   map[string]ServerView{},
	}
	target := &WorkspaceTab{ID: "target", Scope: "global", Ready: true, disabledMCP: map[string]ServerView{}}
	app := &App{
		tabs:             map[string]*WorkspaceTab{"running": running, "target": target},
		tabOrder:         []string{"running", "target"},
		activeTabID:      "running",
		detachedSessions: map[string]*WorkspaceTab{},
	}

	ctrl.Submit("block")
	<-runner.started
	if _, err := app.keepOnlyVisibleTab("target"); err != nil {
		t.Fatalf("keepOnlyVisibleTab: %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "target")
	if !ctrl.Running() {
		t.Fatal("single-surface pruning cancelled a running controller")
	}
	if _, ok := app.detachedSessions[sessionRuntimeKey(path)]; !ok {
		t.Fatalf("detached runtime missing for %q", path)
	}
	if got := loadTabsFile(); len(got.Tabs) != 1 || got.Tabs[0].ID != "target" {
		t.Fatalf("persisted tabs after pruning = %+v, want only target", got)
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
	ctrl.Close()
}

func TestKeepOnlyVisibleTabSnapshotsHiddenTabWithoutAppLock(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{
		tabs:        map[string]*WorkspaceTab{},
		tabOrder:    []string{"hidden", "target"},
		activeTabID: "hidden",
	}
	snapshotChecks := make(chan struct{}, 2)
	hiddenCtrl := &snapshotObservingSession{
		SessionAPI: control.New(control.Options{Label: "hidden"}),
		onSnapshot: expectAppMutexAvailableDuringSnapshot(t, app, snapshotChecks),
	}
	hidden := &WorkspaceTab{
		ID:            "hidden",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		TopicID:       "topic-hidden",
		Ctrl:          hiddenCtrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: "hidden"},
		disabledMCP:   map[string]ServerView{},
	}
	target := &WorkspaceTab{
		ID:          "target",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	app.tabs["hidden"] = hidden
	app.tabs["target"] = target

	if _, err := app.keepOnlyVisibleTab("target"); err != nil {
		t.Fatalf("keepOnlyVisibleTab: %v", err)
	}
	select {
	case <-snapshotChecks:
	case <-time.After(time.Second):
		t.Fatal("hidden tab was not snapshotted before pruning")
	}
	assertTabIDs(t, app.ListTabs(), "target")
}

func TestCloseTabSnapshotsWithoutAppLock(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := &App{
		tabs:        map[string]*WorkspaceTab{},
		tabOrder:    []string{"closing", "survivor"},
		activeTabID: "closing",
	}
	snapshotChecks := make(chan struct{}, 1)
	closingCtrl := &snapshotObservingSession{
		SessionAPI: control.New(control.Options{Label: "closing"}),
		onSnapshot: expectAppMutexAvailableDuringSnapshot(t, app, snapshotChecks),
	}
	closing := &WorkspaceTab{
		ID:            "closing",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		TopicID:       "topic-closing",
		Ctrl:          closingCtrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: "closing"},
		disabledMCP:   map[string]ServerView{},
	}
	survivor := &WorkspaceTab{
		ID:          "survivor",
		Scope:       "global",
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}
	app.tabs["closing"] = closing
	app.tabs["survivor"] = survivor

	if err := app.CloseTab("closing"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	select {
	case <-snapshotChecks:
	case <-time.After(time.Second):
		t.Fatal("closing tab was not snapshotted")
	}
	assertTabIDs(t, app.ListTabs(), "survivor")
}

func TestKeepOnlyVisibleTabCancelsBuildingHiddenTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	cancelled := false
	building := &WorkspaceTab{
		ID:          "building",
		Scope:       "global",
		Ready:       false,
		buildCancel: func() { cancelled = true },
		sink:        &tabEventSink{tabID: "building"},
		disabledMCP: map[string]ServerView{},
	}
	target := &WorkspaceTab{ID: "target", Scope: "global", Ready: true, disabledMCP: map[string]ServerView{}}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"building": building, "target": target},
		tabOrder:    []string{"building", "target"},
		activeTabID: "building",
	}

	if _, err := app.keepOnlyVisibleTab("target"); err != nil {
		t.Fatalf("keepOnlyVisibleTab: %v", err)
	}

	assertTabIDs(t, app.ListTabs(), "target")
	if !cancelled {
		t.Fatal("building tab build was not cancelled")
	}
	if !building.removed {
		t.Fatal("building tab was not marked removed")
	}
}

func TestConcurrentActivateTopicSerializesSingleSurfacePruning(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })

	topics := []string{
		"topic-a",
		"topic-b",
		"topic-c",
		"topic-d",
		"topic-e",
		"topic-f",
		"topic-g",
		"topic-h",
	}
	start := make(chan struct{})
	errs := make(chan error, len(topics))
	var wg sync.WaitGroup
	for _, topicID := range topics {
		wg.Add(1)
		go func(topicID string) {
			defer wg.Done()
			<-start
			_, err := app.ActivateTopic("global", "", topicID, "")
			errs <- err
		}(topicID)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("ActivateTopic returned error under concurrent navigation: %v", err)
		}
	}
	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("ListTabs returned %d tabs after single-surface navigation, want 1: %+v", len(tabs), tabs)
	}
	if !tabs[0].Active {
		t.Fatalf("remaining tab is not active: %+v", tabs[0])
	}
}

func TestClearTabBuildCancelKeepsSuccessfulControllerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tab := &WorkspaceTab{ID: "tab", buildGeneration: 1, buildCancel: cancel}
	app := &App{}

	app.clearTabBuildCancel(tab, 1, cancel, true)

	if tab.buildCancel != nil {
		t.Fatal("build cancel was not cleared")
	}
	select {
	case <-ctx.Done():
		t.Fatal("successful tab build context was cancelled")
	default:
	}
}

func TestClearTabBuildCancelCancelsAbandonedBuildContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tab := &WorkspaceTab{ID: "tab", buildGeneration: 1, buildCancel: cancel}
	app := &App{}

	app.clearTabBuildCancel(tab, 1, cancel, false)

	if tab.buildCancel != nil {
		t.Fatal("build cancel was not cleared")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("abandoned tab build context was not cancelled")
	}
}

func TestEnsureSessionLeaseSerializesConcurrentSameTabAcquire(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "same-tab-concurrent-lease.jsonl")
	tab := &WorkspaceTab{ID: "tab"}
	t.Cleanup(tab.releaseSessionLease)

	acquired := make(chan struct{})
	releaseHook := make(chan struct{})
	var once sync.Once
	sessionLeaseAcquireHookForTest = func() {
		once.Do(func() {
			close(acquired)
			<-releaseHook
		})
	}
	t.Cleanup(func() { sessionLeaseAcquireHookForTest = nil })

	firstErr := make(chan error, 1)
	go func() {
		firstErr <- tab.ensureSessionLease(path)
	}()

	select {
	case <-acquired:
	case err := <-firstErr:
		t.Fatalf("first ensureSessionLease returned before hook: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("first ensureSessionLease did not acquire lease")
	}

	secondErr := make(chan error, 1)
	go func() {
		secondErr <- tab.ensureSessionLease(path)
	}()

	select {
	case err := <-secondErr:
		t.Fatalf("second ensureSessionLease returned while first acquire was unbound: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseHook)
	if err := <-firstErr; err != nil {
		t.Fatalf("first ensureSessionLease: %v", err)
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second ensureSessionLease should reuse the tab lease: %v", err)
	}
}

func TestAttachExistingSessionRuntimeSkipsRemovedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "detached.jsonl")
	key := sessionRuntimeKey(path)
	detachedCtrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "detached", Sink: event.Discard})
	defer detachedCtrl.Close()
	detached := &WorkspaceTab{
		ID:            "detached",
		Scope:         "global",
		SessionPath:   path,
		Ctrl:          detachedCtrl,
		Ready:         true,
		SharedHostKey: "detached-host",
		sink:          &tabEventSink{tabID: "detached"},
		disabledMCP:   map[string]ServerView{},
	}
	target := &WorkspaceTab{
		ID:          "target",
		Scope:       "global",
		SessionPath: path,
		removed:     true,
		sink:        &tabEventSink{tabID: "target"},
		disabledMCP: map[string]ServerView{},
	}
	app := &App{
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{key: detached},
	}

	if app.attachExistingSessionRuntime(target, path, nil) {
		t.Fatal("removed tab reattached a detached runtime")
	}
	if app.detachedSessions[key] != detached {
		t.Fatal("detached runtime was removed")
	}
	if target.Ctrl != nil || target.Ready {
		t.Fatal("removed target tab was mutated")
	}
}

func TestRemoveWorkspaceDropsVisibleTabsAndPersistedEntries(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", Scope: "project", WorkspaceRoot: projectRoot, TopicID: "topic-project", Ready: true, disabledMCP: map[string]ServerView{}},
			"global":  {ID: "global", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), TopicID: "topic-global", Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"project", "global"},
		activeTabID:      "project",
		detachedSessions: map[string]*WorkspaceTab{},
	}
	app.mu.Lock()
	app.saveTabsLocked()
	app.mu.Unlock()

	if err := app.RemoveWorkspace(projectRoot); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "global")
	if got := app.ListWorkspaces(); len(got) != 0 {
		t.Fatalf("workspaces after remove = %+v, want none", got)
	}
	if got := loadTabsFile(); len(got.Tabs) != 1 || got.Tabs[0].ID != "global" {
		t.Fatalf("persisted tabs after workspace remove = %+v, want only global", got)
	}
}

func TestRemoveWorkspaceSnapshotsProjectTabBeforeRemovingBinding(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", Scope: "project", WorkspaceRoot: projectRoot, TopicID: "topic-project", Ready: true, disabledMCP: map[string]ServerView{}},
			"global":  {ID: "global", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), TopicID: "topic-global", Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"project", "global"},
		activeTabID:      "project",
		detachedSessions: map[string]*WorkspaceTab{},
	}
	sawBindingDuringSnapshot := false
	app.tabs["project"].Ctrl = &snapshotObservingSession{
		SessionAPI: control.New(control.Options{Label: "project"}),
		onSnapshot: func() {
			sawBindingDuringSnapshot = app.tabs["project"] != nil
		},
	}

	if err := app.RemoveWorkspace(projectRoot); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	if !sawBindingDuringSnapshot {
		t.Fatal("project tab was removed from app.tabs before Snapshot")
	}
}

func TestRemoveWorkspaceSnapshotsProjectTabWithoutAppLock(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", Scope: "project", WorkspaceRoot: projectRoot, TopicID: "topic-project", Ready: true, disabledMCP: map[string]ServerView{}},
			"global":  {ID: "global", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), TopicID: "topic-global", Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"project", "global"},
		activeTabID:      "project",
		detachedSessions: map[string]*WorkspaceTab{},
	}
	snapshotChecks := make(chan struct{}, 1)
	app.tabs["project"].Ctrl = &snapshotObservingSession{
		SessionAPI: control.New(control.Options{Label: "project"}),
		onSnapshot: expectAppMutexAvailableDuringSnapshot(t, app, snapshotChecks),
	}

	if err := app.RemoveWorkspace(projectRoot); err != nil {
		t.Fatalf("RemoveWorkspace: %v", err)
	}
	select {
	case <-snapshotChecks:
	case <-time.After(time.Second):
		t.Fatal("project tab was not snapshotted before removing workspace")
	}
	assertTabIDs(t, app.ListTabs(), "global")
}

func TestRemoveWorkspaceRejectsRunningProjectRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir project sessions: %v", err)
	}
	path := filepath.Join(dir, "running.jsonl")
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: path, Label: "running", Sink: event.Discard})
	project := &WorkspaceTab{
		ID:            "project",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic-project",
		SessionPath:   path,
		Ctrl:          ctrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: "project"},
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": project,
			"global":  {ID: "global", Scope: "global", WorkspaceRoot: globalTabWorkspaceRoot(), Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"project", "global"},
		activeTabID:      "project",
		detachedSessions: map[string]*WorkspaceTab{},
	}

	ctrl.Submit("block")
	<-runner.started
	if err := app.RemoveWorkspace(projectRoot); err == nil {
		t.Fatal("RemoveWorkspace succeeded with a running project session")
	}
	if got := app.ListWorkspaces(); len(got) != 1 || got[0].Path != normalizeProjectRoot(projectRoot) {
		t.Fatalf("workspaces after rejected remove = %+v, want project retained", got)
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
	ctrl.Close()
}

func TestListTabsRepairsStaleOrderWithoutRacing(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")
	app.tabOrder = []string{"a"}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan string, 8)
	iterations := 100
	if testing.Short() {
		iterations = 5
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				if got := strings.Join(tabIDs(app.ListTabs()), ","); got != "a,b,c" {
					errs <- got
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for got := range errs {
		t.Fatalf("tab ids = %q, want a,b,c", got)
	}

	if got := strings.Join(app.tabOrder, ","); got != "a,b,c" {
		t.Fatalf("repaired tab order = %q, want a,b,c", got)
	}
}

func TestSaveTabsSkipsOlderSnapshot(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b")

	app.mu.Lock()
	dir, oldEntries, oldActiveID, oldVersion := app.saveTabsCollectLocked()
	app.activeTabID = "b"
	_, newEntries, newActiveID, newVersion := app.saveTabsCollectLocked()
	app.mu.Unlock()

	app.saveTabsWrite(dir, newEntries, newActiveID, newVersion)
	app.saveTabsWrite(dir, oldEntries, oldActiveID, oldVersion)

	if got := loadTabsFile().ActiveTab; got != "b" {
		t.Fatalf("persisted active tab = %q, want b", got)
	}
}

func TestReorderTabsPersistsSubmittedOrder(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")

	if err := app.ReorderTabs([]string{"c", "a", "b"}); err != nil {
		t.Fatalf("ReorderTabs: %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "c", "a", "b")
	if got := app.activeTabID; got != "a" {
		t.Fatalf("active tab = %q, want a", got)
	}
}

func TestCloseActiveTabChoosesNeighborByOrder(t *testing.T) {
	app := testAppWithOrderedTabs(t, "b", "a", "b", "c")
	if err := app.CloseTab("b"); err != nil {
		t.Fatalf("CloseTab(b): %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "a", "c")
	if got := app.activeTabID; got != "c" {
		t.Fatalf("active tab after closing middle = %q, want c", got)
	}

	if err := app.CloseTab("c"); err != nil {
		t.Fatalf("CloseTab(c): %v", err)
	}
	assertTabIDs(t, app.ListTabs(), "a")
	if got := app.activeTabID; got != "a" {
		t.Fatalf("active tab after closing last = %q, want a", got)
	}
}

func TestCloseActiveTabDoesNotResetSurvivorPlannerSession(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrlClosed := control.New(control.Options{Label: "closed"})
	ctrlSurvivor := &resetCountingSession{SessionAPI: control.New(control.Options{Label: "survivor"})}
	defer ctrlSurvivor.Close()
	app := testAppWithOrderedTabs(t, "a", "a", "b")
	app.tabs["a"].Ctrl = ctrlClosed
	app.tabs["b"].Ctrl = ctrlSurvivor

	if err := app.CloseTab("a"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if ctrlSurvivor.resets != 0 {
		t.Fatalf("survivor planner resets = %d, want 0", ctrlSurvivor.resets)
	}
}

func TestCloseRunningTabDetachesSessionRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "running.jsonl")
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: path, Label: "running", Sink: event.Discard})
	tab := &WorkspaceTab{
		ID:            "running",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		SessionPath:   path,
		Ctrl:          ctrl,
		Ready:         true,
		sink:          &tabEventSink{tabID: "running"},
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"running": tab,
			"other":   {ID: "other", Scope: "global", Ready: true, disabledMCP: map[string]ServerView{}},
		},
		tabOrder:         []string{"running", "other"},
		activeTabID:      "running",
		detachedSessions: map[string]*WorkspaceTab{},
	}

	ctrl.Submit("block")
	<-runner.started
	if err := app.CloseTab("running"); err != nil {
		t.Fatalf("CloseTab(running): %v", err)
	}
	if !ctrl.Running() {
		t.Fatal("closing a visible tab cancelled its running controller")
	}
	if _, ok := app.detachedSessions[sessionRuntimeKey(path)]; !ok {
		t.Fatalf("detached runtime missing for %q", path)
	}
	if tab.sink.ctx != nil {
		t.Fatal("detached tab sink should stop emitting to the closed view")
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
	ctrl.Close()
}

func TestBuildTabControllerReattachesDetachedSessionRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "reattach.jsonl")
	oldSink := &tabEventSink{tabID: "old"}
	oldCtrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "detached", Sink: oldSink})
	defer oldCtrl.Close()
	app := NewApp()
	app.detachedSessions[sessionRuntimeKey(path)] = &WorkspaceTab{
		ID:            "old",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		SessionPath:   path,
		Ctrl:          oldCtrl,
		Label:         "detached",
		Ready:         true,
		sink:          oldSink,
		model:         "detached-model",
		disabledMCP:   map[string]ServerView{},
	}
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "new")
	tab.SessionPath = path
	tab.sink = &tabEventSink{tabID: "new", app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl != oldCtrl {
		t.Fatalf("reattached controller = %p, want detached %p", tab.Ctrl, oldCtrl)
	}
	if tab.sink != oldSink {
		t.Fatalf("reattached sink = %p, want detached %p", tab.sink, oldSink)
	}
	if oldSink.tabID != "new" {
		t.Fatalf("sink tab id = %q, want new", oldSink.tabID)
	}
	if _, ok := app.detachedSessions[sessionRuntimeKey(path)]; ok {
		t.Fatal("detached runtime was not removed after reattach")
	}
}

func TestBuildTabControllerReusesOpenSessionPathRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "open-runtime.jsonl")
	oldSink := &tabEventSink{tabID: "old"}
	oldCtrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "open", Sink: oldSink})
	defer oldCtrl.Close()
	app := NewApp()
	oldTab := &WorkspaceTab{
		ID:            "old",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		SessionPath:   path,
		Ctrl:          oldCtrl,
		Label:         "open",
		Ready:         true,
		sink:          oldSink,
		disabledMCP:   map[string]ServerView{},
	}
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "new")
	tab.SessionPath = path
	tab.sink = &tabEventSink{tabID: "new", app: app}
	app.tabs[oldTab.ID] = oldTab
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{oldTab.ID, tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl != oldCtrl {
		t.Fatalf("reused controller = %p, want open %p", tab.Ctrl, oldCtrl)
	}
	if oldSink.tabID != "new" {
		t.Fatalf("sink tab id = %q, want new", oldSink.tabID)
	}
	if _, ok := app.tabs[oldTab.ID]; ok {
		t.Fatal("source tab for reused runtime should be removed")
	}
}

func TestBuildTabControllerBlocksWhenSessionLeaseHeld(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "leased.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer lease.Release()

	app := NewApp()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "leased")
	tab.SessionPath = path
	tab.sink = &tabEventSink{tabID: "leased", app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl != nil {
		t.Fatalf("tab controller = %T, want nil when lease is held", tab.Ctrl)
	}
	if !tab.Ready {
		t.Fatal("tab should be ready with startup error")
	}
	// The surfaced startup error is the sanitized busy message: the raw lease
	// error would leak the session path and the holder's host-pid-writer id
	// into the topbar banner.
	if !strings.Contains(tab.StartupErr, "already open in another Reasonix window") {
		t.Fatalf("startup error = %q, want user-facing busy message", tab.StartupErr)
	}
	if strings.Contains(tab.StartupErr, agent.ErrSessionLeaseHeld.Error()) ||
		strings.Contains(tab.StartupErr, path) {
		t.Fatalf("startup error leaked raw lease details: %q", tab.StartupErr)
	}
}

func TestDeferredStartupRetryBuildsAfterLeaseRelease(t *testing.T) {
	isolateDesktopUserDirs(t)
	prevInterval := deferredRebuildRetryInterval
	deferredRebuildRetryInterval = 20 * time.Millisecond
	t.Cleanup(func() { deferredRebuildRetryInterval = prevInterval })

	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "startup-retry.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			lease.Release()
		}
	})

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.enableDeferredRebuildRetry()
	t.Cleanup(app.stopDeferredRebuildRetry)
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "startup_retry")
	tab.SessionPath = path
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	installNoopRuntimeEvents(app, tab.sink)
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if ctrl := app.controllerForTab(tab); ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	app.buildTabController(tab)
	if tab.Ctrl != nil {
		t.Fatalf("controller = %T, want nil while external lease is held", tab.Ctrl)
	}
	if !tab.StartupErrLeaseHeld {
		t.Fatalf("startup retry flag = false, startup err = %q", tab.StartupErr)
	}
	if !app.deferredRebuildPending(tab.ID) {
		t.Fatal("startup retry was not scheduled while the lease was held")
	}

	lease.Release()
	released = true

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !app.deferredRebuildPending(tab.ID) && app.controllerForTab(tab) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if app.deferredRebuildPending(tab.ID) {
		t.Fatal("startup retry is still pending after the lease was released")
	}
	if ctrl := app.controllerForTab(tab); ctrl == nil {
		t.Fatal("controller was not rebuilt after the lease was released")
	}
	if tab.StartupErr != "" || tab.StartupErrLeaseHeld {
		t.Fatalf("startup error after retry = %q retryable=%v, want cleared", tab.StartupErr, tab.StartupErrLeaseHeld)
	}
}

func TestTabAndCtrlByIDRecoversStartupLeaseBeforeAction(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "startup-before-action.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			lease.Release()
		}
	})

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "startup_action")
	tab.SessionPath = path
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	installNoopRuntimeEvents(app, tab.sink)
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if ctrl := app.controllerForTab(tab); ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	app.buildTabController(tab)
	if !tab.StartupErrLeaseHeld {
		t.Fatalf("startup retry flag = false, startup err = %q", tab.StartupErr)
	}
	lease.Release()
	released = true

	gotTab, ctrl := app.tabAndCtrlByID(tab.ID)
	if gotTab != tab {
		t.Fatalf("tabAndCtrlByID tab = %p, want %p", gotTab, tab)
	}
	if ctrl == nil {
		t.Fatal("tabAndCtrlByID did not rebuild the controller before returning")
	}
	if app.deferredRebuildPending(tab.ID) {
		t.Fatal("startup retry remained pending after synchronous recovery")
	}
	if tab.StartupErr != "" || tab.StartupErrLeaseHeld {
		t.Fatalf("startup error after synchronous recovery = %q retryable=%v, want cleared", tab.StartupErr, tab.StartupErrLeaseHeld)
	}
}

func TestOpenGlobalTabResolvesTopicToLatestSessionRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_multi_session"
	topicTitle := "Multi session topic"
	oldPath := writeTopicSessionWithPrompt(t, dir, "old.jsonl", topicID, topicTitle, "", "old session prompt", time.Now().Add(-2*time.Hour))
	newPath := writeTopicSessionWithPrompt(t, dir, "new.jsonl", topicID, topicTitle, "", "new session prompt", time.Now().Add(-time.Hour))

	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	oldSink := &tabEventSink{tabID: "topic-tab"}
	oldCtrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: oldPath, Label: "old", Sink: oldSink})
	defer oldCtrl.Close()
	app := NewApp()
	oldSink.app = app
	oldTab := &WorkspaceTab{
		ID:            "topic-tab",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   oldPath,
		Ctrl:          oldCtrl,
		Ready:         true,
		sink:          oldSink,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs[oldTab.ID] = oldTab
	app.tabOrder = []string{oldTab.ID}
	app.activeTabID = oldTab.ID

	oldCtrl.Submit("keep old runtime running")
	<-runner.started

	meta, err := app.OpenGlobalTab(topicID)
	if err != nil {
		t.Fatalf("OpenGlobalTab: %v", err)
	}
	if meta.ID != oldTab.ID {
		t.Fatalf("OpenGlobalTab reused tab %q, want %q", meta.ID, oldTab.ID)
	}
	if !oldCtrl.Running() {
		t.Fatal("old session runtime was cancelled while selecting topic")
	}
	if detached := app.detachedSessions[sessionRuntimeKey(oldPath)]; detached == nil || detached.Ctrl != oldCtrl {
		t.Fatalf("old runtime was not detached under its session path: %+v", detached)
	}
	visible := app.tabs[oldTab.ID]
	if visible == nil || visible.Ctrl == nil {
		t.Fatalf("visible tab was not rebuilt: %+v", visible)
	}
	if got := filepath.Clean(visible.Ctrl.SessionPath()); got != filepath.Clean(newPath) {
		t.Fatalf("visible session path = %q, want %q", got, newPath)
	}
	history := visible.Ctrl.History()
	if len(history) == 0 || history[0].Content != "new session prompt" {
		t.Fatalf("visible history = %+v, want latest session prompt", history)
	}

	close(runner.release)
	waitNotRunning(t, oldCtrl)
}

func TestReorderTabsRejectsInvalidOrder(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")
	for name, order := range map[string][]string{
		"missing":   {"a", "b"},
		"unknown":   {"a", "b", "missing"},
		"duplicate": {"a", "b", "b"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := app.ReorderTabs(order); err == nil {
				t.Fatalf("ReorderTabs(%v) succeeded, want error", order)
			}
		})
	}
	assertTabIDs(t, app.ListTabs(), "a", "b", "c")
}

func TestNewUniqueTabIDLockedUsesFreshRandomID(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")

	app.mu.Lock()
	got := app.newUniqueTabIDLocked()
	app.mu.Unlock()
	if _, exists := app.tabs[got]; exists {
		t.Fatalf("newUniqueTabIDLocked returned existing id %q", got)
	}
	if !strings.HasPrefix(got, "tab_") {
		t.Fatalf("tab id = %q, want tab_ prefix", got)
	}
	if len(got) != len("tab_")+32 {
		t.Fatalf("tab id = %q, length %d, want 36", got, len(got))
	}
}

func TestRestoredTabIDLockedReplacesEmptyAndDuplicateIDs(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")

	app.mu.Lock()
	kept := app.restoredTabIDLocked("d")
	duplicate := app.restoredTabIDLocked("a")
	empty := app.restoredTabIDLocked(" ")
	app.mu.Unlock()

	if kept != "d" {
		t.Fatalf("restored unique id = %q, want d", kept)
	}
	for name, got := range map[string]string{"duplicate": duplicate, "empty": empty} {
		if _, exists := app.tabs[got]; exists {
			t.Fatalf("%s restored id %q already exists", name, got)
		}
		if !strings.HasPrefix(got, "tab_") {
			t.Fatalf("%s restored id = %q, want tab_ prefix", name, got)
		}
	}
}
