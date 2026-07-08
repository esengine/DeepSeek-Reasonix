package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

func waitForTabReady(t *testing.T, app *App, tabID string) *WorkspaceTab {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		tab := app.tabs[tabID]
		ready := tab != nil && tab.Ready
		startupErr := ""
		if tab != nil {
			startupErr = tab.StartupErr
		}
		app.mu.RUnlock()
		if tab == nil {
			t.Fatalf("tab %q was not found", tabID)
		}
		if ready {
			if startupErr != "" {
				t.Fatalf("tab %q startup error: %s", tabID, startupErr)
			}
			if tab.Ctrl != nil {
				t.Cleanup(func() { tab.Ctrl.Close() })
			}
			return tab
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tab %q was not ready before timeout", tabID)
	return nil
}

func writeTopicSession(t *testing.T, dir, name, topicID, topicTitle, workspaceRoot string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		Scope:         "project",
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	return path
}

func writeTopicSessionWithPrompt(t *testing.T, dir, name, topicID, topicTitle, workspaceRoot, prompt string, updatedAt time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":`+strconv.Quote(prompt)+`}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	scope := "global"
	if strings.TrimSpace(workspaceRoot) != "" {
		scope = "project"
	}
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		CreatedAt:     updatedAt.Add(-time.Minute),
		UpdatedAt:     updatedAt,
		Scope:         scope,
		WorkspaceRoot: workspaceRoot,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	return path
}

func writeLegacySession(t *testing.T, dir, name, prompt string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"role":"user","content":`+strconv.Quote(prompt)+`}`+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes legacy session: %v", err)
	}
	return path
}

func writeLegacyEventSession(t *testing.T, dir, name, prompt, reply string, modTime time.Time) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir legacy sessions: %v", err)
	}
	path := filepath.Join(dir, name)
	body := `{"type":"user.message","id":1,"ts":"t","turn":0,"text":` + strconv.Quote(prompt) + `}` + "\n" +
		`{"type":"model.final","id":2,"ts":"t","turn":0,"content":` + strconv.Quote(reply) + `,"toolCalls":[],"usage":{},"costUsd":0}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write legacy event session: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes legacy event session: %v", err)
	}
	return path
}

func TestSessionListCacheRefillsAfterInvalidate(t *testing.T) {
	cache := &sessionListCache{byDir: map[string]sessionListCacheEntry{}}
	dir := t.TempDir()
	first := []agent.SessionInfo{{Path: filepath.Join(dir, "first.jsonl")}}
	second := []agent.SessionInfo{{Path: filepath.Join(dir, "second.jsonl")}}

	token := cache.versionToken()
	cache.put(dir, first, map[string]string{"first.jsonl": "First"}, token)
	if infos, titles, ok := cache.get(dir); !ok || len(infos) != 1 || filepath.Base(infos[0].Path) != "first.jsonl" || titles["first.jsonl"] != "First" {
		t.Fatalf("initial cache entry = %+v, %+v, %v", infos, titles, ok)
	}

	cache.invalidate()
	if _, _, ok := cache.get(dir); ok {
		t.Fatalf("cache entry survived invalidate")
	}
	cache.put(dir, first, map[string]string{"first.jsonl": "stale"}, token)
	if _, _, ok := cache.get(dir); ok {
		t.Fatalf("stale token repopulated cache after invalidate")
	}

	token = cache.versionToken()
	cache.put(dir, second, map[string]string{"second.jsonl": "Second"}, token)
	if infos, titles, ok := cache.get(dir); !ok || len(infos) != 1 || filepath.Base(infos[0].Path) != "second.jsonl" || titles["second.jsonl"] != "Second" {
		t.Fatalf("refilled cache entry = %+v, %+v, %v", infos, titles, ok)
	}
}

func TestRenameSessionInvalidatesProjectTreeCache(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldProjectCache := projectSessionCache
	projectSessionCache = &sessionListCache{byDir: map[string]sessionListCacheEntry{}}
	t.Cleanup(func() {
		projectSessionCache = oldProjectCache
	})

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "rename-me.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test"})
	defer ctrl.Close()
	app := NewApp()
	app.setTestCtrl(ctrl, "")

	token := projectSessionCache.versionToken()
	projectSessionCache.put(dir, []agent.SessionInfo{{Path: sessionPath}}, map[string]string{"rename-me.jsonl": "old"}, token)
	if _, _, ok := projectSessionCache.get(dir); !ok {
		t.Fatalf("expected primed project tree cache")
	}
	if err := app.RenameSession(sessionPath, "new title"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	if _, _, ok := projectSessionCache.get(dir); ok {
		t.Fatalf("RenameSession should invalidate project tree cache")
	}
}

func TestTopicMetadataUpdatesPreserveExistingEntriesWhenTimedReadSlotsFull(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := saveTopicTitles(projectRoot, map[string]string{"old": "Old"}); err != nil {
		t.Fatalf("save old title: %v", err)
	}
	if err := saveTopicTitleSources(projectRoot, map[string]string{"old": topicTitleSourceManual}); err != nil {
		t.Fatalf("save old source: %v", err)
	}
	if err := saveTopicCreatedAts(projectRoot, map[string]int64{"old": 100}); err != nil {
		t.Fatalf("save old created-at: %v", err)
	}

	release := occupyReadFileWithTimeoutSlots(t)
	if err := setTopicTitleWithSource(projectRoot, "new", "New", topicTitleSourceAuto); err != nil {
		t.Fatalf("setTopicTitleWithSource: %v", err)
	}
	if err := setTopicCreatedAt(projectRoot, "new", 200); err != nil {
		t.Fatalf("setTopicCreatedAt: %v", err)
	}
	release()

	titles := loadTopicTitles(projectRoot)
	if got := titles["old"]; got != "Old" {
		t.Fatalf("old title = %q, want Old (all titles: %v)", got, titles)
	}
	if got := titles["new"]; got != "New" {
		t.Fatalf("new title = %q, want New (all titles: %v)", got, titles)
	}
	sources := loadTopicTitleSources(projectRoot)
	if got := sources["old"]; got != topicTitleSourceManual {
		t.Fatalf("old source = %q, want %q (all sources: %v)", got, topicTitleSourceManual, sources)
	}
	if got := sources["new"]; got != topicTitleSourceAuto {
		t.Fatalf("new source = %q, want %q (all sources: %v)", got, topicTitleSourceAuto, sources)
	}
	created := loadTopicCreatedAts(projectRoot)
	if got := created["old"]; got != 100 {
		t.Fatalf("old created-at = %d, want 100 (all created: %v)", got, created)
	}
	if got := created["new"]; got != 200 {
		t.Fatalf("new created-at = %d, want 200 (all created: %v)", got, created)
	}
}

func TestDeleteTopicKeepsSessionHistory(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_keep_history"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Keep history"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "keep.jsonl", topicID, "Keep history", projectRoot)

	if err := NewApp().DeleteTopic(topicID); err != nil {
		t.Fatalf("delete topic: %v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("delete topic should keep session history: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("topic title should be removed, got %q", got)
	}
}

func TestSetTopicPinnedOrdersProjectTopics(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, "topic_a", "Alpha"); err != nil {
		t.Fatalf("set topic a title: %v", err)
	}
	if err := setTopicTitle(projectRoot, "topic_b", "Beta"); err != nil {
		t.Fatalf("set topic b title: %v", err)
	}
	app := NewApp()
	nodes := app.ListProjectTree()
	if got := []string{nodes[0].Children[0].TopicID, nodes[0].Children[1].TopicID}; got[0] != "topic_a" || got[1] != "topic_b" {
		t.Fatalf("initial topic order = %v, want [topic_a topic_b]", got)
	}

	if err := app.SetTopicPinned("topic_b", true); err != nil {
		t.Fatalf("pin topic: %v", err)
	}
	nodes = app.ListProjectTree()
	if got := []string{nodes[0].Children[0].TopicID, nodes[0].Children[1].TopicID}; got[0] != "topic_b" || got[1] != "topic_a" {
		t.Fatalf("pinned topic order = %v, want [topic_b topic_a]", got)
	}
	if !nodes[0].Children[0].Pinned {
		t.Fatalf("pinned topic should expose pinned=true")
	}

	if err := app.SetTopicPinned("topic_b", false); err != nil {
		t.Fatalf("unpin topic: %v", err)
	}
	nodes = app.ListProjectTree()
	if nodes[0].Children[0].Pinned || nodes[0].Children[1].Pinned {
		t.Fatalf("unpin should clear pinned flags: %#v", nodes[0].Children)
	}
}

func TestSetProjectPinnedOrdersProjectFolders(t *testing.T) {
	isolateDesktopUserDirs(t)

	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()
	if err := addProject(first, "First"); err != nil {
		t.Fatalf("add first project: %v", err)
	}
	if err := addProject(second, "Second"); err != nil {
		t.Fatalf("add second project: %v", err)
	}
	if err := addProject(third, "Third"); err != nil {
		t.Fatalf("add third project: %v", err)
	}

	app := NewApp()
	if err := app.ReorderProjects([]string{third, first, second}); err != nil {
		t.Fatalf("ReorderProjects: %v", err)
	}
	if err := app.SetProjectPinned(second, true); err != nil {
		t.Fatalf("pin project: %v", err)
	}
	nodes := app.ListProjectTree()
	if got := []string{nodes[0].Root, nodes[1].Root, nodes[2].Root}; got[0] != second || got[1] != third || got[2] != first {
		t.Fatalf("pinned project order = %v, want %v", got, []string{second, third, first})
	}
	if !nodes[0].Pinned {
		t.Fatalf("pinned project should expose pinned=true")
	}

	if err := app.SetProjectPinned(second, false); err != nil {
		t.Fatalf("unpin project: %v", err)
	}
	nodes = app.ListProjectTree()
	if got := []string{nodes[0].Root, nodes[1].Root, nodes[2].Root}; got[0] != third || got[1] != first || got[2] != second {
		t.Fatalf("unpinned project order = %v, want %v", got, []string{third, first, second})
	}
	if nodes[0].Pinned || nodes[1].Pinned || nodes[2].Pinned {
		t.Fatalf("unpin should clear pinned flags: %#v", nodes)
	}
}

func TestDeleteTopicClearsPinnedTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, "topic_pinned_delete", "Pinned"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	app := NewApp()
	if err := app.SetTopicPinned("topic_pinned_delete", true); err != nil {
		t.Fatalf("pin topic: %v", err)
	}
	if err := app.DeleteTopic("topic_pinned_delete"); err != nil {
		t.Fatalf("delete topic: %v", err)
	}
	projects := loadProjectsFile().Projects
	if len(projects) != 1 {
		t.Fatalf("projects len = %d, want 1", len(projects))
	}
	if got := projects[0].PinnedTopics; len(got) != 0 {
		t.Fatalf("pinned topics after delete = %v, want empty", got)
	}
}

func TestRenameProjectUpdatesSidebarTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := NewApp().RenameProject(projectRoot, "Client API"); err != nil {
		t.Fatalf("rename project: %v", err)
	}

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 {
		t.Fatalf("project tree len = %d, want 1", len(nodes))
	}
	if got := nodes[0].Label; got != "Client API" {
		t.Fatalf("project label = %q, want Client API", got)
	}

	if err := NewApp().RenameProject(projectRoot, ""); err != nil {
		t.Fatalf("clear project title: %v", err)
	}
	nodes = NewApp().ListProjectTree()
	if got, want := nodes[0].Label, filepath.Base(projectRoot); got != want {
		t.Fatalf("cleared project label = %q, want %q", got, want)
	}
}

