package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

// TestReconcileDoesNotRaiseLastActivityFromFileMtime guards against a recency
// regression: an external touch (backup, sync, cloud mirror, antivirus scan, or
// a projection rebuild that rewrites sidecars) advances the .jsonl mtime
// without representing real user activity. The catalog must keep the
// authoritative meta UpdatedAt instead of stamping every historical session as
// "just now".
func TestReconcileDoesNotRaiseLastActivityFromFileMtime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	past := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		Scope:         "global",
		TopicID:       "topic",
		TopicTitle:    "Topic",
		SchemaVersion: agent.BranchMetaCountsVersion,
		Turns:         1,
		CreatedAt:     past,
		UpdatedAt:     past,
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate a later external touch that advances only the file mtime.
	later := time.Now()
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{
		Path:          filepath.Join(t.TempDir(), "catalog.sqlite"),
		DisableRepair: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}

	page, err := catalog.ListTopics(ctx, TopicPageRequest{Scope: "global", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	if got, want := page.Items[0].LastActivityAt, past.UnixMilli(); got != want {
		t.Fatalf("lastActivityAt = %d, want %d (meta UpdatedAt must not be raised by file mtime)", got, want)
	}
}
