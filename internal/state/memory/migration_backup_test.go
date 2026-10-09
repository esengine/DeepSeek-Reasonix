package memory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
)

const legacyBackupFixture = "---\r\ndescription:    old file\r\nunknown_key: [keep, this]\r\nname: legacy-fact\r\nmetadata:\r\n  type: project\r\n  scope: project\r\n---\r\n\r\nlegacy body  \r\n"

func TestMigrationBackupExactBytesAndIsolation(t *testing.T) {
	store := Store{Dir: filepath.Join(testenv.TempDir(t), "project"), GlobalDir: filepath.Join(testenv.TempDir(t), "global")}
	originals := map[string]string{
		store.Dir:       legacyBackupFixture,
		store.GlobalDir: strings.ReplaceAll(legacyBackupFixture, "scope: project", "scope: global"),
	}
	for _, dir := range store.dirs() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "original-name.md"), []byte(originals[dir]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if report, err := store.MigrateV2(); err != nil || report.Migrated != 2 {
		t.Fatalf("migration = %+v, %v", report, err)
	}
	index := store.Index()
	for _, dir := range store.dirs() {
		backup := filepath.Join(dir, ".migration-backup", "original-name.md")
		if got := mustReadString(t, backup); got != originals[dir] {
			t.Fatalf("backup changed bytes: %q", got)
		}
		var manifest struct {
			MigratedAt time.Time `json:"migrated_at"`
			AppVersion string    `json:"app_version"`
		}
		if err := json.Unmarshal([]byte(mustReadString(t, filepath.Join(dir, ".migration-backup", "manifest.json"))), &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.MigratedAt.IsZero() || manifest.AppVersion == "" {
			t.Fatalf("manifest = %+v", manifest)
		}
		active, ok := loadMemory(filepath.Join(dir, "original-name.md"))
		if !ok || active.Revision != 1 || active.Scope != store.scopeForDir(dir) {
			t.Fatalf("active = %+v, %v", active, ok)
		}
		if len(store.Revisions(active.ID)) != 0 {
			t.Fatal("backup entered revision history")
		}
		if err := os.WriteFile(filepath.Join(dir, ".migration-backup", "ghost.md"), []byte("backup-only-secret-marker"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.ListAll()) != 2 || len(store.List()) != 1 || store.Index() != index {
		t.Fatal("backup changed active catalog/index")
	}
	if _, ok := store.Read("ghost"); ok {
		t.Fatal("backup became active")
	}
	for _, dir := range store.dirs() {
		path := filepath.Join(dir, "original-name.md")
		before := mustReadString(t, path)
		manifest := mustReadString(t, filepath.Join(dir, ".migration-backup", "manifest.json"))
		if report, err := store.MigrateV2(); err != nil || report.Migrated != 0 {
			t.Fatalf("second migration = %+v, %v", report, err)
		}
		if mustReadString(t, path) != before || mustReadString(t, filepath.Join(dir, ".migration-backup", "manifest.json")) != manifest {
			t.Fatal("second migration changed files")
		}
	}
}

func TestMigrationBackupFailureBlocksRewrite(t *testing.T) {
	for _, obstruction := range []string{"directory", "fact", "manifest"} {
		t.Run(obstruction, func(t *testing.T) {
			dir := testenv.TempDir(t)
			path := filepath.Join(dir, "legacy-fact.md")
			if err := os.WriteFile(path, []byte(legacyBackupFixture), 0o644); err != nil {
				t.Fatal(err)
			}
			backupDir := filepath.Join(dir, ".migration-backup")
			if obstruction == "directory" {
				if err := os.WriteFile(backupDir, []byte("blocked"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				name := "legacy-fact.md"
				if obstruction == "manifest" {
					name = "manifest.json"
				}
				if err := os.MkdirAll(filepath.Join(backupDir, name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := (Store{Dir: dir}).MigrateV2()
			var backupErr *MigrationBackupError
			if !errors.As(err, &backupErr) || errors.Unwrap(backupErr) == nil {
				t.Fatalf("want typed backup cause, got %v", err)
			}
			if mustReadString(t, path) != legacyBackupFixture {
				t.Fatal("failed backup rewrote legacy fact")
			}
		})
	}
}

func TestMigrationBackupCrashBeforeRewriteAndRetry(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "legacy-fact.md")
	if err := os.WriteFile(path, []byte(legacyBackupFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := fileutil.CrashPoint
	t.Cleanup(func() { fileutil.CrashPoint = previous })
	crashed := false
	func() {
		defer func() {
			if recover() != nil {
				crashed = true
			}
		}()
		fileutil.CrashPoint = func(op, target string) {
			if op == "atomic-write" && target == path {
				panic("interrupted rewrite")
			}
		}
		_, _ = (Store{Dir: dir}).MigrateV2()
	}()
	fileutil.CrashPoint = previous
	if !crashed {
		t.Fatal("rewrite crash point not reached")
	}
	if mustReadString(t, path) != legacyBackupFixture || mustReadString(t, filepath.Join(dir, ".migration-backup", "legacy-fact.md")) != legacyBackupFixture {
		t.Fatal("interruption lost original bytes")
	}
	if err := os.WriteFile(path, []byte(legacyBackupFixture+"edited before retry\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if report, err := (Store{Dir: dir}).MigrateV2(); err != nil || report.Migrated != 1 {
		t.Fatalf("retry = %+v, %v", report, err)
	}
	if mustReadString(t, filepath.Join(dir, ".migration-backup", "legacy-fact.md")) != legacyBackupFixture {
		t.Fatal("retry replaced first backup")
	}
}

func TestMigrationBackupRejectsNamesOutsideBackupDir(t *testing.T) {
	dir := testenv.TempDir(t)
	for _, name := range []string{"", ".", "..", "../escape.md", "a/b.md", `a\b.md`, "a\x00b.md"} {
		var backupErr *MigrationBackupError
		err := backupLegacyMemory(dir, name, []byte("x"), "test")
		if !errors.As(err, &backupErr) || !errors.Is(err, os.ErrInvalid) {
			t.Errorf("name %q: want typed ErrInvalid, got %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, migrationBackupDir)); err == nil {
		t.Fatal("refused name still created a backup directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.md")); err == nil {
		t.Fatal("hostile name escaped")
	}
}