func TestListWorkspacesUsesProjectRegistryTitles(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Client API"); err != nil {
		t.Fatalf("add project: %v", err)
	}

	workspaces := NewApp().ListWorkspaces()
	if len(workspaces) != 1 {
		t.Fatalf("workspaces len = %d, want 1: %+v", len(workspaces), workspaces)
	}
	if got := workspaces[0].Path; got != projectRoot {
		t.Fatalf("workspace path = %q, want %q", got, projectRoot)
	}
	if got := workspaces[0].Name; got != "Client API" {
		t.Fatalf("workspace name = %q, want Client API", got)
	}
}

func TestListWorkspacesMigratesLegacyWorkspaceList(t *testing.T) {
	isolateDesktopUserDirs(t)

	legacyRoot := t.TempDir()
	rememberWorkspace(legacyRoot)

	workspaces := NewApp().ListWorkspaces()
	if len(workspaces) != 1 {
		t.Fatalf("workspaces len = %d, want 1: %+v", len(workspaces), workspaces)
	}
	if got := workspaces[0].Path; got != legacyRoot {
		t.Fatalf("workspace path = %q, want %q", got, legacyRoot)
	}
	projects := loadProjectsFile().Projects
	if len(projects) != 1 || projects[0].Root != legacyRoot {
		t.Fatalf("legacy workspace was not migrated into projects: %+v", projects)
	}
}

func TestLegacySessionsMigrateIntoGlobalTopics(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	older := writeLegacySession(t, dir, "older.jsonl", "older imported prompt", time.Now().Add(-2*time.Hour))
	newer := writeLegacySession(t, dir, "newer.jsonl", "newer imported prompt", time.Now().Add(-time.Hour))

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" {
		t.Fatalf("project tree = %#v, want global folder", nodes)
	}
	if got := len(nodes[0].Children); got != 2 {
		t.Fatalf("global migrated topics = %d, want 2: %#v", got, nodes[0].Children)
	}
	if got, want := nodes[0].Children[0].TopicID, legacySessionTopicID(newer); got != want {
		t.Fatalf("newest topic first = %q, want %q", got, want)
	}
	if got, want := nodes[0].Children[1].TopicID, legacySessionTopicID(older); got != want {
		t.Fatalf("older topic second = %q, want %q", got, want)
	}

	meta, ok, err := agent.LoadBranchMeta(newer)
	if err != nil || !ok {
		t.Fatalf("load migrated meta: ok=%v err=%v", ok, err)
	}
	if meta.Scope != "global" || meta.WorkspaceRoot != "" || meta.TopicID != legacySessionTopicID(newer) {
		t.Fatalf("migrated meta = %+v", meta)
	}

	nodes = NewApp().ListProjectTree()
	if got := len(nodes[0].Children); got != 2 {
		t.Fatalf("migration should be idempotent, global topics = %d", got)
	}
}

func TestLegacyRecoverySessionsDoNotMigrateIntoTopics(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	normal := writeLegacySession(t, dir, "normal.jsonl", "normal imported prompt", time.Now().Add(-2*time.Hour))
	recovery := writeLegacySession(t, dir, "normal-recovery-0123456789abcdef.jsonl", "legacy recovery prompt", time.Now().Add(-time.Hour))

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" {
		t.Fatalf("project tree = %#v, want global folder", nodes)
	}
	if got := len(nodes[0].Children); got != 1 {
		t.Fatalf("global migrated topics = %d, want only the normal session: %#v", got, nodes[0].Children)
	}
	if got, want := nodes[0].Children[0].TopicID, legacySessionTopicID(normal); got != want {
		t.Fatalf("migrated topic = %q, want normal session topic %q", got, want)
	}
	if meta, ok, err := agent.LoadBranchMeta(recovery); err != nil {
		t.Fatalf("load recovery meta: %v", err)
	} else if ok && strings.TrimSpace(meta.TopicID) != "" {
		t.Fatalf("legacy recovery branch was migrated into topic %q", meta.TopicID)
	}
}

func TestHistoryMarksLegacyRecoverySessionsAsRecovered(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	recovery := writeLegacySession(t, dir, "desktop-recovery-0123456789abcdef.jsonl", "legacy recovery prompt", time.Now())
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: recovery, Label: "test"})
	defer ctrl.Close()
	app := NewApp()
	app.setTestCtrl(ctrl, "")

	sessions := app.ListSessions()
	for _, session := range sessions {
		if filepath.Clean(session.Path) != filepath.Clean(recovery) {
			continue
		}
		if !session.Recovered {
			t.Fatalf("history session recovered flag = false, want true: %+v", session)
		}
		return
	}
	t.Fatalf("history sessions = %#v, want recovery session %q", sessions, recovery)
}

func TestTrashMarksLegacyRecoverySessionsAsRecovered(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	recovery := writeLegacySession(t, dir, "desktop-recovery-0123456789abcdef.jsonl", "legacy recovery prompt", time.Now())
	if err := deleteSessionFile(dir, recovery); err != nil {
		t.Fatalf("delete recovery session: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(recovery), filepath.Base(recovery))

	sessions := NewApp().ListTrashedSessions()
	if len(sessions) != 1 || filepath.Clean(sessions[0].Path) != filepath.Clean(trashPath) {
		t.Fatalf("trashed sessions = %#v, want %q", sessions, trashPath)
	}
	if !sessions[0].Recovered {
		t.Fatalf("trashed recovery session recovered flag = false, want true: %+v", sessions[0])
	}
}

func TestProjectTreeHidesAlreadyMigratedLegacyRecoveryTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	recovery := writeLegacySession(t, dir, "desktop-recovery-0123456789abcdef.jsonl", "legacy recovery prompt", time.Now().Add(-time.Hour))
	topicID := legacySessionTopicID(recovery)
	if err := agent.SaveBranchMetaPreserveUpdated(recovery, agent.BranchMeta{
		ID:         agent.BranchID(recovery),
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
		Scope:      "global",
		TopicID:    topicID,
		TopicTitle: "恢复分支",
		Turns:      1,
		Preview:    "legacy recovery prompt",
	}); err != nil {
		t.Fatalf("save migrated recovery meta: %v", err)
	}
	if err := setTopicTitle("", topicID, "恢复分支"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	if err := prependTopicsInProjectsFile("", []string{topicID}, false); err != nil {
		t.Fatalf("prepend topic: %v", err)
	}

	nodes := NewApp().ListProjectTree()
	var walk func([]ProjectNode)
	walk = func(list []ProjectNode) {
		for _, node := range list {
			if node.TopicID == topicID {
				t.Fatalf("already-migrated recovery topic should be hidden, got node %+v", node)
			}
			walk(node.Children)
		}
	}
	walk(nodes)
}

func TestTopicMigrationMarkerRescansWhenSessionFileChanges(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	writeLegacySession(t, dir, "first.jsonl", "first legacy prompt", time.Now().Add(-time.Hour))

	// First render migrates the legacy session and, with nothing deferred, stamps
	// the one-shot marker so later renders can skip the scan.
	NewApp().ListProjectTree()
	if _, err := os.Stat(filepath.Join(dir, topicMigrationMarker)); err != nil {
		t.Fatalf("expected migration marker after a complete pass: %v", err)
	}

	// A CLI-created session added after the marker invalidates the lightweight
	// gate and gets a fresh migration pass.
	time.Sleep(10 * time.Millisecond)
	second := writeLegacySession(t, dir, "second.jsonl", "second legacy prompt", time.Now())
	NewApp().ListProjectTree()
	meta, ok, err := agent.LoadBranchMeta(second)
	if err != nil {
		t.Fatalf("load second meta: %v", err)
	}
	if !ok || strings.TrimSpace(meta.TopicID) != legacySessionTopicID(second) {
		t.Fatalf("new session after marker should be migrated, got ok=%v meta=%+v", ok, meta)
	}
}

func TestProjectTreeRepairsIndexedGlobalTopicsAfterMigrationMarker(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := addProject(t.TempDir(), "Existing project"); err != nil {
		t.Fatalf("add project: %v", err)
	}

	sessionPath := writeLegacySession(t, dir, "desktop-legacy.jsonl", "who are you", time.Now().Add(-time.Hour))
	topicID := "legacy_desktop-legacy_1234"
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		ID:         agent.BranchID(sessionPath),
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
		Scope:      "global",
		TopicID:    topicID,
		TopicTitle: "你是谁",
		Turns:      1,
		Preview:    "who are you",
	}); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	markTopicMigrationDone(dir)

	repaired := migrateLegacySessionsIntoGlobalTopics(dir)
	if len(repaired) != 1 || repaired[0] != topicID {
		t.Fatalf("repaired topics = %#v, want %q", repaired, topicID)
	}

	nodes := NewApp().ListProjectTree()
	var global *ProjectNode
	for i := range nodes {
		if nodes[i].Kind == "global_folder" {
			global = &nodes[i]
			break
		}
	}
	if global == nil {
		t.Fatalf("project tree = %#v, want repaired Global folder", nodes)
	}
	if len(global.Children) != 1 || global.Children[0].TopicID != topicID || global.Children[0].Label != "你是谁" {
		t.Fatalf("global children = %#v, want repaired topic %q with preserved title", global.Children, topicID)
	}
	f := loadProjectsFile()
	if !containsDesktopString(f.GlobalTopics, topicID) {
		t.Fatalf("globalTopics = %#v, want %q", f.GlobalTopics, topicID)
	}
	if got := loadTopicTitle("", topicID); got != "你是谁" {
		t.Fatalf("global topic title = %q, want 你是谁", got)
	}
}

