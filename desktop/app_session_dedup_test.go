package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func carryingController(carried []provider.Message, path string) *control.Controller {
	sess := &agent.Session{}
	sess.Replace(carried)
	ag := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	return control.New(control.Options{Executor: ag, SessionPath: path, Sink: event.Discard})
}

// TestCarriedRebuildsKeepOneSession reproduces issue #2807: a model switch or any
// config change rebuilds the controller and carries the conversation forward. Each
// rebuild must keep writing to the same file, so a run of them leaves exactly one
// history entry — not a new identical duplicate per rebuild.
func TestCarriedRebuildsKeepOneSession(t *testing.T) {
	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "model-a")
	ctrl := controllerWithContent(t, path)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		prevPath := ctrl.SessionPath()
		carried := ctrl.History()
		ctrl.Close()

		newPath := agent.ContinueSessionPath(prevPath, dir, "model-b")
		ctrl = carryingController(carried, newPath)
		if err := ctrl.Snapshot(); err != nil {
			t.Fatal(err)
		}
	}
	ctrl.Close()

	infos, err := agent.ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		paths := make([]string, len(infos))
		for i, s := range infos {
			paths[i] = filepath.Base(s.Path)
		}
		t.Fatalf("after 5 carried rebuilds the history shows %d sessions, want 1: %v", len(infos), paths)
	}
}

// EnsureBlankTab reuses an already-open blank tab rather than creating a second one.

func TestEnsureBlankTabReusesExistingBlankTab(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	first, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionPath == "" {
		t.Fatal("EnsureBlankTab should pre-create a session path for immediate deletion")
	}
	if _, err := os.Stat(first.SessionPath); err != nil {
		t.Fatalf("pre-created blank session should exist: %v", err)
	}
	second, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("EnsureBlankTab created duplicate blank tab: first=%q second=%q", first.ID, second.ID)
	}
	if tabs := app.ListTabs(); len(tabs) != 1 {
		t.Fatalf("ListTabs length = %d, want 1: %+v", len(tabs), tabs)
	}
}

