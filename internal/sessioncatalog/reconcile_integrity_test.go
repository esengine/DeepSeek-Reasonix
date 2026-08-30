package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
)

func TestReconcileRepairsPersistedReadyDirectoryMissingCatalogRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Existing conversation",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	catalog := openIntegrityTestCatalog(t, ctx, catalogPath)
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}

	if _, err := catalog.db.ExecContext(ctx, `DELETE FROM catalog_sessions WHERE directory=?`, dir); err != nil {
		t.Fatal(err)
	}
	catalog = reopenIntegrityTestCatalog(t, ctx, catalog, catalogPath)
	assertIntegrityRepair(t, ctx, catalog, target)

	if _, err := catalog.db.ExecContext(ctx, `DELETE FROM catalog_topics WHERE topic_id='topic'`); err != nil {
		t.Fatal(err)
	}
	catalog = reopenIntegrityTestCatalog(t, ctx, catalog, catalogPath)
	assertIntegrityRepair(t, ctx, catalog, target)
}

func TestReconcileRepairsReadyDirectoryWithEqualCountWrongPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "authoritative.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"authoritative"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{
		Scope: "global", TopicID: "topic", TopicTitle: "Authoritative",
		SchemaVersion: agent.BranchMetaCountsVersion, Turns: 1,
	}); err != nil {
		t.Fatal(err)
	}

	catalogPath := filepath.Join(t.TempDir(), "catalog.sqlite")
	catalog := openIntegrityTestCatalog(t, ctx, catalogPath)
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "global"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.db.ExecContext(ctx, `DELETE FROM catalog_sessions WHERE directory=?`, dir); err != nil {
		t.Fatal(err)
	}
	if err := catalog.UpsertSession(ctx, SessionRecord{
		Path: filepath.Join(dir, "wrong.jsonl"), Directory: dir, Scope: "global",
		TopicID: "wrong", TopicTitle: "Wrong", Turns: 1, TurnsState: TurnsValid,
		Health: HealthOK,
	}); err != nil {
		t.Fatal(err)
	}
	catalog = reopenIntegrityTestCatalog(t, ctx, catalog, catalogPath)
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListSessions(ctx, SessionPageRequest{Scope: "global", Directory: dir, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Path != path {
		t.Fatalf("repaired sessions = %#v, want authoritative path %q", page.Items, path)
	}
}

func openIntegrityTestCatalog(t *testing.T, ctx context.Context, path string) *Catalog {
	t.Helper()
	catalog, err := Open(ctx, Options{Path: path, DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func reopenIntegrityTestCatalog(t *testing.T, ctx context.Context, catalog *Catalog, path string) *Catalog {
	t.Helper()
	if err := catalog.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return openIntegrityTestCatalog(t, ctx, path)
}

func assertIntegrityRepair(t *testing.T, ctx context.Context, catalog *Catalog, target DirectoryTarget) {
	t.Helper()
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].TopicID != "topic" || len(page.Items[0].Sessions) != 1 {
		t.Fatalf("repaired page = %#v, want the persisted conversation restored", page)
	}
}
