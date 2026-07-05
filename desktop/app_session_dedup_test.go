package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestEnsureBlankTabStoresCreatedAt(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	before := time.Now().UnixMilli()
	meta, err := app.EnsureBlankTab("global", "")
	after := time.Now().UnixMilli()
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}

	createdAt := loadTopicCreatedAt("", meta.TopicID)
	if createdAt < before || createdAt > after {
		t.Fatalf("createdAt = %d, want between %d and %d", createdAt, before, after)
	}

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want Global with one topic", nodes)
	}
	if got := nodes[0].Children[0].CreatedAt; got != createdAt {
		t.Fatalf("project tree createdAt = %d, want %d", got, createdAt)
	}
}

func TestEnsureBlankTabRepairsMissingCreatedAtForReusedTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	const topicID = "topic_20260704-104018_deadbeef"
	if err := setTopicTitleWithSource("", topicID, defaultTopicTitle, topicTitleSourceAuto); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	if err := prependTopicInProjectsFile("", topicID, false); err != nil {
		t.Fatalf("prepend topic: %v", err)
	}
	if got := loadTopicCreatedAt("", topicID); got != 0 {
		t.Fatalf("createdAt before reuse = %d, want 0", got)
	}

	app := NewApp()
	meta, err := app.EnsureBlankTab("global", "")
	if err != nil {
		t.Fatalf("EnsureBlankTab: %v", err)
	}
	if meta.TopicID != topicID {
		t.Fatalf("EnsureBlankTab topic = %q, want reused topic %q", meta.TopicID, topicID)
	}

	expected := time.Date(2026, 7, 4, 10, 40, 18, 0, time.UTC).UnixMilli()
	if got := loadTopicCreatedAt("", topicID); got != expected {
		t.Fatalf("repaired createdAt = %d, want %d", got, expected)
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

func TestEnsureBlankTabStartsProjectRuntimeWithCurrentWorkspacePrompt(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := robustTempDir(t)
	projectB := robustTempDir(t)
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}

	app := NewApp()
	first, err := app.EnsureBlankTab("project", projectA)
	if err != nil {
		t.Fatalf("EnsureBlankTab(project A): %v", err)
	}
	tabA := waitForTabReady(t, app, first.ID)
	if got := normalizeProjectRoot(tabA.Ctrl.WorkspaceRoot()); got != normalizeProjectRoot(projectA) {
		t.Fatalf("project A controller workspace root = %q, want %q", got, normalizeProjectRoot(projectA))
	}

	second, err := app.EnsureBlankTab("project", projectB)
	if err != nil {
		t.Fatalf("EnsureBlankTab(project B): %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("EnsureBlankTab reused project A tab %q for project B", second.ID)
	}
	tabB := waitForTabReady(t, app, second.ID)

	if got := normalizeProjectRoot(tabB.WorkspaceRoot); got != normalizeProjectRoot(projectB) {
		t.Fatalf("project B tab workspace root = %q, want %q", got, normalizeProjectRoot(projectB))
	}
	if got := normalizeProjectRoot(tabB.Ctrl.WorkspaceRoot()); got != normalizeProjectRoot(projectB) {
		t.Fatalf("project B controller workspace root = %q, want %q", got, normalizeProjectRoot(projectB))
	}
	if !sameDesktopPath(tabB.Ctrl.SessionDir(), desktopSessionDir(projectB)) {
		t.Fatalf("project B controller session dir = %q, want %q", tabB.Ctrl.SessionDir(), desktopSessionDir(projectB))
	}
	if !sameDesktopPath(filepath.Dir(tabB.Ctrl.SessionPath()), desktopSessionDir(projectB)) {
		t.Fatalf("project B controller session path = %q, want under %q", tabB.Ctrl.SessionPath(), desktopSessionDir(projectB))
	}
	sys := systemPromptFrom(tabB.Ctrl.History())
	if !strings.Contains(sys, "Current workspace: "+strconv.Quote(projectB)) {
		t.Fatalf("project B system prompt missing current workspace %q:\n%s", projectB, sys)
	}
	if strings.Contains(sys, "Current workspace: "+strconv.Quote(projectA)) {
		t.Fatalf("project B system prompt retained project A workspace %q:\n%s", projectA, sys)
	}
}

func TestBlankTabSessionPathRejectsOtherProjectWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := robustTempDir(t)
	projectB := robustTempDir(t)
	pathA, err := createEmptySessionFile(desktopSessionDir(projectA), "test-model")
	if err != nil {
		t.Fatalf("create project A empty session: %v", err)
	}
	tab := &WorkspaceTab{
		ID:            "blank-project-b",
		Scope:         "project",
		WorkspaceRoot: projectB,
		SessionPath:   pathA,
	}

	if blankTabSessionPathHasNoContent(tab) {
		t.Fatalf("blank tab treated session %q from project A as reusable for project B %q", pathA, projectB)
	}
}

