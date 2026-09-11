package sessioninbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/filelock"
)

func TestTryFreshSnapshotReloadsAnotherStoreCommit(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	first, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	receipt, err := second.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "new user work"}})
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(first.dir, manifestName)
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := first.CachedSnapshot(); len(got.Items) != 0 {
		t.Fatalf("cached snapshot unexpectedly refreshed: %+v", got)
	}
	snapshot, err := first.TryFreshSnapshot()
	if err != nil {
		t.Fatalf("TryFreshSnapshot: %v", err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].ID != receipt.ItemID {
		t.Fatalf("fresh snapshot = %+v, want external item %q", snapshot, receipt.ItemID)
	}
	after, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("TryFreshSnapshot mutated the durable manifest")
	}
	if got := first.CachedSnapshot(); len(got.Items) != 0 {
		t.Fatalf("read-only fresh snapshot mutated the Store cache: %+v", got)
	}
}

func TestTryFreshSnapshotDoesNotWaitForDiskLock(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	release, err := filelock.TryAcquire(filepath.Join(s.dir, diskLockName))
	if err != nil {
		t.Fatalf("hold inbox disk lock: %v", err)
	}
	defer release()
	if _, err := s.TryFreshSnapshot(); !errors.Is(err, ErrSnapshotBusy) {
		t.Fatalf("TryFreshSnapshot error = %v, want ErrSnapshotBusy", err)
	}
}
