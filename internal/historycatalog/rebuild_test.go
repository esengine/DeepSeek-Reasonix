package historycatalog

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestReconcileRootDetectsMetaOnlyChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "meta.jsonl")
	saveMessages(t, path, provider.Message{Role: provider.RoleUser, Content: "stable content"})
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{CustomTitle: "old"}); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "history.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := Root{Path: root, Source: "project", Scope: "project", WorkspaceRoot: root}
	if err := catalog.ReconcileRoot(ctx, target); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{CustomTitle: "replacement title with a different size"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileRoot(ctx, target); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := catalog.db.QueryRowContext(ctx, `SELECT custom_title FROM history_sources WHERE path=?`, path).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "replacement title with a different size" {
		t.Fatalf("custom title = %q, want updated meta projection", title)
	}
}

func TestRebuildReplacesStaleHistoryProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "history.sqlite")
	staleRoot := t.TempDir()
	currentRoot := t.TempDir()
	saveMessages(t, filepath.Join(staleRoot, "stale.jsonl"), provider.Message{Role: provider.RoleUser, Content: "obsolete unicorn marker"})
	saveMessages(t, filepath.Join(currentRoot, "current.jsonl"), provider.Message{Role: provider.RoleUser, Content: "current phoenix marker"})
	seed, err := Open(ctx, Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.ReconcileRoot(ctx, Root{Path: staleRoot, Source: "global", Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(ctx, Options{Path: databasePath}, []Root{{Path: currentRoot, Source: "global", Scope: "global"}}); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Open(ctx, Options{Path: databasePath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rebuilt.Close(context.Background()) })
	stale, err := rebuilt.Search(ctx, SearchRequest{Query: "unicorn", Roots: []string{staleRoot}})
	if err != nil || len(stale.Items) != 0 {
		t.Fatalf("stale result=%#v err=%v", stale, err)
	}
	current, err := rebuilt.Search(ctx, SearchRequest{Query: "phoenix", Roots: []string{currentRoot}})
	if err != nil || len(current.Items) != 1 {
		t.Fatalf("current result=%#v err=%v", current, err)
	}
}