func TestDeletedRepairedGlobalTopicIsNotAutoRestored(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := addProject(t.TempDir(), "Existing project"); err != nil {
		t.Fatalf("add project: %v", err)
	}

	sessionPath := writeLegacySession(t, dir, "desktop-delete.jsonl", "delete repaired topic", time.Now().Add(-time.Hour))
	topicID := "legacy_desktop-delete_1234"
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		ID:         agent.BranchID(sessionPath),
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
		Scope:      "global",
		TopicID:    topicID,
		TopicTitle: "临时 Global",
		Turns:      1,
		Preview:    "delete repaired topic",
	}); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	markTopicMigrationDone(dir)
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 1 || repaired[0] != topicID {
		t.Fatalf("initial repaired topics = %#v, want %q", repaired, topicID)
	}
	if err := NewApp().DeleteTopic(topicID); err != nil {
		t.Fatalf("delete repaired topic: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	later := time.Now()
	if err := os.Chtimes(sessionPath, later, later); err != nil {
		t.Fatalf("touch session after delete: %v", err)
	}
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 0 {
		t.Fatalf("deleted repaired topic was restored: %#v", repaired)
	}
	f := loadProjectsFile()
	if containsDesktopString(f.GlobalTopics, topicID) {
		t.Fatalf("globalTopics = %#v, deleted topic %q should stay removed", f.GlobalTopics, topicID)
	}
	if got := loadTopicTitle("", topicID); got != "" {
		t.Fatalf("global topic title = %q, want deleted", got)
	}
	if !containsDesktopString(f.DeletedTopics, topicID) {
		t.Fatalf("deletedTopics = %#v, want tombstone for %q", f.DeletedTopics, topicID)
	}
}

func TestRepairRescanKeepsIndexedTopicsUntouched(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := addProject(t.TempDir(), "Existing project"); err != nil {
		t.Fatalf("add project: %v", err)
	}

	writeIndexedSession := func(name, topicID, title string, at time.Time) string {
		p := writeLegacySession(t, dir, name, "prompt "+title, at)
		if err := agent.SaveBranchMetaPreserveUpdated(p, agent.BranchMeta{
			ID:         agent.BranchID(p),
			CreatedAt:  at.Add(-time.Hour),
			UpdatedAt:  at,
			Scope:      "global",
			TopicID:    topicID,
			TopicTitle: title,
			Turns:      1,
			Preview:    "prompt " + title,
		}); err != nil {
			t.Fatalf("save meta %s: %v", name, err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
		return p
	}
	now := time.Now()
	olderPath := writeIndexedSession("older.jsonl", "legacy_older_000000000001", "旧话题", now.Add(-3*time.Hour))
	writeIndexedSession("newer.jsonl", "legacy_newer_000000000002", "新话题", now.Add(-time.Hour))
	markTopicMigrationDone(dir)

	// First pass repairs both missing topics.
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 2 {
		t.Fatalf("initial repaired topics = %#v, want 2 entries", repaired)
	}
	before := loadProjectsFile()
	projPath := filepath.Join(desktopConfigDir(), desktopProjectsFile)
	statBefore, err := os.Stat(projPath)
	if err != nil {
		t.Fatalf("stat projects file: %v", err)
	}

	// Ordinary session activity invalidates the repair marker; the rescan must
	// not reorder the indexed topics, rewrite the projects file, or report the
	// already-visible topics as repaired (the callers bind blank Global tabs
	// to repaired[0]).
	time.Sleep(10 * time.Millisecond)
	later := time.Now()
	if err := os.Chtimes(olderPath, later, later); err != nil {
		t.Fatalf("touch session: %v", err)
	}
	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 0 {
		t.Fatalf("steady-state rescan reported repairs: %#v", repaired)
	}
	after := loadProjectsFile()
	if !sameStringList(before.GlobalTopics, after.GlobalTopics) {
		t.Fatalf("rescan reordered globalTopics: before=%v after=%v", before.GlobalTopics, after.GlobalTopics)
	}
	statAfter, err := os.Stat(projPath)
	if err != nil {
		t.Fatalf("stat projects file: %v", err)
	}
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("steady-state rescan rewrote the projects file")
	}
}

func TestBatchPrependRespectsTombstoneWrittenAfterScanSnapshot(t *testing.T) {
	isolateDesktopUserDirs(t)

	topicID := "legacy_race_000000000001"
	// Simulate a DeleteTopic landing between a repair scan's DeletedTopics
	// snapshot and its batch write: the tombstone exists by the time the
	// prepend runs, so the batch must drop the topic instead of resurrecting it.
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		f.DeletedTopics = prependUniqueString(f.DeletedTopics, topicID)
		return true, nil
	}); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}
	if err := prependTopicsInProjectsFile("", []string{topicID, "legacy_live_000000000002"}, false); err != nil {
		t.Fatalf("batch prepend: %v", err)
	}
	f := loadProjectsFile()
	if containsDesktopString(f.GlobalTopics, topicID) {
		t.Fatalf("globalTopics = %#v, tombstoned topic %q must not be batch-prepended", f.GlobalTopics, topicID)
	}
	if !containsDesktopString(f.GlobalTopics, "legacy_live_000000000002") {
		t.Fatalf("globalTopics = %#v, live topic should still be prepended", f.GlobalTopics)
	}
	if !containsDesktopString(f.DeletedTopics, topicID) {
		t.Fatalf("deletedTopics = %#v, tombstone should survive a batch prepend", f.DeletedTopics)
	}

	// Intentional single-topic writes (create/restore/tab indexing) clear the
	// tombstone and bring the topic back in the same projects-file transaction.
	if err := prependTopicInProjectsFile("", topicID, false); err != nil {
		t.Fatalf("single prepend: %v", err)
	}
	f = loadProjectsFile()
	if !containsDesktopString(f.GlobalTopics, topicID) {
		t.Fatalf("globalTopics = %#v, single prepend should restore %q", f.GlobalTopics, topicID)
	}
	if containsDesktopString(f.DeletedTopics, topicID) {
		t.Fatalf("deletedTopics = %#v, single prepend should clear the tombstone", f.DeletedTopics)
	}
}

func TestTombstonedTitleOnlyTopicStaysHiddenInProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := addProject(t.TempDir(), "Existing project"); err != nil {
		t.Fatalf("add project: %v", err)
	}

	// Control: a legitimate title-only topic (not in GlobalTopics) must keep
	// rendering through the orderedTopicIDs title-map fallback.
	controlID := "topic_control_visible"
	if err := setTopicTitle("", controlID, "正常话题"); err != nil {
		t.Fatalf("set control title: %v", err)
	}
	// Race product: DeleteTopic landed, but a stale whole-map save wrote the
	// topic's title back — tombstoned, absent from GlobalTopics, title present.
	tombstonedID := "legacy_raced_000000000009"
	if err := setTopicTitle("", tombstonedID, "被删除的话题"); err != nil {
		t.Fatalf("set stale title: %v", err)
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		f.DeletedTopics = prependUniqueString(f.DeletedTopics, tombstonedID)
		return true, nil
	}); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}

	nodes := NewApp().ListProjectTree()
	var global *ProjectNode
	for i := range nodes {
		if nodes[i].Kind == "global_folder" {
			global = &nodes[i]
			break
		}
	}
	if global == nil {
		t.Fatalf("project tree = %#v, want Global folder", nodes)
	}
	seen := map[string]bool{}
	for _, c := range global.Children {
		seen[c.TopicID] = true
	}
	if seen[tombstonedID] {
		t.Fatalf("global children = %#v, tombstoned title-only topic %q must stay hidden", global.Children, tombstonedID)
	}
	if !seen[controlID] {
		t.Fatalf("global children = %#v, legitimate title-only topic %q should still render", global.Children, controlID)
	}
}

func TestRepairPassPrunesStaleTitleOfDeletedTopic(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := addProject(t.TempDir(), "Existing project"); err != nil {
		t.Fatalf("add project: %v", err)
	}

	// Stale race product on disk: tombstoned topic whose title lingers in the
	// global title map. The next repair pass that saves the whole map must
	// prune it instead of persisting it again.
	tombstonedID := "legacy_raced_000000000010"
	if err := setTopicTitle("", tombstonedID, "被删除的话题"); err != nil {
		t.Fatalf("set stale title: %v", err)
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		f.DeletedTopics = prependUniqueString(f.DeletedTopics, tombstonedID)
		return true, nil
	}); err != nil {
		t.Fatalf("seed tombstone: %v", err)
	}

	sessionPath := writeLegacySession(t, dir, "desktop-prune.jsonl", "needs repair", time.Now().Add(-time.Hour))
	repairID := "legacy_desktop-prune_1234"
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		ID:         agent.BranchID(sessionPath),
		CreatedAt:  time.Now().Add(-2 * time.Hour),
		UpdatedAt:  time.Now().Add(-time.Hour),
		Scope:      "global",
		TopicID:    repairID,
		TopicTitle: "待修复话题",
		Turns:      1,
		Preview:    "needs repair",
	}); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	markTopicMigrationDone(dir)

	if repaired := migrateLegacySessionsIntoGlobalTopics(dir); len(repaired) != 1 || repaired[0] != repairID {
		t.Fatalf("repaired topics = %#v, want %q", repaired, repairID)
	}
	if got := loadTopicTitle("", tombstonedID); got != "" {
		t.Fatalf("stale title = %q, repair save should prune the tombstoned entry", got)
	}
	if got := loadTopicTitle("", repairID); got != "待修复话题" {
		t.Fatalf("repaired title = %q, want 待修复话题", got)
	}
	f := loadProjectsFile()
	if containsDesktopString(f.GlobalTopics, tombstonedID) {
		t.Fatalf("globalTopics = %#v, tombstoned topic must stay out", f.GlobalTopics)
	}
	if !containsDesktopString(f.GlobalTopics, repairID) {
		t.Fatalf("globalTopics = %#v, want repaired topic %q", f.GlobalTopics, repairID)
	}
}

func TestTopicMigrationDefersEmptyLegacySession(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	// An empty legacy session (no user turns) is not migratable yet but could gain
	// content later, so the pass must NOT mark the dir done — otherwise the gate
	// would hide it forever.
	if err := os.WriteFile(filepath.Join(dir, "empty.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write empty session: %v", err)
	}

	NewApp().ListProjectTree()
	if _, err := os.Stat(filepath.Join(dir, topicMigrationMarker)); err == nil {
		t.Fatal("an empty legacy session must defer marking, but the dir was marked done")
	}
}

func TestV05LegacyEventSessionsImportIntoGlobalTopic(t *testing.T) {
	home := isolateDesktopUserDirs(t)

	legacyDir := filepath.Join(home, ".reasonix", "sessions")
	destDir := config.SessionDir()
	writeLegacyEventSession(t, legacyDir, "v053-chat.events.jsonl", "hello from v0.53", "hi from v0.53", time.Now().Add(-time.Hour))

	imported, err := agent.MigrateLegacySessions(legacyDir, destDir, config.ProjectSessionDir)
	if err != nil {
		t.Fatalf("migrate legacy sessions: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported legacy sessions = %d, want 1", imported)
	}
	migratedSession := filepath.Join(destDir, "v053-chat.jsonl")
	if _, err := os.Stat(migratedSession); err != nil {
		t.Fatalf("legacy v0.5 session was not imported to %s: %v", migratedSession, err)
	}

	wantTopicID := legacySessionTopicID(migratedSession)
	migratedTopics := migrateLegacySessionsIntoGlobalTopics(destDir)
	if len(migratedTopics) != 1 || migratedTopics[0] != wantTopicID {
		t.Fatalf("migrated topics = %#v, want imported v0.5 topic %q", migratedTopics, wantTopicID)
	}

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" {
		t.Fatalf("project tree = %#v, want global folder", nodes)
	}
	if len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != wantTopicID {
		t.Fatalf("global topics = %#v, want imported v0.5 topic %q", nodes[0].Children, wantTopicID)
	}
	meta, ok, err := agent.LoadBranchMeta(migratedSession)
	if err != nil || !ok {
		t.Fatalf("load imported v0.5 meta: ok=%v err=%v", ok, err)
	}
	if meta.Scope != "global" || meta.TopicID != wantTopicID {
		t.Fatalf("imported v0.5 meta = %+v", meta)
	}
}

func TestLegacySessionTopicIDsKeepNormalizedNameCollisionsDistinct(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	dotted := writeLegacySession(t, dir, "chat.1.jsonl", "dotted prompt", time.Now().Add(-2*time.Hour))
	underscored := writeLegacySession(t, dir, "chat_1.jsonl", "underscored prompt", time.Now().Add(-time.Hour))

	dottedTopic := legacySessionTopicID(dotted)
	underscoredTopic := legacySessionTopicID(underscored)
	if dottedTopic == underscoredTopic {
		t.Fatalf("normalized legacy topic IDs collided: %q", dottedTopic)
	}

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" {
		t.Fatalf("project tree = %#v, want global folder", nodes)
	}
	if got := len(nodes[0].Children); got != 2 {
		t.Fatalf("global migrated topics = %d, want 2: %#v", got, nodes[0].Children)
	}
	seen := map[string]bool{}
	for _, child := range nodes[0].Children {
		seen[child.TopicID] = true
	}
	if !seen[dottedTopic] || !seen[underscoredTopic] {
		t.Fatalf("global topics = %#v, want %q and %q", nodes[0].Children, dottedTopic, underscoredTopic)
	}
}

func TestDefaultGlobalTabGetsMigratedTopicID(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeLegacySession(t, dir, "legacy-tab.jsonl", "resume this legacy tab", time.Now().Add(-time.Hour))

	tab := &WorkspaceTab{
		ID:            "tab_legacy",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		Ready:         false,
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"tab_legacy": tab},
		tabOrder:    []string{"tab_legacy"},
		activeTabID: "tab_legacy",
	}
	app.buildTabController(tab)
	if tab.Ctrl != nil {
		defer tab.Ctrl.Close()
	}

	wantTopicID := legacySessionTopicID(sessionPath)
	if tab.TopicID != wantTopicID {
		t.Fatalf("tab topicID = %q, want %q", tab.TopicID, wantTopicID)
	}
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built")
	}
	if tab.Ctrl.SessionPath() != sessionPath {
		t.Fatalf("tab session path = %q, want %q", tab.Ctrl.SessionPath(), sessionPath)
	}
	f := loadTabsFile()
	if len(f.Tabs) != 1 || f.Tabs[0].ID != "tab_legacy" || f.Tabs[0].TopicID != wantTopicID {
		t.Fatalf("desktop tabs file = %+v, want tab id and migrated topic", f)
	}
}

