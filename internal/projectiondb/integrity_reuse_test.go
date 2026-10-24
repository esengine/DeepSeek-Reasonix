package projectiondb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIntegrityReuseConcurrentReadersAndBoundedCertificates(t *testing.T) {
	dir := t.TempDir()
	var cache integrityCertificates
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for index := range 20 {
				path := filepath.Join(dir, fmt.Sprintf("%d-%d.sqlite", worker, index))
				if err := os.WriteFile(path, []byte("database"), 0600); err != nil {
					t.Error(err)
					return
				}
				for range 2 {
					if err := cache.check(path, false, func() error { return nil }); err != nil {
						t.Error(err)
						return
					}
				}
			}
		})
	}
	workers.Wait()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) > 128 {
		t.Fatalf("unbounded certificates: %d", len(cache.entries))
	}
}

func TestIntegrityReuseRequiresUnchangedDatabaseAndWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projection.sqlite")
	if err := os.WriteFile(path, []byte("verified database"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache integrityCertificates
	audits := 0
	validate := func() error { audits++; return nil }
	check := func(want int) {
		t.Helper()
		if err := cache.check(path, false, validate); err != nil {
			t.Fatal(err)
		}
		if audits != want {
			t.Fatalf("full integrity audits=%d want=%d", audits, want)
		}
	}
	check(1)
	check(1)
	// SQLite creates/removes an empty WAL when a read connection opens/closes.
	if err := os.WriteFile(path+"-wal", nil, 0600); err != nil {
		t.Fatal(err)
	}
	check(1)
	if err := os.WriteFile(path+"-wal", make([]byte, 100), 0600); err != nil {
		t.Fatal(err)
	}
	check(2)
	check(3) // An open nonempty WAL is always audited, even with stable stat data.
	if err := os.WriteFile(path+"-wal", make([]byte, 200), 0600); err != nil {
		t.Fatal(err)
	}
	check(4)
	if err := os.Remove(path + "-wal"); err != nil {
		t.Fatal(err)
	}
	check(5)
	// Atomic replacement must invalidate even with the same length and mtime.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := path + ".new"
	if err := os.WriteFile(replacement, []byte("verified database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	check(6)
	check(6)
	// Checkpoints may retain coarse timestamps while changing SQLite's header.
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("modified database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	check(7)
}

func TestIntegrityReuseNeverCertifiesFailureOrConcurrentMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projection.sqlite")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache integrityCertificates
	failure := errors.New("corrupt index")
	if err := cache.check(path, false, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	audits := 0
	if err := cache.check(path, false, func() error { audits++; return os.WriteFile(path, []byte("changed during audit"), 0600) }); err != nil {
		t.Fatal(err)
	}
	if err := cache.check(path, false, func() error { audits++; return nil }); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatal("concurrent mutation was certified")
	}
	// A quick audit is never enough for a full-audit caller.
	var quickCache integrityCertificates
	if err := quickCache.check(path, true, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := quickCache.check(path, false, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal("quick audit replaced full integrity validation")
	}
}

func TestIntegrityReuseRejectsReplacementDuringOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projection.sqlite")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	opened, known := integrityFileState(path)
	if err := os.WriteFile(path, []byte("second generation"), 0600); err != nil {
		t.Fatal(err)
	}
	var cache integrityCertificates
	// A successful audit of the old open handle is not proof of the new path.
	if err := cache.checkOpened(path, false, opened, known, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	audits := 0
	if err := cache.check(path, false, func() error { audits++; return nil }); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatal("replacement was incorrectly certified by an older open handle")
	}
}

func TestIntegrityReuseDetectsNewConstraintCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projection.sqlite")
	opts := OpenOptions{Path: path, RequireDisk: true, ReuseIntegrityCheck: true,
		Migrations: []Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE guarded(value INTEGER CHECK(value>0))`)
			return err
		}}}}
	for range 3 {
		h, err := Open(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.DB.Close(); err != nil {
			t.Fatal(err)
		}
	}
	h, err := Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	h.DB.SetMaxOpenConns(1)
	if _, err := h.DB.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`INSERT INTO guarded VALUES(-1)`); err != nil {
		t.Fatal(err)
	}
	if err := h.DB.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = Open(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer h.DB.Close()
	if h.Status.QuarantinedPath == "" {
		t.Fatal("changed corrupt database reused an earlier integrity certificate")
	}
}

func BenchmarkHistoryProjectionRepeatedOpen(b *testing.B) {
	path := filepath.Join(b.TempDir(), "history.sqlite")
	opts := OpenOptions{Path: path, RequireDisk: true, MaxOpenConns: 1,
		Migrations: []Migration{{Version: 1, Apply: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, `CREATE TABLE history(id INTEGER PRIMARY KEY, body BLOB); CREATE INDEX history_size ON history(length(body))`)
			return err
		}}}}
	h, err := Open(b.Context(), opts)
	if err != nil {
		b.Fatal(err)
	}
	// About 100 MiB with a body table and index, comparable to a large locator.
	if _, err := h.DB.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<24000) INSERT INTO history SELECT x,zeroblob(4096) FROM n`); err != nil {
		b.Fatal(err)
	}
	if err := h.DB.Close(); err != nil {
		b.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("fixture database bytes=%d", info.Size())
	for _, reuse := range []bool{false, true} {
		b.Run(fmt.Sprintf("reuse=%v", reuse), func(b *testing.B) {
			opts.ReuseIntegrityCheck = reuse
			// Warm the unchanged-file proof outside the measured loop.
			warm, err := Open(b.Context(), opts)
			if err != nil {
				b.Fatal(err)
			}
			if err := warm.DB.Close(); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for range b.N {
				h, err := Open(b.Context(), opts)
				if err != nil {
					b.Fatal(err)
				}
				var count int
				if err := h.DB.QueryRow(`SELECT COUNT(*) FROM (SELECT id FROM history ORDER BY id DESC LIMIT 32)`).Scan(&count); err != nil || count != 32 {
					b.Fatalf("count=%d err=%v", count, err)
				}
				if err := h.DB.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
