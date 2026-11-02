package historycatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// A diagnostic and a reindex must read the same numbers. Failed here is the
// count `catalogs reindex --json` reports, and the source it names is the one
// the catalog recorded as corrupt.
func TestInspectAgreesWithStatusOnFailedSources(t *testing.T) {
	sessions := testenv.TempDir(t)
	path := filepath.Join(testenv.TempDir(t), "history.sqlite")
	catalog, err := Open(context.Background(), Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	badPath := filepath.Join(sessions, "undecodable.jsonl")
	if err := os.WriteFile(badPath, []byte("this is not a session transcript\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := catalog.ReconcileRoot(context.Background(), Root{Path: sessions, Source: "global", Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	status := catalog.Status()
	if status.Failed != 1 {
		t.Fatalf("fixture: status failed=%d, want the corrupt source recorded", status.Failed)
	}

	// The catalog is still open, so its row lives in the WAL: an inspection
	// that skipped the WAL would report a healthy catalog here.
	recorded, err := Inspect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Failed != status.Failed || recorded.Pending != status.Pending {
		t.Fatalf("inspect failed=%d pending=%d, status failed=%d pending=%d",
			recorded.Failed, recorded.Pending, status.Failed, status.Pending)
	}
	if len(recorded.Failures) != 1 {
		t.Fatalf("failures = %#v, want the one corrupt source", recorded.Failures)
	}
	failure := recorded.Failures[0]
	if failure.Path != badPath || failure.Health != "corrupt" || failure.LastError == "" {
		t.Fatalf("failure = %#v, want %s recorded corrupt with its reason", failure, badPath)
	}
}
