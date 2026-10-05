package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestLastKnownGoodProvenancePrefersRecordedMetadata(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := lastKnownGoodConfigPath()
	if path == "" {
		t.Fatal("no snapshot path under an isolated home")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("default_model = \"a/b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorded := time.Date(2026, 8, 9, 14, 34, 17, 0, time.UTC)
	meta := `{"schemaVersion":1,"recordedAt":"` + recorded.Format(time.RFC3339Nano) + `"}`
	if err := os.WriteFile(path+".json", []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}

	at, age := lastKnownGoodProvenance(recorded.Add(16 * 24 * time.Hour))
	if !strings.HasPrefix(at, "2026-08-09T14:34:17") {
		t.Errorf("recordedAt = %q, want the metadata timestamp", at)
	}
	if age != "16 days" {
		t.Errorf("age = %q, want 16 days", age)
	}
}

func TestLastKnownGoodProvenanceEmptyWithoutSnapshot(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	if at, age := lastKnownGoodProvenance(time.Now()); at != "" || age != "" {
		t.Fatalf("dated a snapshot that does not exist: %q %q", at, age)
	}
}

func TestHumanizeSnapshotAgeUnits(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "moments"},
		{time.Minute, "1 minute"},
		{5 * time.Minute, "5 minutes"},
		{time.Hour, "1 hour"},
		{25 * time.Hour, "1 day"},
		{16 * 24 * time.Hour, "16 days"},
	} {
		if got := humanizeSnapshotAge(tc.in); got != tc.want {
			t.Errorf("humanizeSnapshotAge(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