func TestForkKeepsProjectWorkspacePrompt(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := robustTempDir(t)
	projectB := robustTempDir(t)
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}

	app := NewApp()
	first, err := app.EnsureBlankTab("project", projectA)
	if err != nil {
		t.Fatalf("EnsureBlankTab(project A): %v", err)
	}
	waitForTabReady(t, app, first.ID)

	second, err := app.EnsureBlankTab("project", projectB)
	if err != nil {
		t.Fatalf("EnsureBlankTab(project B): %v", err)
	}
	tabB := waitForTabReady(t, app, second.ID)
	ctrl := installStubControllerWithCurrentPrompt(t, app, tabB)
	turn := submitStubTurnAndWaitForCheckpoint(t, ctrl, "project B turn")

	forked, err := app.Fork(turn)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if forked.ID == "" || forked.ID == second.ID {
		t.Fatalf("forked tab ID = %q, want a fresh tab distinct from %q", forked.ID, second.ID)
	}
	forkTab := waitForTabReady(t, app, forked.ID)
	if got := normalizeProjectRoot(forkTab.WorkspaceRoot); got != normalizeProjectRoot(projectB) {
		t.Fatalf("fork tab workspace root = %q, want %q", got, normalizeProjectRoot(projectB))
	}
	if got := normalizeProjectRoot(forkTab.Ctrl.WorkspaceRoot()); got != normalizeProjectRoot(projectB) {
		t.Fatalf("fork controller workspace root = %q, want %q", got, normalizeProjectRoot(projectB))
	}
	sys := systemPromptFrom(forkTab.Ctrl.History())
	if !strings.Contains(sys, "Current workspace: "+strconv.Quote(projectB)) {
		t.Fatalf("fork system prompt missing project B workspace %q:\n%s", projectB, sys)
	}
	if strings.Contains(sys, "Current workspace: "+strconv.Quote(projectA)) {
		t.Fatalf("fork system prompt retained project A workspace %q:\n%s", projectA, sys)
	}
}

func TestRewindKeepsProjectWorkspacePrompt(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := robustTempDir(t)
	projectB := robustTempDir(t)
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}

	app := NewApp()
	first, err := app.EnsureBlankTab("project", projectA)
	if err != nil {
		t.Fatalf("EnsureBlankTab(project A): %v", err)
	}
	waitForTabReady(t, app, first.ID)

	second, err := app.EnsureBlankTab("project", projectB)
	if err != nil {
		t.Fatalf("EnsureBlankTab(project B): %v", err)
	}
	tabB := waitForTabReady(t, app, second.ID)
	ctrl := installStubControllerWithCurrentPrompt(t, app, tabB)
	turn := submitStubTurnAndWaitForCheckpoint(t, ctrl, "project B turn")

	if err := app.Rewind(turn, "conversation"); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if got := normalizeProjectRoot(tabB.Ctrl.WorkspaceRoot()); got != normalizeProjectRoot(projectB) {
		t.Fatalf("rewound controller workspace root = %q, want %q", got, normalizeProjectRoot(projectB))
	}
	sys := systemPromptFrom(tabB.Ctrl.History())
	if !strings.Contains(sys, "Current workspace: "+strconv.Quote(projectB)) {
		t.Fatalf("rewound system prompt missing project B workspace %q:\n%s", projectB, sys)
	}
	if strings.Contains(sys, "Current workspace: "+strconv.Quote(projectA)) {
		t.Fatalf("rewound system prompt retained project A workspace %q:\n%s", projectA, sys)
	}
}

func installStubControllerWithCurrentPrompt(t *testing.T, app *App, tab *WorkspaceTab) *control.Controller {
	t.Helper()
	if tab == nil || tab.Ctrl == nil {
		t.Fatal("tab controller is required")
	}
	sys := systemPromptFrom(tab.Ctrl.History())
	if strings.TrimSpace(sys) == "" {
		t.Fatal("tab controller did not expose a system prompt")
	}
	sessionDir := tab.Ctrl.SessionDir()
	sessionPath := tab.Ctrl.SessionPath()
	workspaceRoot := tab.Ctrl.WorkspaceRoot()
	label := tab.Ctrl.Label()
	tab.Ctrl.Close()

	sess := agent.NewSession(sys)
	ag := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Runner:        ag,
		Executor:      ag,
		SessionDir:    sessionDir,
		SessionPath:   sessionPath,
		WorkspaceRoot: workspaceRoot,
		Label:         label,
		SystemPrompt:  sys,
		Sink:          event.Discard,
	})
	tab.Ctrl = ctrl
	app.bindControllerDisplayRecorder(ctrl)
	return ctrl
}

func submitStubTurnAndWaitForCheckpoint(t *testing.T, ctrl control.SessionAPI, input string) int {
	t.Helper()
	ctrl.SubmitUserTurn(input, input)
	waitNotRunning(t, ctrl)

	deadline := time.Now().Add(time.Second)
	for {
		checkpoints := ctrl.Checkpoints()
		if len(checkpoints) > 0 {
			return checkpoints[len(checkpoints)-1].Turn
		}
		if time.Now().After(deadline) {
			t.Fatal("controller did not record a checkpoint")
		}
		time.Sleep(10 * time.Millisecond)
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
