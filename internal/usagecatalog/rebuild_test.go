package usagecatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRebuildReplacesStaleUsageProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "usage.sqlite")
	staleDir := t.TempDir()
	currentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staleDir, "2026-08-09.jsonl"),
		[]byte("{\"ts\":\"2026-08-09T10:00:00Z\",\"source\":\"desktop\",\"total\":9}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "2026-08-10.jsonl"),
		[]byte("{\"ts\":\"2026-08-10T10:00:00Z\",\"source\":\"desktop\",\"total\":42}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seed, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.ReconcileDir(ctx, staleDir); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(ctx, databasePath, currentDir); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rebuilt.Close(context.Background()) })
	stale, err := rebuilt.Query(ctx, "2026-08-09", "2026-08-09", "desktop")
	if err != nil || len(stale) != 0 {
		t.Fatalf("stale rows=%#v err=%v", stale, err)
	}
	current, err := rebuilt.Query(ctx, "2026-08-10", "2026-08-10", "desktop")
	if err != nil || len(current) != 1 || current[0].Total != 42 {
		t.Fatalf("current rows=%#v err=%v", current, err)
	}
}
