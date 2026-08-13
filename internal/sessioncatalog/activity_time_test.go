package sessioncatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestReconcileKeepsSidecarActivityWhenFileMtimeIsNewer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "old.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	activity := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := agent.SaveBranchMetaPreserveUpdated(path, agent.BranchMeta{
		CreatedAt:     activity,
		UpdatedAt:     activity,
		Scope:         "global",
		TopicID:       "topic-old",
		TopicTitle:    "old",
		SchemaVersion: agent.BranchMetaCountsVersion,
		Turns:         1,
		Preview:       "hi",
	}); err != nil {
		t.Fatal(err)
	}
	later := activity.Add(48 * time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	record, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if record.LastActivityAt != activity.UnixMilli() {
		t.Fatalf("lastActivityAt = %d, want sidecar %d; file mtime must not raise known activity", record.LastActivityAt, activity.UnixMilli())
	}
}

func TestReconcileFillsZeroActivityFromFileMtime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "bare.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}

	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	record, ok, err := catalog.GetSession(ctx, path)
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if record.LastActivityAt != when.UnixMilli() {
		t.Fatalf("lastActivityAt = %d, want file mtime %d", record.LastActivityAt, when.UnixMilli())
	}
}
