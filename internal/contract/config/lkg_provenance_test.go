package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func writeLKGSnapshot(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := LastKnownGoodConfigPath()
	if path == "" {
		t.Fatal("no last-known-good path under an isolated home")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLastKnownGoodProvenanceDatesTheSnapshot(t *testing.T) {
	path := writeLKGSnapshot(t, "default_model = \"a/b\"\n")
	recorded := time.Date(2026, 8, 9, 14, 34, 17, 0, time.UTC)
	if err := os.Chtimes(path, recorded, recorded); err != nil {
		t.Fatal(err)
	}
	prev := lkgClock
	lkgClock = func() time.Time { return recorded.Add(16 * 24 * time.Hour) }
	t.Cleanup(func() { lkgClock = prev })

	got := lastKnownGoodProvenance()
	if !strings.Contains(got, "2026-08-09") || !strings.Contains(got, "16 days ago") {
		t.Fatalf("provenance = %q, want the record date and a 16 days ago age", got)
	}
}

func TestLastKnownGoodProvenanceEmptyWithoutSnapshot(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	if got := lastKnownGoodProvenance(); got != "" {
		t.Fatalf("dated a snapshot that does not exist: %q", got)
	}
}

// A recovery that substitutes a stale snapshot must say how stale, so the
// warning is actionable rather than reassuring.
func TestInvalidUserConfigWarningDatesTheSnapshot(t *testing.T) {
	writeLKGSnapshot(t, "default_model = \"snapshot/model\"\n")
	if err := os.WriteFile(UserConfigPath(), []byte("default_model = [broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := lkgClock
	lkgClock = func() time.Time { return time.Now().Add(48 * time.Hour) }
	t.Cleanup(func() { lkgClock = prev })

	cfg, err := LoadForRoot(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cfg.LoadWarnings(), "\n")
	if !strings.Contains(joined, "last-known-good snapshot (recorded ") {
		t.Fatalf("warning does not date the snapshot: %q", joined)
	}
	if !strings.Contains(joined, "ago)") {
		t.Fatalf("warning does not age the snapshot: %q", joined)
	}
}
