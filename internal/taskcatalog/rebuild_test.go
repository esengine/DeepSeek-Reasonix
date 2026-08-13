package taskcatalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/taskmonitor"
)

func TestRebuildReplacesStaleTaskProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "tasks.sqlite")
	staleRoot := t.TempDir()
	currentRoot := t.TempDir()
	store := taskmonitor.NewFileStore(filepath.Join(".reasonix", "tasks"))
	now := time.Now()
	if err := store.SaveTask(ctx, staleRoot, snapshot("stale-task", "stale-session", 1, now)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, currentRoot, snapshot("current-task", "current-session", 1, now)); err != nil {
		t.Fatal(err)
	}
	seed, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	staleProject, err := seed.RegisterProject(ctx, staleRoot, "Stale")
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.ReconcileProject(ctx, staleProject); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	currentProject := normalizeProject(Project{Root: currentRoot, Label: "Current"})
	if _, err := Rebuild(ctx, databasePath, []Project{currentProject}); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rebuilt.Close(context.Background()) })
	stalePage, err := rebuilt.ListPage(ctx, PageRequest{ProjectKeys: []string{staleProject.Key}, Limit: 50})
	if err != nil || len(stalePage.Items) != 0 {
		t.Fatalf("stale page=%#v err=%v", stalePage, err)
	}
	currentPage, err := rebuilt.ListPage(ctx, PageRequest{ProjectKeys: []string{currentProject.Key}, Limit: 50})
	if err != nil || len(currentPage.Items) != 1 || currentPage.Items[0].Task.TaskID != "current-task" {
		t.Fatalf("current page=%#v err=%v", currentPage, err)
	}
}
