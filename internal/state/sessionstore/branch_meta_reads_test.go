package sessionstore

import (
	"crypto/sha256"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"
)

func countMetaReads(t *testing.T) *int {
	var n int
	branchMetaReadHook = func(string) { n++ }
	t.Cleanup(func() { branchMetaReadHook = nil })
	return &n
}

// One locked read-modify-write reads the sidecar once: the update has already
// seen what is on disk, so the write does not ask again.
func TestOneMetaTransactionReadsTheSidecarOnce(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	if err := UpdateSessionMeta(path, "m", "p", 1, true); err != nil {
		t.Fatal(err)
	}
	n := countMetaReads(t)
	if err := UpdateSessionMeta(path, "m", "p2", 2, true); err != nil {
		t.Fatal(err)
	}
	if *n != 1 {
		t.Fatalf("UpdateSessionMeta read the sidecar %d times, want 1", *n)
	}
	*n = 0
	if err := UpdateBranchMeta(path, true, func(m *BranchMeta) error { m.TopicTitle = "t"; return nil }); err != nil {
		t.Fatal(err)
	}
	if *n != 1 {
		t.Fatalf("UpdateBranchMeta read the sidecar %d times, want 1", *n)
	}
}

func TestRecordingARevisionReadsTheSidecarOnceAndKeepsTheLedger(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	if err := UpdateSessionMeta(path, "m", "p", 1, true); err != nil {
		t.Fatal(err)
	}
	n := countMetaReads(t)
	rev, err := recordSessionContentRevision(path, sha256.Sum256([]byte("a")), 0)
	if err != nil || rev != 1 {
		t.Fatalf("revision = %d, %v", rev, err)
	}
	if *n != 1 {
		t.Fatalf("recording a revision read the sidecar %d times, want 1", *n)
	}
	rev, err = recordSessionContentRevision(path, sha256.Sum256([]byte("b")), 0)
	if err != nil || rev != 2 {
		t.Fatalf("second revision = %d, %v", rev, err)
	}
	meta, _, _ := LoadBranchMeta(path)
	if meta.Revision != 2 || meta.ContentDigest == "" {
		t.Fatalf("ledger = %+v", meta)
	}
}

// A revision another runtime already recorded must survive our write.
func TestAWriterBehindTheLedgerDoesNotLowerIt(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	if err := UpdateSessionMeta(path, "m", "p", 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := recordSessionContentRevision(path, sha256.Sum256([]byte("a")), 5); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(path, true, func(m *BranchMeta) error { m.Revision = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	meta, _, _ := LoadBranchMeta(path)
	if meta.Revision != 6 {
		t.Fatalf("revision = %d, want the recorded 6 preserved", meta.Revision)
	}
}
