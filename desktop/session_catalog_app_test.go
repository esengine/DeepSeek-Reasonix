package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/history"
	"reasonix/internal/provider"
	"reasonix/internal/sessioncatalog"
)

func installSessionCatalogForTest(t *testing.T, app *App, path, scope, workspaceRoot string) {
	t.Helper()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatalf("open in-memory session catalog: %v", err)
	}
	target := sessioncatalog.DirectoryTarget{Path: path, Scope: scope, WorkspaceRoot: workspaceRoot}
	if err := catalog.ReconcileDirectory(context.Background(), target); err != nil {
		_ = catalog.Close(context.Background())
		t.Fatalf("reconcile session catalog %q: %v", target.Path, err)
	}
	app.sessionCatalog.Store(catalog)
	t.Cleanup(func() {
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = catalog.Close(ctx)
	})
}

func reconcileSessionCatalogForTest(t *testing.T, app *App, path, scope, workspaceRoot string) {
	t.Helper()
	catalog := app.sessionCatalog.Load()
	if catalog == nil {
		t.Fatal("session catalog is not installed")
	}
	target := sessioncatalog.DirectoryTarget{Path: path, Scope: scope, WorkspaceRoot: workspaceRoot}
	if err := catalog.ReconcileDirectory(context.Background(), target); err != nil {
		t.Fatalf("reconcile session catalog %q: %v", path, err)
	}
}

