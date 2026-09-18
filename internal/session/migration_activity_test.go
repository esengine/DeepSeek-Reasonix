package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// legacySessionWithTimes writes a one-turn legacy transcript whose branch
// metadata records the given source times, then returns the transcript path.
func legacySessionWithTimes(t *testing.T, dir string, created, updated time.Time) string {
	t.Helper()
	path := filepath.Join(dir, "sessions", "legacy.jsonl")
	s := agent.NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf(`{"id":"legacy","created_at":%q,"updated_at":%q}`,
		created.UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(path+".meta", []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrateLegacyPreservesSourceTimes(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 8, 30, 16, 39, 47, 0, time.UTC)
	updated := time.Date(2026, 8, 30, 17, 43, 45, 0, time.UTC)
	source := legacySessionWithTimes(t, dir, created, updated)

	result, err := MigrateLegacy(t.Context(), source, filepath.Join(dir, "sessions-v4"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readManifest(filepath.Join(result.TargetDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.CreatedAt.Equal(created) {
		t.Fatalf("manifest createdAt = %v, want %v", manifest.CreatedAt, created)
	}
	if manifest.Source == nil || !manifest.Source.UpdatedAt.Equal(updated) {
		t.Fatalf("manifest source updatedAt = %+v, want %v", manifest.Source, updated)
	}
	info, err := os.Stat(logPathForManifest(result.TargetDir, manifest))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(updated) {
		t.Fatalf("migrated log mtime = %v, want the source activity %v", info.ModTime(), updated)
	}
}

func TestRepairMigratedActivityBackfillsSourceTimes(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 8, 30, 16, 39, 47, 0, time.UTC)
	updated := time.Date(2026, 8, 30, 17, 43, 45, 0, time.UTC)
	source := legacySessionWithTimes(t, dir, created, updated)

	root := filepath.Join(dir, "sessions-v4")
	result, err := MigrateLegacy(t.Context(), source, root)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a target published before the source times were recorded: the
	// manifest carries no source times and the log looks freshly imported.
	manifestPath := filepath.Join(result.TargetDir, "manifest.json")
	manifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.CreatedAt = time.Now().UTC()
	manifest.Source.CreatedAt, manifest.Source.UpdatedAt = time.Time{}, time.Time{}
	if err := writeManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	logPath := logPathForManifest(result.TargetDir, manifest)
	now := time.Now()
	if err := os.Chtimes(logPath, now, now); err != nil {
		t.Fatal(err)
	}

	repaired, err := RepairMigratedActivity(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
	manifest, err = readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.CreatedAt.Equal(created) {
		t.Fatalf("repaired manifest createdAt = %v, want %v", manifest.CreatedAt, created)
	}
	if manifest.Source == nil || !manifest.Source.UpdatedAt.Equal(updated) {
		t.Fatalf("repaired source updatedAt = %+v, want %v", manifest.Source, updated)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(updated) {
		t.Fatalf("repaired log mtime = %v, want %v", info.ModTime(), updated)
	}

	// A second pass is a no-op once the times are recorded.
	again, err := RepairMigratedActivity(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second repair pass = %d, want 0", again)
	}
}

func TestRepairMigratedActivitySkipsContinuedSession(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 8, 30, 16, 39, 47, 0, time.UTC)
	updated := time.Date(2026, 8, 30, 17, 43, 45, 0, time.UTC)
	source := legacySessionWithTimes(t, dir, created, updated)

	root := filepath.Join(dir, "sessions-v4")
	result, err := MigrateLegacy(t.Context(), source, root)
	if err != nil {
		t.Fatal(err)
	}
	// Drop the recorded times, then continue the session with a real turn so
	// the log is no longer import-only.
	manifestPath := filepath.Join(result.TargetDir, "manifest.json")
	manifest, err := readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Source.CreatedAt, manifest.Source.UpdatedAt = time.Time{}, time.Time{}
	if err := writeManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	target, err := OpenWithOptions(result.TargetDir, result.TargetID, OpenOptions{ExternalHistory: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Append(t.Context(), Batch{OperationID: "user:turn", Events: []Event{{Kind: "turn/start"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := target.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	logPath := logPathForManifest(result.TargetDir, manifest)
	before, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}

	repaired, err := RepairMigratedActivity(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if repaired != 0 {
		t.Fatalf("repaired a continued session: %d", repaired)
	}
	after, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("continued session mtime changed: %v -> %v", before.ModTime(), after.ModTime())
	}
}
