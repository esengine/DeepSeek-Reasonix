package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
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
