package usagecatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReconcileFileAndDuplicateReceiptAreIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-08-10.jsonl")
	line := []byte("{\"ts\":\"2026-08-10T10:00:00+08:00\",\"model\":\"deepseek/model\",\"source\":\"desktop\",\"total\":42}\n")
	if err := os.WriteFile(path, line, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	rows, err := catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop")
	if err != nil || len(rows) != 1 || rows[0].Total != 42 || rows[0].Requests != 1 {
		t.Fatalf("rows=%#v err=%v", rows, err)
	}
	var receipt AppendReceipt
	if err := catalog.db.QueryRow(`SELECT file_path,day,byte_offset,byte_length,line_hash FROM usage_records WHERE file_path=?`, path).Scan(
		&receipt.Path, &receipt.Day, &receipt.Offset, &receipt.Length, &receipt.LineHash); err != nil {
		t.Fatal(err)
	}
	entry := Entry{Day: receipt.Day, Source: "desktop", ModelRef: "deepseek/model", Provider: "deepseek", Total: 42, Requests: 1}
	if err := catalog.applyReceipt(ctx, receipt, entry); err != nil {
		t.Fatal(err)
	}
	rows, err = catalog.Query(ctx, "2026-08-10", "2026-08-10", "desktop")
	if err != nil || len(rows) != 1 || rows[0].Total != 42 {
		t.Fatalf("duplicate changed aggregate: rows=%#v err=%v", rows, err)
	}
}

func TestReadyRejectsExternalAppendUntilReconciled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-08-10.jsonl")
	if err := os.WriteFile(path, []byte("{\"ts\":\"2026-08-10T10:00:00Z\",\"total\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	if !catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("catalog should be ready")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{\"ts\":\"2026-08-10T11:00:00Z\",\"total\":2}\n")
	_ = f.Close()
	if catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("external append was treated as indexed")
	}
}

func TestReadyRejectsSameSizeRewriteWithDifferentMtime(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-08-10.jsonl")
	original := []byte("{\"ts\":\"2026-08-10T10:00:00Z\",\"total\":1,\"source\":\"desktop\"}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileFile(ctx, path, "2026-08-10"); err != nil {
		t.Fatal(err)
	}
	if !catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("catalog should be ready after reconcile")
	}
	// Same byte length, different content and mtime — Ready must fail so JSONL
	// remains the authority until the projection is rescanned.
	replacement := []byte("{\"ts\":\"2026-08-10T12:00:00Z\",\"total\":9,\"source\":\"desktop\"}\n")
	if len(replacement) != len(original) {
		t.Fatalf("test fixture length mismatch: %d vs %d", len(replacement), len(original))
	}
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		t.Fatal(err)
	}
	// Force a distinct mtime on filesystems with coarse timestamps.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if catalog.Ready(ctx, dir, []string{"2026-08-10"}) {
		t.Fatal("same-size rewrite was treated as still ready")
	}
}