func TestProjectTreeSnapshotReturnsProjectShellWithoutMigratingSessions(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Large Project"); err != nil {
		t.Fatal(err)
	}
	sessionDir := desktopSessionDir(root)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(sessionDir, "legacy.jsonl")
	if err := os.WriteFile(legacyPath, []byte(`{"role":"user","content":"legacy"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot := NewApp().GetProjectTreeSnapshot()
	if len(snapshot.Projects) != 1 || snapshot.Projects[0].Root != root {
		t.Fatalf("snapshot = %#v, want project shell %q", snapshot, root)
	}
	if snapshot.Projects[0].Children == nil {
		t.Fatal("project shell children encoded as null, want []")
	}
	if _, err := os.Stat(legacyPath + ".meta"); !os.IsNotExist(err) {
		t.Fatalf("snapshot migrated session metadata: %v", err)
	}
}

func TestProjectTreeSnapshotIncludesPinnedTopicsForCollapsedFolders(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Pinned Project"); err != nil {
		t.Fatal(err)
	}
	if err := setTopicTitle(root, "ordinary", "Ordinary chat"); err != nil {
		t.Fatal(err)
	}
	if err := setTopicTitle(root, "pinned", "Pinned chat"); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if err := app.SetTopicPinned("pinned", true); err != nil {
		t.Fatal(err)
	}

	snapshot := app.GetProjectTreeSnapshot()
	if len(snapshot.Projects) != 1 {
		t.Fatalf("project shells = %#v, want one project", snapshot.Projects)
	}
	children := snapshot.Projects[0].Children
	if len(children) != 1 || children[0].TopicID != "pinned" || !children[0].Pinned {
		t.Fatalf("collapsed project children = %#v, want only pinned topic shell", children)
	}
	if children[0].Label != "Pinned chat" {
		t.Fatalf("pinned topic label = %q, want %q", children[0].Label, "Pinned chat")
	}
}

func TestCompatibilityProjectTreeDoesNotMigrateLegacySession(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Project"); err != nil {
		t.Fatal(err)
	}
	sessionDir := desktopSessionDir(root)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(sessionDir, "legacy.jsonl")
	if err := os.WriteFile(legacyPath, []byte(`{"role":"user","content":"legacy"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_ = NewApp().ListProjectTree()
	if _, err := os.Stat(legacyPath + ".meta"); !os.IsNotExist(err) {
		t.Fatalf("ListProjectTree migrated legacy session: %v", err)
	}
}

func TestProjectTreeShellSurvivesCatalogRevisionRace(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Shell Race"); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	// Catalog not open yet: revision stays 0 while the shell still returns projects.
	snapshot := app.GetProjectTreeSnapshot()
	if snapshot.Revision != 0 {
		t.Fatalf("revision = %d, want 0 while catalog is opening", snapshot.Revision)
	}
	if len(snapshot.Projects) == 0 {
		t.Fatal("project shell empty while catalog opening")
	}
	found := false
	for _, project := range snapshot.Projects {
		if project.Root == root || project.Label == "Shell Race" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("snapshot projects = %#v, want Shell Race", snapshot.Projects)
	}
}

func TestUpgradeLegacyRecoveryChainNeedsNoUserActionForOneRowSidebar(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(path, topic string, messages ...provider.Message) {
		t.Helper()
		session := agent.NewSession("sys")
		for _, message := range messages {
			session.Add(message)
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "global", TopicID: topic,
			TopicTitle: "Upgraded conversation",
		}); err != nil {
			t.Fatal(err)
		}
	}
	q := provider.Message{Role: provider.RoleUser, Content: "question"}
	a := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
	next := provider.Message{Role: provider.RoleUser, Content: "continue"}
	done := provider.Message{Role: provider.RoleAssistant, Content: "done"}
	root := filepath.Join(dir, "root.jsonl")
	copy := filepath.Join(dir, "copy.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	save(root, "conversation", q, a)
	save(copy, "legacy-copy-topic", q, a)
	save(leaf, "legacy-leaf-topic", q, a, next, done)
	for path, meta := range map[string]agent.BranchMeta{
		copy: {ID: "copy", Scope: "global", TopicID: "legacy-copy-topic", Recovered: true, ParentID: "root", RecoveryDepth: 1},
		leaf: {ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic", Recovered: true, ParentID: "copy", RecoveryDepth: 2},
	} {
		if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
			t.Fatal(err)
		}
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")
	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "conversation" || len(page.Items[0].Children) != 0 {
		t.Fatalf("zero-touch upgraded sidebar = %+v, want one ordinary conversation row", page.Items)
	}
	if page.Items[0].RecoveryBranchCount != 0 || page.Items[0].RecoveryUnresolvedCount != 0 ||
		page.Items[0].Status == topicStatusDivergedRecovery {
		// Recovery counts and conflict status belong only to History advanced
		// views; the ordinary root row must stay a plain conversation line.
		t.Fatalf("ordinary row leaked recovery decoration: %+v", page.Items[0])
	}
}

func TestListProjectTopicsSurfacesLiveTabWhileCatalogLags(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Lagging Catalog"); err != nil {
		t.Fatal(err)
	}
	sessionDir := desktopSessionDir(root)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, sessionDir, "project", root)
	app.tabs["tab-1"] = &WorkspaceTab{
		ID: "tab-1", Scope: "project", WorkspaceRoot: root,
		TopicID: "topic_20260812-082637_live", TopicTitle: "Ownership Hub",
	}

	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "topic_20260812-082637_live" {
		t.Fatalf("page items = %#v, want the live tab's topic", page.Items)
	}
	if page.Items[0].CreatedAt <= 0 {
		t.Fatalf("createdAt = %d, want the topic's creation time", page.Items[0].CreatedAt)
	}
}

func TestListProjectTopicsDoesNotDuplicateIndexedLiveTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Indexed"); err != nil {
		t.Fatal(err)
	}
	sessionDir := desktopSessionDir(root)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(sessionDir, "live.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.UpdateBranchMeta(sessionPath, false, func(meta *agent.BranchMeta) error {
		meta.TopicID = "topic-indexed"
		meta.TopicTitle = "Indexed Topic"
		meta.WorkspaceRoot = root
		meta.Scope = "project"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, sessionDir, "project", root)
	app.tabs["tab-1"] = &WorkspaceTab{
		ID: "tab-1", Scope: "project", WorkspaceRoot: root,
		TopicID: "topic-indexed", TopicTitle: "Indexed Topic", SessionPath: sessionPath,
	}

	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("page items = %#v, want the catalog row only", page.Items)
	}
}

func TestListProjectTopicsHonorsConversationSortMode(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Sortable Conversations"); err != nil {
		t.Fatal(err)
	}
	sessionDir := desktopSessionDir(root)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, sessionDir, "project", root)
	catalog := app.sessionCatalog.Load()
	if catalog == nil {
		t.Fatal("session catalog not installed")
	}
	for _, record := range []sessioncatalog.SessionRecord{
		{Path: filepath.Join(sessionDir, "created-new.jsonl"), Directory: sessionDir, Scope: "project", WorkspaceRoot: root, TopicID: "created-new", TopicTitle: "Created New", CreatedAt: 300, LastActivityAt: 100, Turns: 1, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
		{Path: filepath.Join(sessionDir, "updated-new.jsonl"), Directory: sessionDir, Scope: "project", WorkspaceRoot: root, TopicID: "updated-new", TopicTitle: "Updated New", CreatedAt: 100, LastActivityAt: 300, Turns: 1, TurnsState: sessioncatalog.TurnsValid, Health: sessioncatalog.HealthOK},
	} {
		if err := catalog.UpsertSession(context.Background(), record); err != nil {
			t.Fatal(err)
		}
	}

	created, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root, SortMode: "created"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root, SortMode: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Items) != 2 || created.Items[0].TopicID != "created-new" {
		t.Fatalf("created sort = %#v, want created-new first", created.Items)
	}
	if len(updated.Items) != 2 || updated.Items[0].TopicID != "updated-new" {
		t.Fatalf("updated sort = %#v, want updated-new first", updated.Items)
	}
}