func TestBuildTabControllerRestoresPinnedSessionBeforeTopicFallback(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_same"
	topicTitle := "Pinned topic"
	pinned := writeTopicSessionWithPrompt(t, dir, "long.jsonl", topicID, topicTitle, "", "full 64-turn conversation", time.Now().Add(-2*time.Hour))
	_ = writeTopicSessionWithPrompt(t, dir, "short.jsonl", topicID, topicTitle, "", "early 5-turn snapshot", time.Now().Add(time.Hour))

	app := NewApp()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), topicID, "tab_pinned")
	tab.TopicTitle = topicTitle
	tab.SessionPath = pinned
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built: %s", tab.StartupErr)
	}
	defer tab.Ctrl.Close()

	if got := filepath.Clean(tab.Ctrl.SessionPath()); got != filepath.Clean(pinned) {
		t.Fatalf("restored session path = %q, want pinned %q", got, pinned)
	}
	history := tab.Ctrl.History()
	if len(history) == 0 || history[0].Content != "full 64-turn conversation" {
		t.Fatalf("restored history = %+v, want pinned long conversation", history)
	}
	f := loadTabsFile()
	if len(f.Tabs) != 1 || filepath.Clean(f.Tabs[0].SessionPath) != filepath.Clean(pinned) {
		t.Fatalf("desktop tabs file = %+v, want pinned session path %q", f, pinned)
	}
}

func TestBuildTabControllerUsesPinnedSessionMetaWorkspace(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}

	topicID := "topic_restore_workspace"
	topicTitle := "Restore workspace"
	sessionDirA := desktopSessionDir(projectA)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	pinned := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", topicID, topicTitle, projectA, "project A prompt", time.Now())

	app := NewApp()
	tab := app.createTabEntryWithID("project", projectB, topicID, "tab_stale_workspace")
	tab.TopicTitle = topicTitle
	tab.SessionPath = pinned
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built: %s", tab.StartupErr)
	}
	defer tab.Ctrl.Close()

	if got := filepath.Clean(tab.Ctrl.SessionPath()); got != filepath.Clean(pinned) {
		t.Fatalf("restored session path = %q, want pinned %q", got, pinned)
	}
	if got := normalizeProjectRoot(tab.WorkspaceRoot); got != normalizeProjectRoot(projectA) {
		t.Fatalf("tab workspace root = %q, want project A %q", got, normalizeProjectRoot(projectA))
	}
	history := tab.Ctrl.History()
	if len(history) == 0 || history[0].Content != "project A prompt" {
		t.Fatalf("restored history = %+v, want project A prompt", history)
	}
}

func TestPersistTabSessionPathUsesSessionDirOwnerBeforeSavingMeta(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}

	topicID := "topic_owner_before_meta"
	topicTitle := "Owner before meta"
	sessionDirA := desktopSessionDir(projectA)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", topicID, topicTitle, projectA, "project A prompt", time.Now())
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("load branch meta: ok=%v err=%v", ok, err)
	}
	meta.WorkspaceRoot = projectB
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, meta); err != nil {
		t.Fatalf("pollute branch meta: %v", err)
	}

	app := NewApp()
	tab := app.createTabEntryWithID("project", projectB, topicID, "tab_stale_workspace")
	tab.TopicTitle = topicTitle
	tab.SessionPath = sessionPath
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.persistTabSessionPath(tab, sessionPath)

	if got := normalizeProjectRoot(tab.WorkspaceRoot); got != normalizeProjectRoot(projectA) {
		t.Fatalf("tab workspace root = %q, want project A %q", got, normalizeProjectRoot(projectA))
	}
	gotMeta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("reload branch meta: ok=%v err=%v", ok, err)
	}
	if gotMeta.Scope != "project" || normalizeProjectRoot(gotMeta.WorkspaceRoot) != normalizeProjectRoot(projectA) {
		t.Fatalf("saved branch meta scope/root = %q/%q, want project/%q", gotMeta.Scope, gotMeta.WorkspaceRoot, normalizeProjectRoot(projectA))
	}
}

func TestBuildTabControllerIgnoresStaleSessionModelWhenTabModelResolves(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("REASONIX_TEST_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "default-provider/default-model"

[[providers]]
name = "default-provider"
kind = "openai"
base_url = "https://default.invalid/v1"
model = "default-model"
api_key_env = "REASONIX_TEST_KEY"

[[providers]]
name = "tab-provider"
kind = "openai"
base_url = "https://tab.invalid/v1"
model = "tab-model"
api_key_env = "REASONIX_TEST_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	pinned := writeLegacySession(t, dir, "stale-model.jsonl", "resume with tab model", time.Now())
	meta, err := agent.EnsureBranchMeta(pinned)
	if err != nil {
		t.Fatal(err)
	}
	meta.Model = "missing-provider/missing-model"
	if err := agent.SaveBranchMetaPreserveUpdated(pinned, meta); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "tab_stale_model")
	tab.SessionPath = pinned
	tab.model = "tab-provider/tab-model"
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built: %s", tab.StartupErr)
	}
	defer tab.Ctrl.Close()
	if tab.model != "tab-provider/tab-model" {
		t.Fatalf("tab model = %q, want valid tab model", tab.model)
	}
}

func TestLoadPinnedTabSessionFallsBackToMigratedBasename(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := writeLegacySession(t, dir, "migrated-tab.jsonl", "resume after path migration", time.Now())
	oldPath := filepath.Join(t.TempDir(), "old-reasonix", "projects", "slug", "sessions", filepath.Base(path))

	loaded, pinnedPath, ok := loadPinnedTabSession(dir, oldPath)
	if !ok || loaded == nil {
		t.Fatalf("loadPinnedTabSession did not recover migrated basename: ok=%v loaded=%v path=%q", ok, loaded, pinnedPath)
	}
	if filepath.Clean(pinnedPath) != filepath.Clean(path) {
		t.Fatalf("pinned path = %q, want %q", pinnedPath, path)
	}
}

func TestPinnedTabSessionPathRejectsExistingAbsolutePathOutsideDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	dirA := t.TempDir()
	dirB := t.TempDir()
	pathA := writeLegacySession(t, dirA, "same-name.jsonl", "project A", time.Now())
	_ = writeLegacySession(t, dirB, filepath.Base(pathA), "project B", time.Now())

	if got, ok := pinnedTabSessionPath(dirB, pathA); ok {
		t.Fatalf("pinnedTabSessionPath mapped existing absolute path outside dir to %q", got)
	}
}

func TestLoadPinnedTabSessionSkipsCleanupPending(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := writeLegacySession(t, dir, "pending-pinned.jsonl", "pending pinned", time.Now())
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	if loaded, pinnedPath, ok := loadPinnedTabSession(dir, path); ok || loaded != nil || pinnedPath != "" {
		t.Fatalf("loadPinnedTabSession cleanup-pending = loaded:%v path:%q ok:%v, want skipped", loaded, pinnedPath, ok)
	}
}

func TestBuildTabControllerSkipsCleanupPendingPinnedSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	pending := writeLegacySession(t, dir, "pending-startup.jsonl", "pending startup", time.Now())
	if err := agent.MarkCleanupPending(pending, "delete"); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), "", "tab_pending")
	tab.SessionPath = pending
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built: %s", tab.StartupErr)
	}
	defer tab.Ctrl.Close()

	if got := filepath.Clean(tab.Ctrl.SessionPath()); got == filepath.Clean(pending) {
		t.Fatalf("startup bound cleanup-pending pinned session path %q", got)
	}
	for _, msg := range tab.Ctrl.History() {
		if msg.Content == "pending startup" {
			t.Fatalf("startup loaded cleanup-pending history: %+v", tab.Ctrl.History())
		}
	}
}

func TestBuildTabControllerKeepsMissingPinnedSessionPath(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_empty"
	topicTitle := "Empty pinned topic"
	_ = writeTopicSessionWithPrompt(t, dir, "old.jsonl", topicID, topicTitle, "", "old topic history", time.Now())
	pinned := filepath.Join(dir, "empty-new.jsonl")

	app := NewApp()
	tab := app.createTabEntryWithID("global", globalTabWorkspaceRoot(), topicID, "tab_empty")
	tab.TopicTitle = topicTitle
	tab.SessionPath = pinned
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	app.buildTabController(tab)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built: %s", tab.StartupErr)
	}
	defer tab.Ctrl.Close()

	if got := filepath.Clean(tab.Ctrl.SessionPath()); got != filepath.Clean(pinned) {
		t.Fatalf("empty pinned session path = %q, want %q", got, pinned)
	}
	for _, msg := range tab.Ctrl.History() {
		if msg.Content == "old topic history" {
			t.Fatalf("empty pinned session loaded fallback topic history: %+v", tab.Ctrl.History())
		}
	}
}

func TestReorderProjectsPersistsSidebarAndWorkspaceOrder(t *testing.T) {
	isolateDesktopUserDirs(t)

	first := t.TempDir()
	second := t.TempDir()
	third := t.TempDir()
	if err := addProject(first, "First"); err != nil {
		t.Fatalf("add first project: %v", err)
	}
	if err := addProject(second, "Second"); err != nil {
		t.Fatalf("add second project: %v", err)
	}
	if err := addProject(third, "Third"); err != nil {
		t.Fatalf("add third project: %v", err)
	}

	app := NewApp()
	if err := app.ReorderProjects([]string{third, first, second}); err != nil {
		t.Fatalf("ReorderProjects: %v", err)
	}

	nodes := app.ListProjectTree()
	if len(nodes) != 3 {
		t.Fatalf("project tree len = %d, want 3: %+v", len(nodes), nodes)
	}
	if got := []string{nodes[0].Root, nodes[1].Root, nodes[2].Root}; got[0] != third || got[1] != first || got[2] != second {
		t.Fatalf("project tree order = %v, want %v", got, []string{third, first, second})
	}
	workspaces := app.ListWorkspaces()
	if len(workspaces) != 3 {
		t.Fatalf("workspaces len = %d, want 3: %+v", len(workspaces), workspaces)
	}
	if got := []string{workspaces[0].Path, workspaces[1].Path, workspaces[2].Path}; got[0] != third || got[1] != first || got[2] != second {
		t.Fatalf("workspace order = %v, want %v", got, []string{third, first, second})
	}
}

func TestReorderProjectsPersistsGlobalSidebarOrder(t *testing.T) {
	isolateDesktopUserDirs(t)

	first := t.TempDir()
	second := t.TempDir()
	if err := addProject(first, "First"); err != nil {
		t.Fatalf("add first project: %v", err)
	}
	if err := addProject(second, "Second"); err != nil {
		t.Fatalf("add second project: %v", err)
	}

	app := NewApp()
	if _, err := app.CreateTopic("global", "", "Global note"); err != nil {
		t.Fatalf("create global topic: %v", err)
	}
	if err := app.ReorderProjects([]string{second, desktopGlobalOrderToken, first}); err != nil {
		t.Fatalf("ReorderProjects with global: %v", err)
	}

	nodes := app.ListProjectTree()
	if len(nodes) != 3 {
		t.Fatalf("project tree len = %d, want 3: %+v", len(nodes), nodes)
	}
	if got := []string{nodes[0].Root, nodes[1].Kind, nodes[2].Root}; got[0] != second || got[1] != "global_folder" || got[2] != first {
		t.Fatalf("project tree order = %v, want [%s global_folder %s]", got, second, first)
	}
	workspaces := app.ListWorkspaces()
	if len(workspaces) != 2 {
		t.Fatalf("workspaces len = %d, want 2: %+v", len(workspaces), workspaces)
	}
	if got := []string{workspaces[0].Path, workspaces[1].Path}; got[0] != second || got[1] != first {
		t.Fatalf("workspace order = %v, want %v", got, []string{second, first})
	}
}

