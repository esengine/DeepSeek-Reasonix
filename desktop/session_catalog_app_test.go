package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
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