func TestEnsureBlankTabReusesPrecreatedBlankBeforeControllerReady(t *testing.T) {
	isolateDesktopUserDirs(t)

	globalRoot := globalWorkspaceRoot()
	if err := os.MkdirAll(globalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := agent.NewSessionPath(desktopSessionDir(globalRoot), "")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	topic, err := app.CreateTopic("global", "", "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	app.tabs["blank"] = &WorkspaceTab{
		ID:            "blank",
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		TopicID:       topic.ID,
		TopicTitle:    defaultTopicTitle,
		SessionPath:   sessionPath,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabOrder = []string{"blank"}
	app.activeTabID = "blank"

	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.ID != "blank" {
		t.Fatalf("EnsureBlankTab created duplicate blank tab %q, want existing pre-created blank", meta.ID)
	}
}

func TestEnsureBlankTabReusesIndexedTopicWithEmptyStub(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	topic, err := app.CreateTopic("global", "", "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	globalRoot := globalWorkspaceRoot()
	dir := desktopSessionDir(globalRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	stubPath := filepath.Join(dir, "empty-stub.jsonl")
	if err := os.WriteFile(stubPath, nil, 0o644); err != nil {
		t.Fatalf("write empty stub: %v", err)
	}
	now := time.Now()
	if err := agent.SaveBranchMetaPreserveUpdated(stubPath, agent.BranchMeta{
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now,
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		TopicID:       topic.ID,
		TopicTitle:    defaultTopicTitle,
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}

	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.TopicID != topic.ID {
		t.Fatalf("EnsureBlankTab topic = %q, want reused empty topic %q", meta.TopicID, topic.ID)
	}
}

// EnsureBlankTab reuses an already-open project-scoped blank tab.

func TestEnsureBlankTabCreatesOneBlankPerProject(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	first, err := app.EnsureBlankTab("project", projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.EnsureBlankTab("project", projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("EnsureBlankTab created duplicate project blank tab: first=%q second=%q", first.ID, second.ID)
	}
	if tabs := app.ListTabs(); len(tabs) != 1 {
		t.Fatalf("ListTabs length = %d, want 1: %+v", len(tabs), tabs)
	}
}

func TestEnsureBlankTabResetsReusableAutoTopicTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := setTopicTitleWithSource(projectRoot, topic.ID, "Old auto title", topicTitleSourceAuto); err != nil {
		t.Fatalf("set stale auto title: %v", err)
	}
	tab := app.createTabEntryWithID("project", projectRoot, topic.ID, "tab1")
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	meta, err := app.EnsureBlankTab("project", projectRoot)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if got := meta.TopicTitle; got != defaultTopicTitle {
		t.Fatalf("reused auto topic title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != defaultTopicTitle {
		t.Fatalf("stored title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceAuto {
		t.Fatalf("title source = %q, want auto", got)
	}
}

func TestEnsureBlankTabPreservesReusableManualTopicTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "Manual title")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	tab := app.createTabEntryWithID("project", projectRoot, topic.ID, "tab1")
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	meta, err := app.EnsureBlankTab("project", projectRoot)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if got := meta.TopicTitle; got != "Manual title" {
		t.Fatalf("reused manual topic title = %q, want Manual title", got)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "Manual title" {
		t.Fatalf("stored title = %q, want Manual title", got)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("title source = %q, want manual", got)
	}
}

func TestEnsureBlankTabKeepsActiveTabWhenTitleResetFails(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := setTopicTitleWithSource(projectRoot, topic.ID, "Old auto title", topicTitleSourceAuto); err != nil {
		t.Fatalf("set stale auto title: %v", err)
	}
	activeTab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "active-tab")
	reusableTab := app.createTabEntryWithID("project", projectRoot, topic.ID, "reusable-tab")
	app.tabs[activeTab.ID] = activeTab
	app.tabs[reusableTab.ID] = reusableTab
	app.tabOrder = []string{activeTab.ID, reusableTab.ID}
	app.activeTabID = activeTab.ID

	titlePath := topicTitlesPath(projectRoot)
	if err := os.Remove(titlePath); err != nil {
		t.Fatalf("remove title file: %v", err)
	}
	if err := os.Mkdir(titlePath, 0o755); err != nil {
		t.Fatalf("replace title file with directory: %v", err)
	}

	if _, err := app.EnsureBlankTab("project", projectRoot); err == nil {
		t.Fatal("EnsureBlankTab succeeded, want title reset error")
	}
	if got := app.activeTabID; got != activeTab.ID {
		t.Fatalf("active tab after failed title reset = %q, want %q", got, activeTab.ID)
	}
}

// EnsureBlankTab picks up an existing blank topic created in the sidebar
// instead of creating a fresh topic, for global scope.

func TestEnsureBlankTabOpensExistingSidebarBlankTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	topic, err := app.CreateTopic("global", "", "")
	if err != nil {
		t.Fatal(err)
	}

	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatal(err)
	}
	if meta.TopicID != topic.ID {
		t.Fatalf("EnsureBlankTab opened topic %q, want existing blank topic %q", meta.TopicID, topic.ID)
	}
	if topics := loadProjectsFile().GlobalTopics; len(topics) != 1 {
		t.Fatalf("global topics length = %d, want 1: %v", len(topics), topics)
	}
}

// EnsureBlankTab picks up an existing blank topic created in the sidebar
// instead of creating a fresh topic, for project scope.

func TestEnsureBlankTabOpensExistingProjectSidebarBlankTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatal(err)
	}

	meta, err := app.EnsureBlankTab("project", projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if meta.TopicID != topic.ID {
		t.Fatalf("EnsureBlankTab opened topic %q, want existing blank topic %q", meta.TopicID, topic.ID)
	}
	var topics []string
	for _, project := range loadProjectsFile().Projects {
		if project.Root == projectRoot {
			topics = project.Topics
			break
		}
	}
	if len(topics) != 1 {
		t.Fatalf("project topics length = %d, want 1: %v", len(topics), topics)
	}
}

func TestEnsureBlankTabDoesNotReuseProjectTopicWithSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := robustTempDir(t)
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("CreateTopic: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	existingPath := writeTopicSession(t, dir, "existing.jsonl", topic.ID, defaultTopicTitle, projectRoot)
	if got, _ := app.findTopicSessionForTarget("project", projectRoot, topic.ID); got != existingPath {
		t.Fatalf("precondition topic session = %q, want %q", got, existingPath)
	}

	meta, err := app.EnsureBlankTab("project", projectRoot)
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.TopicID == topic.ID {
		t.Fatalf("EnsureBlankTab reused topic %q even though it already has session %q", topic.ID, existingPath)
	}
	if got, _ := app.findTopicSessionForTarget("project", projectRoot, topic.ID); got != existingPath {
		t.Fatalf("existing topic session changed = %q, want %q", got, existingPath)
	}
}

// NewSession skips the snapshot when the current tab has no real conversation content.

func TestNewSessionNoopsWhenCurrentTabIsBlank(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := t.TempDir()
	path := agent.NewSessionPath(dir, "model-a")
	ctrl := carryingController([]provider.Message{{Role: provider.RoleSystem, Content: "sys"}}, path)
	app := NewApp()
	app.setTestCtrl(ctrl, "model-a")

	if err := app.NewSession(); err != nil {
		t.Fatal(err)
	}
	if got := ctrl.SessionPath(); got != path {
		t.Fatalf("blank NewSession changed session path = %q, want %q", got, path)
	}
}

func TestNewSessionUsesFreshTopicIdentity(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	oldTopicID := "topic_old"
	oldTopicTitle := "Old topic"
	oldPath := writeTopicSessionWithPrompt(t, dir, "old.jsonl", oldTopicID, oldTopicTitle, projectRoot, "old prompt", time.Now().Add(-time.Hour))
	sess := &agent.Session{}
	sess.Replace([]provider.Message{{Role: provider.RoleUser, Content: "old prompt"}})
	ag := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: ag, SessionDir: dir, SessionPath: oldPath, Sink: event.Discard})

	app := NewApp()
	app.setTestCtrl(ctrl, "model-a")
	tab := app.tabs["test"]
	tab.Scope = "project"
	tab.WorkspaceRoot = projectRoot
	tab.TopicID = oldTopicID
	tab.TopicTitle = oldTopicTitle
	tab.SessionPath = oldPath
	app.projectTreeChangedHook = func() {}

	if err := app.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := tab.TopicID; got == "" || got == oldTopicID {
		t.Fatalf("new session topic ID = %q, want fresh ID distinct from %q", got, oldTopicID)
	}
	if got := tab.TopicTitle; got != defaultTopicTitle {
		t.Fatalf("new session topic title = %q, want %q", got, defaultTopicTitle)
	}
	newPath := ctrl.SessionPath()
	if newPath == "" || filepath.Clean(newPath) == filepath.Clean(oldPath) {
		t.Fatalf("new session path = %q, want fresh path distinct from %q", newPath, oldPath)
	}

	if err := os.WriteFile(newPath, []byte(`{"role":"user","content":"new prompt"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write new session: %v", err)
	}
	if !app.maybeAutoTitleTopic(tab) {
		t.Fatalf("new session should auto-title its fresh topic")
	}

	oldMeta, ok, err := agent.LoadBranchMeta(oldPath)
	if err != nil || !ok {
		t.Fatalf("load old meta: ok=%v err=%v", ok, err)
	}
	if oldMeta.TopicID != oldTopicID || oldMeta.TopicTitle != oldTopicTitle {
		t.Fatalf("old session meta changed after new session auto-title: %+v", oldMeta)
	}
	newMeta, ok, err := agent.LoadBranchMeta(newPath)
	if err != nil || !ok {
		t.Fatalf("load new meta: ok=%v err=%v", ok, err)
	}
	if newMeta.TopicID != tab.TopicID || newMeta.TopicTitle != "new prompt" {
		t.Fatalf("new session meta = %+v, want topic %q titled new prompt", newMeta, tab.TopicID)
	}
}

func TestNewSessionKeepsFreshRuntimeWhenTopicRepairFails(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := agent.NewSessionPath(dir, "model-a")
	ctrl := controllerWithContent(t, path)
	app := NewApp()
	app.projectTreeChangedHook = func() {}
	app.setTestCtrl(ctrl, "model-a")
	tab := app.tabs["test"]
	tab.TopicID = "topic_old"
	tab.TopicTitle = "Old topic"

	// Block desktopConfigDir-backed topic-index writes without affecting the
	// session directory, which exercises the post-NewSession repair failure path.
	if err := os.MkdirAll(filepath.Dir(desktopConfigDir()), 0o755); err != nil {
		t.Fatalf("mkdir desktop config parent: %v", err)
	}
	if err := os.WriteFile(desktopConfigDir(), []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("block desktop config dir: %v", err)
	}

	if err := app.NewSession(); err != nil {
		t.Fatalf("NewSession should keep the fresh runtime even when topic repair fails: %v", err)
	}
	if got := tab.TopicID; got == "" || got == "topic_old" {
		t.Fatalf("new session topic ID = %q, want fresh ID distinct from the old topic", got)
	}
	if got := tab.TopicTitle; got != defaultTopicTitle {
		t.Fatalf("new session topic title = %q, want %q", got, defaultTopicTitle)
	}
	if got := ctrl.SessionPath(); got == "" || filepath.Clean(got) == filepath.Clean(path) {
		t.Fatalf("new session path = %q, want a fresh path distinct from %q", got, path)
	}
}
