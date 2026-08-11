package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
