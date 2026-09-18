package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
)

// TestMigrateLegacySessionsOnStartupImportsAndDeletes covers the temporary
// startup bridge: a legacy JSONL transcript is converted into sessions-v4 and
// the legacy files are removed once the target is verified.
func TestMigrateLegacySessionsOnStartupImportsAndDeletes(t *testing.T) {
	isolateCLIConfigHome(t)
	legacyDir := config.SessionDir()
	if legacyDir == "" {
		t.Skip("session dir unavailable")
	}
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(legacyDir, "legacy-session.jsonl")
	saveTestSession(t, transcript, "legacy prompt")
	updated := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	meta := fmt.Sprintf(`{"id":"legacy","created_at":%q,"updated_at":%q}`,
		updated.Add(-time.Hour).UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(transcript+".meta", []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateLegacySessionsOnStartup(nil)

	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Fatalf("legacy transcript survived migration: err=%v", err)
	}
	rows := resumeRows(legacyDir)
	if len(rows) != 1 {
		t.Fatalf("resume rows after migration = %+v, want the migrated v4 session", rows)
	}
	if _, ok := v4ResumeID(rows[0].Path); !ok {
		t.Fatalf("migrated row %q is not a native v4 identity", rows[0].Path)
	}
	if !rows[0].LastActivityAt.Equal(updated) {
		t.Fatalf("migrated row activity = %v, want the source activity %v", rows[0].LastActivityAt, updated)
	}

	// A second startup pass is a no-op: nothing left to import, nothing removed.
	migrateLegacySessionsOnStartup(nil)
	if again := resumeRows(legacyDir); len(again) != 1 {
		t.Fatalf("second migration pass changed the store: %+v", again)
	}
}