func TestListProjectTopicsFoldsRestoredLegacyTopicTabIntoOneRow(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(path, topic string) {
		t.Helper()
		session := agent.NewSession("sys")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "global", TopicID: topic,
			TopicTitle: "Upgraded conversation",
		}); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "root.jsonl")
	copyPath := filepath.Join(dir, "copy.jsonl")
	save(root, "conversation")
	save(copyPath, "legacy-copy-topic")
	if err := agent.SaveBranchMetaPreserveUpdated(copyPath, agent.BranchMeta{
		ID: "copy", Scope: "global", TopicID: "legacy-copy-topic",
		Recovered: true, ParentID: "root", RecoveryDepth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")
	// A restored pre-upgrade tab still carries the legacy topic ID for the
	// recovery copy that the catalog re-anchored onto the root logical topic.
	// withLiveTopics must dedupe by the projected topic, not the tab's one,
	// so the sidebar keeps exactly one row for the conversation.
	app.tabs["tab-1"] = &WorkspaceTab{
		ID: "tab-1", Scope: "global",
		TopicID: "legacy-copy-topic", TopicTitle: "Upgraded conversation",
		SessionPath: copyPath,
	}
	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "conversation" {
		t.Fatalf("page items = %#v, want one folded conversation row", page.Items)
	}
	if !page.Items[0].Open {
		t.Fatal("open recovery tab must aggregate onto the logical row")
	}
}

// The catalog goroutine outlives every request, so an unbounded metadata sync
// there can hold the catalog's single-writer mutex forever and freeze the whole
// sidebar. Guard the call shape, not just today's behaviour.
func TestSessionCatalogGoroutineOnlySyncsMetadataWithATimeout(t *testing.T) {
	source, err := os.ReadFile("session_catalog.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(source)
	start := strings.Index(body, "func (a *App) startSessionCatalog(")
	if start < 0 {
		t.Fatal("startSessionCatalog not found")
	}
	end := strings.Index(body[start+1:], "\nfunc ")
	if end < 0 {
		t.Fatal("end of startSessionCatalog not found")
	}
	for line := range strings.SplitSeq(body[start:start+1+end], "\n") {
		if !strings.Contains(line, "syncSessionCatalogMetadata(") {
			continue
		}
		if !strings.Contains(line, "syncSessionCatalogMetadataBounded(") {
			t.Fatalf("unbounded metadata sync in startSessionCatalog: %s", strings.TrimSpace(line))
		}
	}
}

func TestListProjectTopicsKeepsMetadataWhileFirstCatalogScanIsPending(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Upgraded App"); err != nil {
		t.Fatal(err)
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		for i := range f.Projects {
			if sameProjectRoot(f.Projects[i].Root, root) {
				f.Projects[i].Topics = []string{"topic-keep"}
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicTitles(root, map[string]string{"topic-keep": "Previous chat"}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = catalog.Close(ctx)
	})
	app.sessionCatalog.Store(catalog)

	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "topic-keep" || page.Items[0].Label != "Previous chat" {
		t.Fatalf("topics while v4 catalog is still empty = %#v, want the desktop-projects conversation", page.Items)
	}
}

func TestProjectTreeSnapshotIndexingWaitsForFirstDirectoryScan(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = catalog.Close(ctx)
	})
	app.sessionCatalog.Store(catalog)
	snapshot := app.GetProjectTreeSnapshot()
	if snapshot.IndexingDone {
		t.Fatal("indexingDone must stay false until the first directory scan finishes")
	}
}

func TestContinuePathForOpenFollowsCoveringLeafFromParent(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	q := provider.Message{Role: provider.RoleUser, Content: "question"}
	a := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
	next := provider.Message{Role: provider.RoleUser, Content: "next"}
	done := provider.Message{Role: provider.RoleAssistant, Content: "done"}
	save := func(path, topic string, messages ...provider.Message) {
		t.Helper()
		session := agent.NewSession("sys")
		for _, message := range messages {
			session.Add(message)
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "global", TopicID: topic, TopicTitle: "Upgraded",
		}); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	save(root, "conversation", q, a)
	save(leaf, "legacy-leaf-topic", q, a, next, done)
	if err := agent.SaveBranchMetaPreserveUpdated(leaf, agent.BranchMeta{
		ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic",
		Recovered: true, ParentID: "root", RecoveryDepth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")
	if got := app.continuePathForOpen(root); got != leaf {
		t.Fatalf("continue parent = %q, want covering leaf %q", got, leaf)
	}
	if got := app.continuePathForOpen(leaf); got != "" {
		t.Fatalf("continue leaf = %q, want keep", got)
	}
}

func TestListProjectTopicsUsesAvailableProjectionBeforeEveryGlobalDirectoryIsScanned(t *testing.T) {
	isolateDesktopUserDirs(t)
	legacy := config.SessionDir()
	global := desktopSessionDir(globalWorkspaceRoot())
	for _, dir := range []string{legacy, global} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		f.GlobalTopics = []string{"topic-keep"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicTitles("", map[string]string{"topic-keep": "Previous chat"}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = catalog.Close(ctx)
	})
	app.sessionCatalog.Store(catalog)
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: legacy, Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	page, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "topic-keep" || page.Items[0].Label != "Previous chat" {
		t.Fatalf("topics after only one global dir scanned = %#v, want merged metadata", page.Items)
	}
	if page.Complete || page.ReadyDirectories != 1 || page.PendingDirectories != 1 {
		t.Fatalf("partial directory completeness = %+v", page)
	}
}

func TestListProjectTopicsPaginatesMetadataWhileCatalogIsPartiallyAvailable(t *testing.T) {
	isolateDesktopUserDirs(t)
	legacy := config.SessionDir()
	global := desktopSessionDir(globalWorkspaceRoot())
	for _, dir := range []string{legacy, global} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := updateProjectsFile(func(f *desktopProjectFile) (bool, error) {
		f.GlobalTopics = []string{"topic-a", "topic-b", "topic-c"}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicTitles("", map[string]string{
		"topic-a": "A", "topic-b": "B", "topic-c": "C",
	}); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicCreatedAts("", map[string]int64{
		"topic-a": 100, "topic-b": 200, "topic-c": 300,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = catalog.Close(ctx)
	})
	app.sessionCatalog.Store(catalog)
	if err := catalog.ReconcileDirectory(context.Background(), sessioncatalog.DirectoryTarget{Path: legacy, Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	first, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].TopicID != "topic-c" || first.Items[1].TopicID != "topic-b" {
		t.Fatalf("first page topics = %#v", first.Items)
	}
	if first.NextCursor == "" {
		t.Fatal("first page omitted metadata continuation cursor")
	}
	second, err := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "global", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].TopicID != "topic-a" {
		t.Fatalf("second page topics = %#v", second.Items)
	}
	if second.NextCursor != "" {
		t.Fatalf("second page next cursor = %q, want empty", second.NextCursor)
	}
}

func TestAuthoritativeBotStylePersistIndexesSessionImmediately(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	if err := addProject(root, "Bot project"); err != nil {
		t.Fatal(err)
	}
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	catalog, err := sessioncatalog.Open(context.Background(), sessioncatalog.Options{InMemory: true, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	app.sessionCatalog.Store(catalog)
	history.RegisterSessionPersistObserver(desktopSessionCatalogPersistObserverKey, desktopSessionCatalogPersistObserver{app: app})
	t.Cleanup(func() {
		history.RegisterSessionPersistObserver(desktopSessionCatalogPersistObserverKey, nil)
		app.sessionCatalog.CompareAndSwap(catalog, nil)
		_ = catalog.Close(context.Background())
	})

	path := filepath.Join(dir, "bot-session.jsonl")
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		ID: agent.BranchID(path), Scope: "project", WorkspaceRoot: root,
		TopicID: "bot-topic", TopicTitle: "Bot conversation",
	}); err != nil {
		t.Fatal(err)
	}
	session := agent.NewSession("system")
	session.SetPersistObserver(history.PersistObserver())
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello from bot"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		page, listErr := app.ListProjectTopics(ProjectTopicPageRequest{Scope: "project", WorkspaceRoot: root, Limit: 50})
		if listErr == nil && len(page.Items) == 1 && page.Items[0].TopicID == "bot-topic" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("authoritative bot-style persist did not enter the project catalog immediately")
}

type retargetRuntimeController struct {
	control.SessionAPI
	status control.RuntimeStatus
	path   string
}

func (c *retargetRuntimeController) RuntimeStatus() control.RuntimeStatus { return c.status }
func (c *retargetRuntimeController) SessionPath() string                  { return c.path }
func (c *retargetRuntimeController) PlanMode() bool                       { return false }
func (c *retargetRuntimeController) AutoApproveTools() bool               { return false }
func (c *retargetRuntimeController) Goal() string                         { return "" }
func (c *retargetRuntimeController) GoalStatus() string                   { return "" }
func (c *retargetRuntimeController) ToolApprovalMode() string             { return control.ToolApprovalAsk }

func TestRetargetOpenTabsSkipsRunningSessions(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	q := provider.Message{Role: provider.RoleUser, Content: "question"}
	a := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
	save := func(path, topic string, messages ...provider.Message) {
		t.Helper()
		session := agent.NewSession("sys")
		for _, message := range messages {
			session.Add(message)
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "global", TopicID: topic, TopicTitle: "Upgraded",
		}); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	save(root, "conversation", q, a)
	save(leaf, "legacy-leaf-topic", q, a,
		provider.Message{Role: provider.RoleUser, Content: "next"},
		provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := agent.SaveBranchMetaPreserveUpdated(leaf, agent.BranchMeta{
		ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic",
		Recovered: true, ParentID: "root", RecoveryDepth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")
	idle := &WorkspaceTab{ID: "idle", Scope: "global", SessionPath: root}
	running := &WorkspaceTab{
		ID: "running", Scope: "global", SessionPath: root,
		Ctrl: &retargetRuntimeController{status: control.RuntimeStatus{Running: true}, path: root},
	}
	app.tabs = map[string]*WorkspaceTab{"idle": idle, "running": running}

	app.retargetOpenTabsToContinuations()
	if idle.SessionPath != leaf {
		t.Fatalf("idle tab path = %q, want covering leaf %q", idle.SessionPath, leaf)
	}
	if running.SessionPath != root {
		t.Fatalf("running tab path = %q, want original parent %q", running.SessionPath, root)
	}
}

func TestOpenTopicTabKeepsRunningParentInsteadOfCoveringLeaf(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := desktopSessionDir(globalWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	q := provider.Message{Role: provider.RoleUser, Content: "question"}
	a := provider.Message{Role: provider.RoleAssistant, Content: "answer"}
	save := func(path, topic string, messages ...provider.Message) {
		t.Helper()
		session := agent.NewSession("sys")
		for _, message := range messages {
			session.Add(message)
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
			ID: agent.BranchID(path), Scope: "global", TopicID: topic, TopicTitle: "Upgraded",
		}); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dir, "root.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	save(root, "conversation", q, a)
	save(leaf, "legacy-leaf-topic", q, a,
		provider.Message{Role: provider.RoleUser, Content: "next"},
		provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := agent.SaveBranchMetaPreserveUpdated(leaf, agent.BranchMeta{
		ID: "leaf", Scope: "global", TopicID: "legacy-leaf-topic",
		Recovered: true, ParentID: "root", RecoveryDepth: 1,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "global", "")
	running := &WorkspaceTab{
		ID: "running", Scope: "global", TopicID: "conversation", SessionPath: root,
		Ctrl: &retargetRuntimeController{status: control.RuntimeStatus{Running: true}, path: root},
	}
	app.tabs = map[string]*WorkspaceTab{"running": running}

	_, resolved := app.resolveOpenTopicSessionPath("global", "", root)
	if resolved != root {
		t.Fatalf("resolve running open = %q, want parent %q", resolved, root)
	}

	meta, err := app.openTopicTab("global", "", "conversation", root)
	if err != nil {
		t.Fatal(err)
	}
	if running.SessionPath != root {
		t.Fatalf("running tab path = %q, want parent %q", running.SessionPath, root)
	}
	if meta.ID != "running" || meta.SessionPath != root {
		t.Fatalf("open meta = %+v, want focused running parent", meta)
	}

	idle := &WorkspaceTab{ID: "idle", Scope: "global", TopicID: "conversation", SessionPath: root}
	app.tabs = map[string]*WorkspaceTab{"idle": idle}
	_, idleResolved := app.resolveOpenTopicSessionPath("global", "", root)
	if idleResolved != leaf {
		t.Fatalf("resolve idle open = %q, want covering leaf %q", idleResolved, leaf)
	}
}