func TestReorderProjectsRejectsInvalidOrder(t *testing.T) {
	isolateDesktopUserDirs(t)

	first := t.TempDir()
	second := t.TempDir()
	if err := addProject(first, "First"); err != nil {
		t.Fatalf("add first project: %v", err)
	}
	if err := addProject(second, "Second"); err != nil {
		t.Fatalf("add second project: %v", err)
	}
	app := NewApp()
	for name, order := range map[string][]string{
		"missing":          {first},
		"unknown":          {first, filepath.Join(t.TempDir(), "missing")},
		"duplicate":        {first, first},
		"duplicate-global": {desktopGlobalOrderToken, first, desktopGlobalOrderToken, second},
	} {
		t.Run(name, func(t *testing.T) {
			if err := app.ReorderProjects(order); err == nil {
				t.Fatalf("ReorderProjects(%v) succeeded, want error", order)
			}
		})
	}

	nodes := app.ListProjectTree()
	if got := []string{nodes[0].Root, nodes[1].Root}; got[0] != first || got[1] != second {
		t.Fatalf("project tree order changed after invalid reorder: %v", got)
	}
}

func TestRemoveWorkspaceUsesSharedProjectRegistryForCurrentProject(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, "Current Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	app := NewApp()
	tab := app.createTabEntryWithID("project", projectRoot, "topic_current", "tab_current")
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.RemoveWorkspace(projectRoot); err != nil {
		t.Fatalf("remove current project: %v", err)
	}
	if got := app.ListWorkspaces(); len(got) != 0 {
		t.Fatalf("workspaces after remove = %+v, want empty", got)
	}
	if got := app.ListProjectTree(); len(got) != 1 || got[0].Kind != "global_folder" || len(got[0].Children) != 0 {
		t.Fatalf("project tree after remove = %+v, want empty Global folder", got)
	}
}

func TestRestoredProjectTabUsesStoredTopicTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_stored_title"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "你是谁"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}

	app := NewApp()
	tab := app.createTabEntryWithID("project", projectRoot, topicID, "tab1")
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("tabs len = %d, want 1", len(tabs))
	}
	if got := tabs[0].TopicTitle; got != "你是谁" {
		t.Fatalf("tab title = %q, want 你是谁", got)
	}
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want one project with one topic", nodes)
	}
	if got := nodes[0].Children[0].Label; got != tabs[0].TopicTitle {
		t.Fatalf("tree title = %q, want same as tab title %q", got, tabs[0].TopicTitle)
	}
}

func TestUntitledProjectTopicUsesSameFallbackEverywhere(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_without_title"
	if err := saveProjectsFile(desktopProjectFile{Projects: []desktopProject{{
		Root:   projectRoot,
		Topics: []string{topicID},
	}}}); err != nil {
		t.Fatalf("save projects: %v", err)
	}

	app := NewApp()
	tab := app.createTabEntryWithID("project", projectRoot, topicID, "tab1")
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("tabs len = %d, want 1", len(tabs))
	}
	if got := tabs[0].TopicTitle; got != defaultTopicTitle {
		t.Fatalf("tab title = %q, want %q", got, defaultTopicTitle)
	}
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want one project with one topic", nodes)
	}
	if got := nodes[0].Children[0].Label; got != defaultTopicTitle {
		t.Fatalf("tree title = %q, want %q", got, defaultTopicTitle)
	}
}

func TestCreateTopicDefaultsToAutoNewSessionTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	before := time.Now().UnixMilli()
	topic, err := NewApp().CreateTopic("project", projectRoot, "")
	after := time.Now().UnixMilli()
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if got := topic.Title; got != defaultTopicTitle {
		t.Fatalf("topic title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != defaultTopicTitle {
		t.Fatalf("stored title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceAuto {
		t.Fatalf("title source = %q, want auto", got)
	}
	if got := loadTopicCreatedAt(projectRoot, topic.ID); got < before || got > after {
		t.Fatalf("createdAt = %d, want between %d and %d", got, before, after)
	}
	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want one project with one topic", nodes)
	}
	if got := nodes[0].Children[0].CreatedAt; got != topic.CreatedAt {
		t.Fatalf("project tree createdAt = %d, want %d", got, topic.CreatedAt)
	}
}

func TestListProjectTreeFallsBackToTopicIDCreatedAt(t *testing.T) {
	isolateDesktopUserDirs(t)

	const topicID = "legacy_20260606-114914_2276f13fd87c"
	if err := setTopicTitleWithSource("", topicID, "你好，你是谁", topicTitleSourceManual); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	if err := prependTopicInProjectsFile("", topicID, false); err != nil {
		t.Fatalf("prepend topic: %v", err)
	}

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" || len(nodes[0].Children) != 1 {
		t.Fatalf("project tree = %#v, want Global with one topic", nodes)
	}
	expected := time.Date(2026, 6, 6, 11, 49, 14, 0, time.UTC).UnixMilli()
	if got := nodes[0].Children[0].CreatedAt; got != expected {
		t.Fatalf("project tree createdAt = %d, want %d", got, expected)
	}
}

func TestCreateTopicAppearsFirstInProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	first, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create first topic: %v", err)
	}
	second, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create second topic: %v", err)
	}

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 2 {
		t.Fatalf("project tree = %#v, want one project with two topics", nodes)
	}
	if got := nodes[0].Children[0].TopicID; got != second.ID {
		t.Fatalf("first visible topic = %q, want newest %q", got, second.ID)
	}
	if got := nodes[0].Children[1].TopicID; got != first.ID {
		t.Fatalf("second visible topic = %q, want older %q", got, first.ID)
	}
}

func TestCreateGlobalTopicAppearsFirstInProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	first, err := app.CreateTopic("global", "", "")
	if err != nil {
		t.Fatalf("create first global topic: %v", err)
	}
	second, err := app.CreateTopic("global", "", "")
	if err != nil {
		t.Fatalf("create second global topic: %v", err)
	}

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" || len(nodes[0].Children) != 2 {
		t.Fatalf("project tree = %#v, want Global with two topics", nodes)
	}
	if got := nodes[0].Children[0].TopicID; got != second.ID {
		t.Fatalf("first visible global topic = %q, want newest %q", got, second.ID)
	}
	if got := nodes[0].Children[1].TopicID; got != first.ID {
		t.Fatalf("second visible global topic = %q, want older %q", got, first.ID)
	}
}

func TestListProjectTreeShowsEmptyGlobalWhenNoProjects(t *testing.T) {
	isolateDesktopUserDirs(t)

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 {
		t.Fatalf("project tree = %#v, want one Global folder", nodes)
	}
	if nodes[0].Kind != "global_folder" || nodes[0].Label != "Global" || len(nodes[0].Children) != 0 {
		t.Fatalf("project tree = %#v, want empty Global folder", nodes)
	}
}

func TestSwitchWorkspaceRegistersDefaultTopicInProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	if got, err := app.SwitchWorkspace(projectRoot); err != nil {
		t.Fatalf("SwitchWorkspace: %v", err)
	} else if got != projectRoot {
		t.Fatalf("SwitchWorkspace root = %q, want %q", got, projectRoot)
	}

	nodes := app.ListProjectTree()
	if len(nodes) != 1 {
		t.Fatalf("project tree len = %d, want 1: %+v", len(nodes), nodes)
	}
	if got := nodes[0].Root; got != projectRoot {
		t.Fatalf("project root = %q, want %q", got, projectRoot)
	}
	if len(nodes[0].Children) != 1 {
		t.Fatalf("project children len = %d, want 1: %+v", len(nodes[0].Children), nodes[0].Children)
	}
	child := nodes[0].Children[0]
	if got := child.Label; got != defaultTopicTitle {
		t.Fatalf("default topic label = %q, want %q", got, defaultTopicTitle)
	}
	if strings.TrimSpace(child.TopicID) == "" {
		t.Fatalf("default topic ID should be persisted in the project tree: %+v", child)
	}
	tabs := app.ListTabs()
	if len(tabs) != 1 || tabs[0].TopicID != child.TopicID {
		t.Fatalf("opened tab should use the persisted topic, tabs=%+v child=%+v", tabs, child)
	}
}

func TestRenameTopicLocksTitleManual(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := app.RenameTopic(topic.ID, "手动标题"); err != nil {
		t.Fatalf("rename topic: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "手动标题" {
		t.Fatalf("stored title = %q, want 手动标题", got)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("title source = %q, want manual", got)
	}
}

func TestRenameTopicUpdatesOpenTabMeta(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "旧标题")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	tab, err := app.OpenProjectTab(projectRoot, topic.ID)
	if err != nil {
		t.Fatalf("open project tab: %v", err)
	}
	waitForTabReady(t, app, tab.ID)
	if tab.TopicTitle != "旧标题" {
		t.Fatalf("opened tab title = %q, want 旧标题", tab.TopicTitle)
	}

	if err := app.RenameTopic(topic.ID, "新标题"); err != nil {
		t.Fatalf("rename topic: %v", err)
	}
	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("tabs len = %d, want 1: %+v", len(tabs), tabs)
	}
	if got := tabs[0].TopicTitle; got != "新标题" {
		t.Fatalf("open tab title = %q, want 新标题", got)
	}
}

func TestRenameTopicRecreatesDeletedProjectTitleIndexFromOpenTab(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "旧标题")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	tab, err := app.OpenProjectTab(projectRoot, topic.ID)
	if err != nil {
		t.Fatalf("open project tab: %v", err)
	}
	waitForTabReady(t, app, tab.ID)
	if err := os.Remove(topicTitlesPath(projectRoot)); err != nil {
		t.Fatalf("remove topic titles: %v", err)
	}
	if err := os.Remove(topicTitleSourcesPath(projectRoot)); err != nil {
		t.Fatalf("remove topic title sources: %v", err)
	}

	if err := app.RenameTopic(topic.ID, "恢复标题"); err != nil {
		t.Fatalf("rename topic after deleting title index: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "恢复标题" {
		t.Fatalf("restored topic title = %q, want 恢复标题", got)
	}
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topic.ID {
		t.Fatalf("project tree should still contain topic, got %#v", nodes)
	}
}

func TestRenameTopicRecreatesDeletedProjectTitleIndexFromSessionMeta(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_missing_index"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "旧标题"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	writeTopicSession(t, dir, "missing-index.jsonl", topicID, "旧标题", projectRoot)
	if err := os.Remove(topicTitlesPath(projectRoot)); err != nil {
		t.Fatalf("remove topic titles: %v", err)
	}
	if err := os.Remove(topicTitleSourcesPath(projectRoot)); err != nil {
		t.Fatalf("remove topic title sources: %v", err)
	}

	if err := NewApp().RenameTopic(topicID, "恢复标题"); err != nil {
		t.Fatalf("rename topic from session meta after deleting title index: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "恢复标题" {
		t.Fatalf("restored topic title = %q, want 恢复标题", got)
	}
	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topicID {
		t.Fatalf("project tree should contain restored topic, got %#v", nodes)
	}
}

func TestOpenProjectTabRecoversMissingTopicTitleFromSessionMeta(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := robustTempDir(t)
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "旧标题")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "stored-meta.jsonl", topic.ID, "用户保存标题", projectRoot, "first prompt should not win", time.Now())
	if err := os.Remove(topicTitlesPath(projectRoot)); err != nil {
		t.Fatalf("remove topic titles: %v", err)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("precondition title source = %q, want manual", got)
	}

	meta, err := app.OpenProjectTab(projectRoot, topic.ID)
	if err != nil {
		t.Fatalf("open project tab: %v", err)
	}
	tab := waitForTabReady(t, app, meta.ID)
	if got := filepath.Clean(tab.Ctrl.SessionPath()); got != filepath.Clean(sessionPath) {
		t.Fatalf("opened session path = %q, want %q", got, sessionPath)
	}
	if got := meta.TopicTitle; got != "用户保存标题" {
		t.Fatalf("opened topic title = %q, want 用户保存标题", got)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "用户保存标题" {
		t.Fatalf("stored topic title = %q, want 用户保存标题", got)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("title source = %q, want manual", got)
	}
}

