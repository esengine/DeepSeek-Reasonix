package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestListTabsRepairsStaleOrderWithoutRacing(t *testing.T) {
	app := testAppWithOrderedTabs(t, "a", "a", "b", "c")
	app.tabOrder = []string{"a"}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
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
