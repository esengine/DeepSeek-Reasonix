package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/topicstate"
)

func seedLegacyTopicBridge(t *testing.T, workspaceRoot string) {
	t.Helper()
	for _, path := range legacyTopicPaths(workspaceRoot) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFreshTopicStateUsesSQLiteWithoutLegacyFiles(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	if err := setTopicTitle(workspaceRoot, "topic-fresh", "Fresh"); err != nil {
		t.Fatal(err)
	}
	if got := loadTopicTitle(workspaceRoot, "topic-fresh"); got != "Fresh" {
		t.Fatalf("title = %q", got)
	}
	if _, err := os.Stat(config.DesktopTopicStatePath(workspaceRoot)); err != nil {
		t.Fatalf("SQLite topic state missing: %v", err)
	}
	for _, path := range legacyTopicPaths(workspaceRoot) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("fresh workspace created legacy file %s: %v", filepath.Base(path), err)
		}
	}
}

func TestLegacyTopicStateMigratesAndContinuesMirroring(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	paths := legacyTopicPaths(workspaceRoot)
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(paths[0], []byte(`{"topic-old":"Old title"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[1], []byte(`{"topic-old":"manual"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[2], []byte(`{"topic-old":1234}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[3], []byte(`{"topic-old":{"stage":1,"basisHash":"old","future":{"kept":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := loadTopicTitle(workspaceRoot, "topic-old"); got != "Old title" {
		t.Fatalf("migrated title = %q", got)
	}
	if err := setTopicCreatedAt(workspaceRoot, "topic-old", 5678); err != nil {
		t.Fatal(err)
	}
	if err := recordTopicAutoTitleMeta(workspaceRoot, "topic-old", autoTopicTitleProposal{Stage: 2, UserTurns: 3, BasisHash: "new"}); err != nil {
		t.Fatal(err)
	}
	legacyTitles, err := loadLegacyStringMap(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if got := legacyTitles["topic-old"]; got != "Old title" {
		t.Fatalf("legacy mirror title = %q", got)
	}
	var raw map[string]json.RawMessage
	data, err := os.ReadFile(paths[3])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw["topic-old"]) || !containsJSONKey(raw["topic-old"], "future") {
		t.Fatalf("unknown auto-title metadata was lost: %s", raw["topic-old"])
	}
	var meta topicAutoTitleMeta
	if err := json.Unmarshal(raw["topic-old"], &meta); err != nil || meta.Stage != 2 || meta.BasisHash != "new" {
		t.Fatalf("known auto-title metadata was not updated: %+v, %v", meta, err)
	}
}

func TestLegacyWriterChangeMergesOnNextNewWrite(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	seedLegacyTopicBridge(t, workspaceRoot)
	if err := setTopicTitle(workspaceRoot, "topic-old", "Before downgrade"); err != nil {
		t.Fatal(err)
	}
	if err := setLegacyTopicTitle(workspaceRoot, "topic-old", "Changed by old version", topicTitleSourceManual); err != nil {
		t.Fatal(err)
	}
	if err := setTopicCreatedAt(workspaceRoot, "topic-old", 999); err != nil {
		t.Fatal(err)
	}
	if got := loadTopicTitle(workspaceRoot, "topic-old"); got != "Changed by old version" {
		t.Fatalf("reconciled title = %q", got)
	}
}

func TestLegacyMigrationSkipsInvalidEntriesAndDeletedTopics(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	if err := addProject(workspaceRoot, ""); err != nil {
		t.Fatal(err)
	}
	if err := updateProjectsFile(func(file *desktopProjectFile) (bool, error) {
		file.DeletedTopics = append(file.DeletedTopics, "topic-deleted")
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	path := topicTitlesPath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"topic-good":"Good","topic-invalid":7,"topic-deleted":"Do not restore"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	titles := loadTopicTitles(workspaceRoot)
	if titles["topic-good"] != "Good" || titles["topic-invalid"] != "" || titles["topic-deleted"] != "" {
		t.Fatalf("migrated titles = %#v", titles)
	}
	legacy, err := loadLegacyStringMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := legacy["topic-deleted"]; ok {
		t.Fatalf("legacy mirror resurrected tombstone: %#v", legacy)
	}
}

func TestFutureTopicSchemaUsesLegacyReadOnlyFallback(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspaceRoot := t.TempDir()
	seedLegacyTopicBridge(t, workspaceRoot)
	if err := setLegacyTopicTitle(workspaceRoot, "topic-old", "Readable by old version", topicTitleSourceManual); err != nil {
		t.Fatal(err)
	}
	path := config.DesktopTopicStatePath(workspaceRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(2, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := loadTopicTitle(workspaceRoot, "topic-old"); got != "Readable by old version" {
		t.Fatalf("legacy read fallback = %q", got)
	}
	err = setTopicTitle(workspaceRoot, "topic-old", "must not overwrite")
	var future *topicstate.FutureSchemaError
	if !errors.As(err, &future) {
		t.Fatalf("write error = %v, want FutureSchemaError", err)
	}
	legacy, err := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
	if err != nil {
		t.Fatal(err)
	}
	if legacy["topic-old"] != "Readable by old version" {
		t.Fatalf("future-schema write changed legacy data: %#v", legacy)
	}
}

func TestCorruptTopicDatabaseRebuildsOnlyWithLegacySource(t *testing.T) {
	t.Run("legacy recovery", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		workspaceRoot := t.TempDir()
		seedLegacyTopicBridge(t, workspaceRoot)
		if err := setLegacyTopicTitle(workspaceRoot, "topic-old", "Recovered", topicTitleSourceManual); err != nil {
			t.Fatal(err)
		}
		path := config.DesktopTopicStatePath(workspaceRoot)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := loadTopicTitle(workspaceRoot, "topic-old"); got != "Recovered" {
			t.Fatalf("recovered title = %q", got)
		}
		if matches, _ := filepath.Glob(path + ".corrupt-*"); len(matches) != 1 {
			t.Fatalf("corrupt backups = %#v", matches)
		}
	})

	t.Run("no recovery source", func(t *testing.T) {
		isolateDesktopUserDirs(t)
		workspaceRoot := t.TempDir()
		path := config.DesktopTopicStatePath(workspaceRoot)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		original := []byte("not a sqlite database")
		if err := os.WriteFile(path, original, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := setTopicTitle(workspaceRoot, "topic-new", "No silent reset"); err == nil {
			t.Fatal("write unexpectedly replaced corrupt authoritative database")
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
			t.Fatalf("corrupt database changed: %q, %v", got, err)
		}
		if matches, _ := filepath.Glob(path + ".corrupt-*"); len(matches) != 0 {
			t.Fatalf("database without recovery source was quarantined: %#v", matches)
		}
	})
}

func containsJSONKey(data []byte, key string) bool {
	var fields map[string]json.RawMessage
	return json.Unmarshal(data, &fields) == nil && fields[key] != nil
}