func TestOpenProjectTabRecoversMissingTopicTitleFromSessionTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := robustTempDir(t)
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "旧标题")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "stored-session-title.jsonl", topic.ID, "", projectRoot, "first prompt should not win", time.Now())
	if err := setSessionTitle(dir, sessionPath, "历史手动标题"); err != nil {
		t.Fatalf("set session title: %v", err)
	}
	if err := os.Remove(topicTitlesPath(projectRoot)); err != nil {
		t.Fatalf("remove topic titles: %v", err)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("precondition title source = %q, want manual", got)
	}

	meta, err := app.OpenProjectTab(projectRoot, topic.ID)
	if err != nil {
		t.Fatalf("open project tab: %v", err)
	}
	waitForTabReady(t, app, meta.ID)
	if got := meta.TopicTitle; got != "历史手动标题" {
		t.Fatalf("opened topic title = %q, want 历史手动标题", got)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "历史手动标题" {
		t.Fatalf("stored topic title = %q, want 历史手动标题", got)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("title source = %q, want manual", got)
	}
}

func TestOpenProjectTabPreservesManualDefaultTopicTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := robustTempDir(t)
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := app.RenameTopic(topic.ID, defaultTopicTitle); err != nil {
		t.Fatalf("rename topic: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	writeTopicSessionWithPrompt(t, dir, "manual-default.jsonl", topic.ID, defaultTopicTitle, projectRoot, "first prompt should not replace manual default", time.Now())

	meta, err := app.OpenProjectTab(projectRoot, topic.ID)
	if err != nil {
		t.Fatalf("open project tab: %v", err)
	}
	waitForTabReady(t, app, meta.ID)
	if got := meta.TopicTitle; got != defaultTopicTitle {
		t.Fatalf("opened topic title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != defaultTopicTitle {
		t.Fatalf("stored topic title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("title source = %q, want manual", got)
	}
}

func TestEnsureTopicIndexedPreservesGlobalAutoTitleSource(t *testing.T) {
	isolateDesktopUserDirs(t)

	topicID := "topic_global_auto"
	if err := setTopicTitleWithSource("", topicID, defaultTopicTitle, topicTitleSourceAuto); err != nil {
		t.Fatalf("set global topic title: %v", err)
	}
	source := loadTopicTitleSource(topicTitleRoot("global", globalTabWorkspaceRoot()), topicID)
	if err := ensureTopicIndexed("global", globalTabWorkspaceRoot(), topicID, defaultTopicTitle, source); err != nil {
		t.Fatalf("ensure global topic indexed: %v", err)
	}

	if got := loadTopicTitleSource("", topicID); got != topicTitleSourceAuto {
		t.Fatalf("global title source = %q, want %q", got, topicTitleSourceAuto)
	}
}

func TestAutoTitleTopicFromFirstUserMessage(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topic, err := NewApp().CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"讲讲这个代码库的架构"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	title, updated := autoTitleTopicFromSession(projectRoot, topic.ID, sessionPath)
	if !updated {
		t.Fatal("auto title should update")
	}
	if title != "讲讲这个代码库的架构" {
		t.Fatalf("generated title = %q", title)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != title {
		t.Fatalf("stored title = %q, want %q", got, title)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceAuto {
		t.Fatalf("title source = %q, want auto", got)
	}
}

func TestAutoTitleTopicStripsReasoningLanguagePrefix(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topic, err := NewApp().CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	prompt := control.New(control.Options{ReasoningLanguage: "zh"}).Compose("讲讲这个代码库的架构")
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":`+strconv.Quote(prompt)+`}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	title, updated := autoTitleTopicFromSession(projectRoot, topic.ID, sessionPath)
	if !updated {
		t.Fatal("auto title should update")
	}
	if title != "讲讲这个代码库的架构" {
		t.Fatalf("generated title = %q", title)
	}
}

func TestAutoTitleTopicRefreshesOnThirdUserTurn(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topic, err := NewApp().CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	firstTurn := strings.Join([]string{
		`{"role":"user","content":"帮我看看"}`,
		`{"role":"assistant","content":"可以"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(sessionPath, []byte(firstTurn), 0o644); err != nil {
		t.Fatalf("write first session: %v", err)
	}

	title, updated := autoTitleTopicFromSession(projectRoot, topic.ID, sessionPath)
	if !updated || title != "帮我看看" {
		t.Fatalf("first auto title = %q updated=%v, want 帮我看看/true", title, updated)
	}

	thirdTurn := strings.Join([]string{
		`{"role":"user","content":"帮我看看"}`,
		`{"role":"assistant","content":"可以"}`,
		`{"role":"user","content":"继续"}`,
		`{"role":"assistant","content":"继续分析"}`,
		`{"role":"user","content":"实现自动更新会话标题"}`,
		`{"role":"assistant","content":"已实现"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(sessionPath, []byte(thirdTurn), 0o644); err != nil {
		t.Fatalf("write third session: %v", err)
	}

	title, updated = autoTitleTopicFromSession(projectRoot, topic.ID, sessionPath)
	if !updated || title != "实现自动更新会话标题" {
		t.Fatalf("third-turn auto title = %q updated=%v, want 实现自动更新会话标题/true", title, updated)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "实现自动更新会话标题" {
		t.Fatalf("stored title = %q, want 实现自动更新会话标题", got)
	}
	meta := loadTopicAutoTitleMeta(projectRoot)[topic.ID]
	if meta.Stage != 3 {
		t.Fatalf("auto title stage = %d, want 3", meta.Stage)
	}
}

func TestAutoTitleDoesNotOverrideManualTopicTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := app.RenameTopic(topic.ID, "手动标题"); err != nil {
		t.Fatalf("rename topic: %v", err)
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"讲讲这个代码库的架构"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	if title, updated := autoTitleTopicFromSession(projectRoot, topic.ID, sessionPath); updated || title != "" {
		t.Fatalf("manual title should not auto-update, title=%q updated=%v", title, updated)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != "手动标题" {
		t.Fatalf("stored title = %q, want 手动标题", got)
	}
}

func TestAutoTitleDoesNotOverrideManualSessionTitle(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topic, err := NewApp().CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"讲讲这个代码库的架构"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{CustomTitle: "手动会话标题"}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}

	if title, updated := autoTitleTopicFromSession(projectRoot, topic.ID, sessionPath); updated || title != "" {
		t.Fatalf("manual session title should not auto-update, title=%q updated=%v", title, updated)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != defaultTopicTitle {
		t.Fatalf("stored title = %q, want default title", got)
	}
}

func TestRenameTopicBlankKeepsManualTitleSource(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	if err := app.RenameTopic(topic.ID, "   "); err != nil {
		t.Fatalf("rename blank topic: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topic.ID); got != defaultTopicTitle {
		t.Fatalf("stored title = %q, want %q", got, defaultTopicTitle)
	}
	if got := loadTopicTitleSource(projectRoot, topic.ID); got != topicTitleSourceManual {
		t.Fatalf("title source = %q, want manual", got)
	}
}

func TestTrashTopicMovesRelatedSessionsToTrash(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_trash_history"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Trash history"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "trash-me.jsonl", topicID, "Trash history", projectRoot)
	placeholderPath := filepath.Join(dir, "trash-placeholder-session.jsonl")
	if err := os.WriteFile(placeholderPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	now := time.Now()
	if err := agent.SaveBranchMetaPreserveUpdated(placeholderPath, agent.BranchMeta{
		CreatedAt:     now.Add(-time.Minute),
		UpdatedAt:     now,
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       topicID,
		TopicTitle:    "Trash history",
	}); err != nil {
		t.Fatalf("save placeholder branch meta: %v", err)
	}
	placeholderGoalPath := strings.TrimSuffix(placeholderPath, ".jsonl") + ".goal-state.json"
	if err := os.WriteFile(placeholderGoalPath, []byte(`{"done":true}`), 0o644); err != nil {
		t.Fatalf("write placeholder goal state: %v", err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))

	if err := NewApp().TrashTopic(topicID); err != nil {
		t.Fatalf("trash topic: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("topic session should be removed from active history, stat err = %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "trash-me.jsonl", "trash-me.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("topic session should be moved to trash: %v", err)
	}
	if _, err := os.Stat(placeholderPath); !os.IsNotExist(err) {
		t.Fatalf("placeholder session should be removed from active history, stat err = %v", err)
	}
	placeholderTrashDir := filepath.Join(dir, sessionTrashDir, "trash-placeholder-session.jsonl")
	if _, err := os.Stat(filepath.Join(placeholderTrashDir, "trash-placeholder-session.jsonl")); err != nil {
		t.Fatalf("placeholder session should be moved to trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(placeholderTrashDir, "trash-placeholder-session.jsonl.meta")); err != nil {
		t.Fatalf("placeholder meta should be moved to trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(placeholderTrashDir, "trash-placeholder-session.goal-state.json")); err != nil {
		t.Fatalf("placeholder goal state should be moved to trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, sessionTrashDir, "trash-me.jsonl", "subagents", ref+".jsonl")); err != nil {
		t.Fatalf("topic subagent should be moved to trash: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("topic title should be removed, got %q", got)
	}
}

func TestTrashTopicRemovesStaleMissingSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_missing_trash"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Missing trash"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	missingPath := filepath.Join(dir, "already-gone.jsonl")
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"stale": {
				ID:            "stale",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Missing trash",
				SessionPath:   missingPath,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"other": {ID: "other", Scope: "project", WorkspaceRoot: projectRoot, TopicID: "other", Ready: true},
		},
		tabOrder:    []string{"stale", "other"},
		activeTabID: "stale",
	}

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic should remove stale missing session: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("topic title should be removed, got %q", got)
	}
	if _, ok := app.tabs["stale"]; ok {
		t.Fatalf("stale tab should be removed")
	}
	if got := app.activeTabID; got != "other" {
		t.Fatalf("active tab = %q, want other", got)
	}
}

func TestRestoreGlobalTopicSessionReindexesProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeLegacySession(t, dir, "restore-global.jsonl", "restore global history", time.Now().Add(-time.Hour))
	topicID := legacySessionTopicID(sessionPath)
	app := NewApp()

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topicID {
		t.Fatalf("legacy session should start in Global, got %#v", nodes)
	}
	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("trash global topic: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "restore-global.jsonl", "restore-global.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("global session should be in trash: %v", err)
	}
	if got := app.ListProjectTree(); len(got) != 1 || got[0].Kind != "global_folder" || len(got[0].Children) != 0 {
		t.Fatalf("trashed global topic should leave empty Global folder, got %#v", got)
	}

	if err := app.RestoreSession(trashPath); err != nil {
		t.Fatalf("restore global session: %v", err)
	}
	if got := app.ListTrashedSessions(); len(got) != 0 {
		t.Fatalf("trash should be empty after restore, got %#v", got)
	}
	nodes = app.ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topicID {
		t.Fatalf("restored global session should reappear in Global, got %#v", nodes)
	}
}

func TestRestoreProjectTopicSessionReindexesProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_restore_project"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Project restore"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	writeTopicSession(t, dir, "restore-project.jsonl", topicID, "Project restore", projectRoot)
	app := NewApp()

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("trash project topic: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "restore-project.jsonl", "restore-project.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("project session should be in trash: %v", err)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("topic title should be removed while trashed, got %q", got)
	}

	if err := app.RestoreSession(trashPath); err != nil {
		t.Fatalf("restore project session: %v", err)
	}
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "project" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topicID {
		t.Fatalf("restored project session should reappear in project tree, got %#v", nodes)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "Project restore" {
		t.Fatalf("restored topic title = %q, want Project restore", got)
	}
}

func TestOpenProjectTabResolvesProjectSessionFromLegacyDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_legacy_project"
	topicTitle := "Legacy project topic"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, topicTitle); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSessionWithPrompt(t, dir, "legacy-project.jsonl", topicID, topicTitle, projectRoot, "legacy project prompt", time.Now())
	app := NewApp()

	nodes := app.ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "project" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topicID {
		t.Fatalf("legacy project session should appear in project tree, got %#v", nodes)
	}
	meta, err := app.OpenProjectTab(projectRoot, topicID)
	if err != nil {
		t.Fatalf("OpenProjectTab: %v", err)
	}
	tab := waitForTabReady(t, app, meta.ID)
	if tab.Ctrl == nil {
		t.Fatalf("tab controller was not built")
	}
	if got := filepath.Clean(tab.Ctrl.SessionPath()); got != filepath.Clean(sessionPath) {
		t.Fatalf("opened session path = %q, want %q", got, sessionPath)
	}
	history := tab.Ctrl.History()
	if len(history) == 0 || history[0].Content != "legacy project prompt" {
		t.Fatalf("opened history = %+v, want legacy project prompt", history)
	}
}

func TestRestoreSessionWithoutTopicMetadataFallsBackToGlobal(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeLegacySession(t, dir, "restore-orphan.jsonl", "restore orphan history", time.Now().Add(-time.Hour))
	topicID := legacySessionTopicID(sessionPath)
	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl"), Label: "test"})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()
	if err := app.DeleteSession(sessionPath); err != nil {
		t.Fatalf("delete orphan session: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "restore-orphan.jsonl", "restore-orphan.jsonl")

	if err := app.RestoreSession(trashPath); err != nil {
		t.Fatalf("restore orphan session: %v", err)
	}
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != topicID {
		t.Fatalf("restored orphan session should fall back to Global, got %#v", nodes)
	}
}

func TestTrashTopicMovesOpenSessionToTrash(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_open_trash"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Open trash"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(dir, "open-trash.jsonl")
	if err := agent.SaveBranchMeta(sessionPath, agent.BranchMeta{
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       topicID,
		TopicTitle:    "Open trash",
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	openTab := &WorkspaceTab{
		ID:            "tab_open",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       topicID,
		TopicTitle:    "Open trash",
		Ctrl:          controllerWithContent(t, sessionPath),
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	otherTab := &WorkspaceTab{
		ID:            "tab_other",
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       "topic_keep",
		TopicTitle:    "Keep",
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"tab_open": openTab, "tab_other": otherTab},
		tabOrder:    []string{"tab_open", "tab_other"},
		activeTabID: "tab_open",
	}

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("trash topic: %v", err)
	}
	if _, ok := app.tabs["tab_open"]; ok {
		t.Fatalf("open tab for trashed topic should be removed")
	}
	if got := app.activeTabID; got != "tab_other" {
		t.Fatalf("active tab = %q, want tab_other", got)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("open topic session should be removed from active history, stat err = %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "open-trash.jsonl", "open-trash.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("open topic session should be moved to trash: %v", err)
	}
	trashed := app.ListTrashedSessions()
	if len(trashed) != 1 || trashed[0].Path != trashPath {
		t.Fatalf("trashed sessions = %#v, want %q", trashed, trashPath)
	}
	preview, err := app.PreviewSession(trashPath)
	if err != nil {
		t.Fatalf("preview trashed session: %v", err)
	}
	if !hasHistoryContent(preview, "remember this turn") {
		t.Fatalf("preview trashed session = %#v, want remembered turn", preview)
	}
	if got := loadTopicTitle(projectRoot, topicID); got != "" {
		t.Fatalf("topic title should be removed, got %q", got)
	}
}

func TestTrashTopicCancelsRunningSessionRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_running_trash"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Running trash"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "running-trash.jsonl", topicID, "Running trash", projectRoot)
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: sessionPath, Label: "test", WorkspaceRoot: projectRoot})
	defer ctrl.Close()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"running": {
				ID:            "running",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Running trash",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"keep": {
				ID:            "keep",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       "topic_keep",
				TopicTitle:    "Keep",
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"running", "keep"},
		activeTabID: "running",
	}

	ctrl.Submit("long turn")
	<-runner.started
	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("trash topic: %v", err)
	}
	waitNotRunning(t, ctrl)
	if _, ok := app.tabs["running"]; ok {
		t.Fatalf("running topic runtime should be removed")
	}
	if got := app.activeTabID; got != "keep" {
		t.Fatalf("active tab = %q, want keep", got)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("running topic session should be moved out of active history, stat err = %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "running-trash.jsonl", "running-trash.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("running topic session should be moved to trash: %v", err)
	}
}

func TestTrashTopicFallbackCreatesUnindexedBlank(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_only"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Only topic"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "only-topic.jsonl", topicID, "Only topic", projectRoot)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test", WorkspaceRoot: projectRoot})
	defer ctrl.Close()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"only": {
				ID:            "only",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Only topic",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"only"},
		activeTabID: "only",
	}

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic: %v", err)
	}
	if len(app.tabs) != 1 {
		t.Fatalf("fallback should create exactly one visible tab, got %d", len(app.tabs))
	}
	for id, tab := range app.tabs {
		if strings.TrimSpace(tab.TopicID) != "" {
			t.Fatalf("fallback tab %q topic ID = %q, want transient unindexed blank", id, tab.TopicID)
		}
		if strings.TrimSpace(tab.SessionPath) == "" {
			t.Fatalf("fallback tab %q has no precreated session path", id)
		}
		f := loadProjectsFile()
		if len(f.Projects) != 1 || containsDesktopString(f.Projects[0].Topics, topicID) {
			t.Fatalf("deleted topic %q should be removed without indexing a replacement: %#v", topicID, f.Projects)
		}
	}
	nodes := app.ListProjectTree()
	if len(nodes) != 1 || len(nodes[0].Children) != 0 {
		t.Fatalf("transient fallback blank should stay out of project tree: %+v", nodes)
	}
}

func TestTransientFallbackIndexesOnFirstUserTurn(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_only"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Only topic"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "only-topic.jsonl", topicID, "Only topic", projectRoot)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test", WorkspaceRoot: projectRoot})
	defer ctrl.Close()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"only": {
				ID:            "only",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Only topic",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"only"},
		activeTabID: "only",
	}

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic: %v", err)
	}
	var fallback *WorkspaceTab
	for _, tab := range app.tabs {
		fallback = tab
	}
	if fallback == nil {
		t.Fatal("fallback tab missing")
	}
	if fallback.TopicID != "" {
		t.Fatalf("fallback topic before first turn = %q, want empty", fallback.TopicID)
	}

	app.ensureTabTopicIndexedForUserTurn(fallback)
	if strings.TrimSpace(fallback.TopicID) == "" {
		t.Fatal("first user turn should assign a topic ID")
	}
	f := loadProjectsFile()
	if len(f.Projects) != 1 || !containsDesktopString(f.Projects[0].Topics, fallback.TopicID) {
		t.Fatalf("first user turn should index fallback topic %q: %#v", fallback.TopicID, f.Projects)
	}
	meta, ok, err := agent.LoadBranchMeta(fallback.SessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta(%q): ok=%v err=%v", fallback.SessionPath, ok, err)
	}
	if meta.TopicID != fallback.TopicID || meta.TopicTitle != defaultTopicTitle {
		t.Fatalf("fallback session meta = %+v, want topic %q title %q", meta, fallback.TopicID, defaultTopicTitle)
	}
}

func TestTransientFallbackDiscardedWhenSingleSurfaceNavigatesAway(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	transientPath, err := createEmptySessionFile(dir, "test")
	if err != nil {
		t.Fatalf("create transient session: %v", err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(transientPath, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: projectRoot,
	}); err != nil {
		t.Fatalf("save transient meta: %v", err)
	}

	targetTopicID := "topic_target"
	if err := setTopicTitle(projectRoot, targetTopicID, "Target"); err != nil {
		t.Fatalf("set target topic title: %v", err)
	}
	targetPath := writeTopicSession(t, dir, "target.jsonl", targetTopicID, "Target", projectRoot)
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"transient": {
				ID:            "transient",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicTitle:    defaultTopicTitle,
				SessionPath:   transientPath,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"target": {
				ID:            "target",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       targetTopicID,
				TopicTitle:    "Target",
				SessionPath:   targetPath,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"transient", "target"},
		activeTabID: "transient",
	}

	if _, err := app.keepOnlyVisibleTab("target"); err != nil {
		t.Fatalf("keepOnlyVisibleTab: %v", err)
	}
	if _, ok := app.tabs["transient"]; ok {
		t.Fatal("transient tab should be removed after single-surface navigation")
	}
	if _, err := os.Stat(transientPath); !os.IsNotExist(err) {
		t.Fatalf("transient session artifact should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(agent.BranchMetaPath(transientPath)); !os.IsNotExist(err) {
		t.Fatalf("transient session meta should be removed, stat err = %v", err)
	}
	f := loadProjectsFile()
	if len(f.Projects) != 1 || containsDesktopString(f.Projects[0].Topics, "") {
		t.Fatalf("transient blank should not be indexed in project topics: %#v", f.Projects)
	}
}

func TestCloseTabDiscardsUnusedTransientBlankSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	dir := desktopSessionDir(projectRoot)
	transientPath, err := createEmptySessionFile(dir, "test")
	if err != nil {
		t.Fatalf("create transient session: %v", err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(transientPath, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: projectRoot,
	}); err != nil {
		t.Fatalf("save transient meta: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"transient": {
				ID:            "transient",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicTitle:    defaultTopicTitle,
				SessionPath:   transientPath,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"other": {
				ID:          "other",
				Scope:       "global",
				TopicID:     "topic_other",
				TopicTitle:  "Other",
				Ready:       true,
				disabledMCP: map[string]ServerView{},
			},
		},
		tabOrder:    []string{"transient", "other"},
		activeTabID: "transient",
	}

	if err := app.CloseTab("transient"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if _, ok := app.tabs["transient"]; ok {
		t.Fatal("transient tab should be closed")
	}
	if _, err := os.Stat(transientPath); !os.IsNotExist(err) {
		t.Fatalf("transient session artifact should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(agent.BranchMetaPath(transientPath)); !os.IsNotExist(err) {
		t.Fatalf("transient session meta should be removed, stat err = %v", err)
	}
}

func TestCloseTabKeepsIndexedBlankSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_indexed_blank"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, defaultTopicTitle); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	indexedPath, err := createEmptySessionFile(dir, "test")
	if err != nil {
		t.Fatalf("create indexed blank session: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"indexed": {
				ID:            "indexed",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    defaultTopicTitle,
				SessionPath:   indexedPath,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"other": {
				ID:          "other",
				Scope:       "global",
				TopicID:     "topic_other",
				TopicTitle:  "Other",
				Ready:       true,
				disabledMCP: map[string]ServerView{},
			},
		},
		tabOrder:    []string{"indexed", "other"},
		activeTabID: "indexed",
	}

	if err := app.CloseTab("indexed"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	if _, err := os.Stat(indexedPath); err != nil {
		t.Fatalf("indexed blank session should be preserved, stat err = %v", err)
	}
}

func TestTrashTopicTrashConflictKeepsRunningRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_trash_conflict"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Trash conflict"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "trash-conflict.jsonl", topicID, "Trash conflict", projectRoot)
	if err := os.MkdirAll(filepath.Join(dir, sessionTrashDir, filepath.Base(sessionPath)), 0o755); err != nil {
		t.Fatalf("create trash conflict: %v", err)
	}
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: sessionPath, Label: "test", WorkspaceRoot: projectRoot})
	defer ctrl.Close()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"running": {
				ID:            "running",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Trash conflict",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"running"},
		activeTabID: "running",
	}

	ctrl.Submit("long turn")
	<-runner.started
	err := app.TrashTopic(topicID)
	if err != nil {
		t.Fatalf("TrashTopic should succeed after cleaning empty trash dir: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session file should be moved to trash, stat err = %v", err)
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
}

func TestTrashTopicValidTrashRemovesEmptyLiveStub(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_valid_trash"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Valid trash"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := filepath.Join(dir, "valid-trash.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write live stub: %v", err)
	}
	if err := agent.SaveBranchMeta(sessionPath, agent.BranchMeta{
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		Scope:         "project",
		WorkspaceRoot: projectRoot,
		TopicID:       topicID,
		TopicTitle:    "Valid trash",
	}); err != nil {
		t.Fatalf("save branch meta: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(sessionPath), filepath.Base(sessionPath))
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o755); err != nil {
		t.Fatalf("create trash dir: %v", err)
	}
	if err := os.WriteFile(trashPath, []byte(`{"role":"user","content":"already trashed"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write trash session: %v", err)
	}

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"stale": {
				ID:            "stale",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Valid trash",
				SessionPath:   sessionPath,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"other": {ID: "other", Scope: "project", WorkspaceRoot: projectRoot, TopicID: "other", Ready: true},
		},
		tabOrder:    []string{"stale", "other"},
		activeTabID: "other",
	}

	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic should remove stale live stub: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("live stub should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("existing trash should remain authoritative: %v", err)
	}
}

func hasHistoryContent(messages []HistoryMessage, content string) bool {
	for _, m := range messages {
		if m.Content == content {
			return true
		}
	}
	return false
}

func TestLegacyMigrationSkipsProjectScopedSessions(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeLegacySession(t, dir, "scoped.jsonl", "hello", time.Now())
	meta, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.Scope = "project"
	meta.WorkspaceRoot = filepath.Join(t.TempDir(), "proj")
	meta.TopicID = ""
	if err := agent.SaveBranchMeta(path, meta); err != nil {
		t.Fatal(err)
	}

	migrateLegacySessionsIntoGlobalTopics(dir)

	got, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != "project" || got.WorkspaceRoot != meta.WorkspaceRoot {
		t.Fatalf("project-scoped legacy session must not be forced into Global: %+v", got)
	}
}

func TestProjectTreeMigratesCLISessionFromProjectDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	dir := config.ProjectSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := writeLegacySession(t, dir, "cli-project.jsonl", "cli project prompt", time.Now())
	wantTopicID := legacySessionTopicID(sessionPath)

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "project" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != wantTopicID {
		t.Fatalf("project CLI session should appear in project tree, got %#v; want topic %q", nodes, wantTopicID)
	}
}

func TestProjectTreeMigratesNewCLISessionAfterProjectDirMarker(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	dir := config.ProjectSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	first := writeLegacySession(t, dir, "first-cli-project.jsonl", "first cli project prompt", time.Now().Add(-time.Hour))
	firstTopicID := legacySessionTopicID(first)

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "project" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != firstTopicID {
		t.Fatalf("first project CLI session should appear in project tree, got %#v; want topic %q", nodes, firstTopicID)
	}
	if _, err := os.Stat(filepath.Join(dir, topicMigrationMarker)); err != nil {
		t.Fatalf("expected migration marker after first project pass: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	second := writeLegacySession(t, dir, "second-cli-project.jsonl", "second cli project prompt", time.Now())
	secondTopicID := legacySessionTopicID(second)

	nodes = NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "project" || len(nodes[0].Children) != 2 {
		t.Fatalf("second project CLI session should trigger re-scan, got %#v", nodes)
	}
	if nodes[0].Children[0].TopicID != secondTopicID || nodes[0].Children[1].TopicID != firstTopicID {
		t.Fatalf("project CLI topics = %#v, want newest %q then %q", nodes[0].Children, secondTopicID, firstTopicID)
	}
}

func TestProjectTreeMigratesCLISessionFromGlobalWorkspaceDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	globalRoot := globalWorkspaceRoot()
	dir := desktopSessionDir(globalRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := writeLegacySession(t, dir, "cli-global.jsonl", "cli global prompt", time.Now())
	if err := agent.SaveBranchMetaPreserveUpdated(sessionPath, agent.BranchMeta{
		CreatedAt:     time.Now().Add(-time.Minute),
		UpdatedAt:     time.Now(),
		Scope:         "global",
		WorkspaceRoot: globalRoot,
	}); err != nil {
		t.Fatal(err)
	}
	wantTopicID := legacySessionTopicID(sessionPath)

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 || nodes[0].Kind != "global_folder" || len(nodes[0].Children) != 1 || nodes[0].Children[0].TopicID != wantTopicID {
		t.Fatalf("global workspace CLI session should appear in Global, got %#v; want topic %q", nodes, wantTopicID)
	}
}

func TestLegacyMigrationConcurrentRunsHaveNoLostUpdates(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const n = 8
	want := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		p := writeLegacySession(t, dir, fmt.Sprintf("legacy-%d.jsonl", i), "hi", time.Now())
		want[legacySessionTopicID(p)] = true
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			migrateLegacySessionsIntoGlobalTopics(dir)
		}()
	}
	wg.Wait()

	gotSet := map[string]bool{}
	for _, id := range loadProjectsFile().GlobalTopics {
		gotSet[id] = true
	}
	for id := range want {
		if !gotSet[id] {
			t.Fatalf("concurrent migration lost topic %q; GlobalTopics=%v", id, loadProjectsFile().GlobalTopics)
		}
	}
}

func TestFindTopicSessionIndexRefreshesWhenMetaChanges(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	topicID := "topic_cache_refresh"
	now := time.Now().UTC()
	first := writeTopicSessionWithPrompt(t, dir, "first.jsonl", topicID, "First", "", "first prompt", now.Add(-time.Hour))

	if got := findTopicSession(dir, topicID); got != first {
		t.Fatalf("first lookup = %q, want %q", got, first)
	}

	second := writeTopicSessionWithPrompt(t, dir, "second.jsonl", topicID, "Second", "", "second prompt", now)
	if got := findTopicSession(dir, topicID); got != second {
		t.Fatalf("lookup after new session = %q, want newer %q", got, second)
	}

	meta, ok, err := agent.LoadBranchMeta(second)
	if err != nil || !ok {
		t.Fatalf("load second meta: ok=%v err=%v", ok, err)
	}
	meta.TopicID = "topic_cache_other"
	meta.UpdatedAt = now.Add(time.Hour)
	if err := agent.SaveBranchMetaPreserveUpdated(second, meta); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(agent.BranchMetaPath(second), future, future); err != nil {
		t.Fatal(err)
	}

	if got := findTopicSession(dir, topicID); got != first {
		t.Fatalf("lookup after retopic = %q, want remaining %q", got, first)
	}
	if got := findTopicSession(dir, "topic_cache_other"); got != second {
		t.Fatalf("lookup for retopic session = %q, want %q", got, second)
	}
}

func TestFindTopicSessionSkipsCleanupPending(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	topicID := "topic_skip_pending"
	now := time.Now().UTC()
	normal := writeTopicSessionWithPrompt(t, dir, "normal.jsonl", topicID, "Normal", "", "normal prompt", now)
	pending := writeTopicSessionWithPrompt(t, dir, "pending.jsonl", topicID, "Pending", "", "pending prompt", now.Add(time.Hour))

	if got := findTopicSession(dir, topicID); got != pending {
		t.Fatalf("pre-marker lookup = %q, want newest pending %q", got, pending)
	}
	if err := agent.MarkCleanupPending(pending, "delete"); err != nil {
		t.Fatal(err)
	}
	if got := findTopicSession(dir, topicID); got != normal {
		t.Fatalf("lookup with cleanup-pending newest = %q, want normal %q", got, normal)
	}
	if err := agent.MarkCleanupPending(normal, "delete"); err != nil {
		t.Fatal(err)
	}
	if got := findTopicSession(dir, topicID); got != "" {
		t.Fatalf("lookup with only cleanup-pending sessions = %q, want empty", got)
	}
}

func TestOpenProjectTabSkipsCleanupPendingTopicSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	app := NewApp()
	topic, err := app.CreateTopic("project", projectRoot, "Pending topic")
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pending := writeTopicSessionWithPrompt(t, dir, "pending-topic.jsonl", topic.ID, "Pending topic", projectRoot, "pending topic prompt", time.Now())
	if err := agent.MarkCleanupPending(pending, "delete"); err != nil {
		t.Fatal(err)
	}
	if got := findTopicSession(dir, topic.ID); got != "" {
		t.Fatalf("topic lookup with only cleanup-pending session = %q, want empty", got)
	}
	if got, _ := app.findTopicSessionForTarget("project", projectRoot, topic.ID); got != "" {
		t.Fatalf("target topic lookup with only cleanup-pending session = %q, want empty", got)
	}

	meta, err := app.OpenProjectTab(projectRoot, topic.ID)
	if err != nil {
		t.Fatalf("open project tab: %v", err)
	}
	tab := waitForTabReady(t, app, meta.ID)
	if got := filepath.Clean(tab.Ctrl.SessionPath()); got == filepath.Clean(pending) {
		t.Fatalf("opened cleanup-pending topic session path %q", got)
	}
	for _, msg := range tab.Ctrl.History() {
		if msg.Content == "pending topic prompt" {
			t.Fatalf("opened cleanup-pending topic history at path %q: %+v", tab.Ctrl.SessionPath(), tab.Ctrl.History())
		}
	}
}

func TestUpdateTopicSessionTitlesUsesTopicIndex(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	topicID := "topic_title_index"
	now := time.Now().UTC()
	valid := writeTopicSessionWithPrompt(t, dir, "valid.jsonl", topicID, "Old", "", "hello", now)
	unpreviewable := filepath.Join(dir, "unpreviewable.jsonl")
	if err := os.WriteFile(unpreviewable, []byte("not-json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(unpreviewable, agent.BranchMeta{
		CreatedAt:  now.Add(-time.Minute),
		UpdatedAt:  now,
		Scope:      "global",
		TopicID:    topicID,
		TopicTitle: "Old",
	}); err != nil {
		t.Fatal(err)
	}

	NewApp().updateTopicSessionTitles(topicID, "Renamed")

	for _, path := range []string{valid, unpreviewable} {
		meta, ok, err := agent.LoadBranchMeta(path)
		if err != nil || !ok {
			t.Fatalf("load meta for %s: ok=%v err=%v", path, ok, err)
		}
		if meta.TopicTitle != "Renamed" {
			t.Fatalf("topic title for %s = %q, want Renamed", path, meta.TopicTitle)
		}
	}
}

func TestEnsureTopicIndexedConcurrentRunsHaveNoLostProjectUpdates(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	const n = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			topicID := fmt.Sprintf("topic_recovered_%02d", i)
			if err := ensureTopicIndexed("project", projectRoot, topicID, fmt.Sprintf("Recovered %02d", i), topicTitleSourceManual); err != nil {
				t.Errorf("ensure topic indexed: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	nodes := NewApp().ListProjectTree()
	if len(nodes) != 1 {
		t.Fatalf("project tree len = %d, want 1: %#v", len(nodes), nodes)
	}
	got := map[string]bool{}
	for _, child := range nodes[0].Children {
		got[child.TopicID] = true
	}
	for i := 0; i < n; i++ {
		topicID := fmt.Sprintf("topic_recovered_%02d", i)
		if !got[topicID] {
			t.Fatalf("concurrent topic index recovery lost %q; children=%#v", topicID, nodes[0].Children)
		}
		if title := loadTopicTitle(projectRoot, topicID); title == "" {
			t.Fatalf("title index missing %q", topicID)
		}
	}
}
