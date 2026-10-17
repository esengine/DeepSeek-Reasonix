package sessioninbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/state/store"
)

// failSalvagedOpenWithUnreadableBlobs corrupts the manifest and replaces the
// blobs directory with a regular file: the portable form of "this directory
// cannot be read", which validatePrivateDir and os.ReadDir both reject.
func failSalvagedOpenWithUnreadableBlobs(t *testing.T, session string) (*Store, string) {
	t.Helper()
	inboxDir := store.SessionInboxDir(session)
	blobsDir := filepath.Join(inboxDir, blobsDirName)

	// Move the bodies aside and leave an unreadable "directory" in their place.
	preserved := filepath.Join(inboxDir, "blobs-preserved")
	if err := os.Rename(blobsDir, preserved); err != nil {
		t.Fatalf("preserve blobs dir: %v", err)
	}
	if err := os.WriteFile(blobsDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("replace blobs dir with regular file: %v", err)
	}

	// Corrupt the manifest so Open takes the quarantine+salvage branch.
	manPath := filepath.Join(inboxDir, manifestName)
	if err := os.WriteFile(manPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}

	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatalf("Open failed instead of reporting the unreadable queue: %v", err)
	}
	return s, preserved
}

func blobBodies(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read preserved blobs: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read preserved blob %s: %v", e.Name(), err)
		}
		out = append(out, string(data))
	}
	return out
}

func newInboxSession(t *testing.T) string {
	t.Helper()
	session := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return session
}

// enqueueThenCorruptManifest leaves one durable body per text on disk and
// replaces the manifest with bytes that cannot be decoded.
func enqueueThenCorruptManifest(t *testing.T, session string, texts ...string) {
	t.Helper()
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range texts {
		if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: text}}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if err := os.WriteFile(filepath.Join(store.SessionInboxDir(session), manifestName), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readManifestBytes(t *testing.T, session string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(store.SessionInboxDir(session), manifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return string(data)
}

// TestCorruptManifestWithUnreadableBlobsIsNotEmptyQueue pins the contract that a
// failed salvage must never publish "0 recovered". On current code the store
// reports Recovered with RecoveredN == 0 and an empty item list while the user's
// bodies are still on disk, so the frontend's RecoveredN>0 notice is skipped and
// the queue looks empty.
func TestCorruptManifestWithUnreadableBlobsIsNotEmptyQueue(t *testing.T) {
	session := newInboxSession(t)
	enqueueThenCorruptManifest(t, session, "the user's lost prompt")
	s2, preserved := failSalvagedOpenWithUnreadableBlobs(t, session)
	defer s2.Close()

	bodies := blobBodies(t, preserved)
	snap := s2.Snapshot()
	t.Logf("snapshot: paused=%v recovered=%v recoveredN=%d unrecovered=%v items=%d; bodies on disk=%d",
		snap.Paused, snap.Recovered, snap.RecoveredN, snap.Unrecovered, len(snap.Items), len(bodies))

	if len(bodies) == 0 {
		t.Fatalf("test setup lost the blob bodies: %v", bodies)
	}
	// Contract: an unreadable queue may be reported as an error or as unknown,
	// but never as a successfully recovered empty queue.
	if snap.Recovered && snap.RecoveredN == 0 && len(snap.Items) == 0 {
		t.Fatalf("unreadable blobs directory reported as an empty recovered queue: "+
			"recovered=%v recoveredN=%d items=%d while %d bodies remain on disk",
			snap.Recovered, snap.RecoveredN, len(snap.Items), len(bodies))
	}
	if !snap.Unrecovered {
		t.Fatalf("unreadable blobs directory was not surfaced as unrecovered: %+v", snap)
	}
	if !snap.Paused {
		t.Fatalf("unreadable queue must not come up runnable: %+v", snap)
	}
	// The failed salvage must not replace the quarantined evidence with an
	// empty manifest, or the next open cannot tell unknown from empty.
	man := readManifestBytes(t, session)
	t.Logf("manifest after failed salvage: %s", man)
	if !strings.Contains(man, "{not-json") {
		t.Fatalf("corrupt manifest was replaced instead of kept as evidence: %s", man)
	}
	if strings.Contains(man, `"items": null`) || strings.Contains(man, `"items": []`) {
		t.Fatalf("failed salvage was persisted as an empty manifest: %s", man)
	}
	// A later mutation must not be able to clear the evidence either.
	if err := s2.SetPaused(false); err == nil {
		t.Fatalf("unpausing an unreadable queue succeeded; manifest now %s", readManifestBytes(t, session))
	} else if !errors.Is(err, ErrUnrecovered) {
		t.Fatalf("the refusal lost its identity: %v", err)
	}
	if got := readManifestBytes(t, session); got != man {
		t.Fatalf("a refused mutation still rewrote the manifest: %s", got)
	}
}

// Control (a): a corrupt manifest over a readable blobs directory still
// recovers every orphan body as an uncertain item and stays paused.
func TestCorruptManifestWithReadableBlobsSalvagesOrphans(t *testing.T) {
	session := newInboxSession(t)
	enqueueThenCorruptManifest(t, session, "orphan one", "orphan two")

	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := s.Snapshot()
	if !snap.Paused || !snap.Recovered {
		t.Fatalf("want paused+recovered after corrupt manifest, got %+v", snap)
	}
	if snap.RecoveredN != 2 || len(snap.Items) != 2 {
		t.Fatalf("want both orphan bodies salvaged, got recoveredN=%d items=%d", snap.RecoveredN, len(snap.Items))
	}
	for _, it := range snap.Items {
		if it.State != StateUncertain || it.BlockCode != BlockManifestSalvaged {
			t.Fatalf("salvaged item %s = state %q code %q, want uncertain/%q", it.ID, it.State, it.BlockCode, BlockManifestSalvaged)
		}
	}
	if _, env, err := s.ReadItem(snap.Items[0].ID); err != nil || env.SubmitText == "" {
		t.Fatalf("salvaged body is not readable: %+v err=%v", env, err)
	}
}

// Control (b): a missing or empty blobs directory is a legitimately empty queue,
// not a failed salvage: recovered with zero items, exactly as before.
func TestCorruptManifestWithAbsentOrEmptyBlobsStaysEmptyRecovered(t *testing.T) {
	for _, tc := range []struct {
		name     string
		emptyDir func(t *testing.T, inboxDir string)
	}{
		{name: "absent", emptyDir: func(t *testing.T, inboxDir string) {
			if err := os.RemoveAll(filepath.Join(inboxDir, blobsDirName)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "empty", emptyDir: func(t *testing.T, inboxDir string) {
			if err := os.RemoveAll(filepath.Join(inboxDir, blobsDirName)); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(inboxDir, blobsDirName), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := newInboxSession(t)
			enqueueThenCorruptManifest(t, session, "body that is gone")
			tc.emptyDir(t, store.SessionInboxDir(session))

			s, err := Open(session, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			snap := s.Snapshot()
			if !snap.Paused || !snap.Recovered || snap.RecoveredN != 0 || len(snap.Items) != 0 {
				t.Fatalf("want paused+recovered+empty for %s blobs dir, got %+v", tc.name, snap)
			}
		})
	}
}

// Control (c): a healthy manifest is loaded verbatim and never rewritten.
func TestHealthyManifestIsUntouchedByOpen(t *testing.T) {
	session := newInboxSession(t)
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "healthy"}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	before := readManifestBytes(t, session)

	s2, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	snap := s2.Snapshot()
	if snap.Paused || snap.Recovered || snap.RecoveredN != 0 || len(snap.Items) != 1 || snap.Items[0].ID != rec.ItemID {
		t.Fatalf("healthy manifest was not loaded as-is: %+v", snap)
	}
	if after := readManifestBytes(t, session); after != before {
		t.Fatalf("healthy manifest was rewritten:\nbefore=%s\nafter=%s", before, after)
	}
}
