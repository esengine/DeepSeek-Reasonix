package serve

import (
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

// A delete that cannot finish must still be one act. Windows briefly refuses to
// unlink an event log a reader has open, which surfaced as a 500 with the
// transcript already erased. The stuck file is staged as a non-empty directory,
// which os.Remove refuses everywhere, so reproducing it needs no Windows.
func TestRemoveSessionFilesHidesSessionWhenAnArtifactIsHeld(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "delete me"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	stuck := store.SessionEventLog(path)
	if err := os.RemoveAll(stuck); err != nil {
		t.Fatalf("clear event log: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stuck, "held"), 0o755); err != nil {
		t.Fatalf("stage stuck event log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "held", "x"), []byte("x"), 0o644); err != nil {
		t.Fatalf("stage stuck child: %v", err)
	}

	if err := removeSessionFiles(dir, path); err != nil {
		t.Fatalf("removeSessionFiles = %v, want the delete to stand despite the held artifact", err)
	}

	// Gone from every surface that lists or resumes, and marked so the next
	// start finishes sweeping what is still held.
	if sessionstore.IsVisibleSession(path) {
		t.Error("session is still visible after being deleted")
	}
	if !sessionstore.IsCleanupPending(path) {
		t.Error("no cleanup marker left, so the held artifacts would never be swept")
	}
	if _, err := os.Stat(stuck); err != nil {
		t.Fatalf("test setup lost the held artifact: %v", err)
	}

	// The reconciler is what finishes it. Once the artifact is releasable, a
	// sweep must leave nothing behind — including the marker.
	if err := os.RemoveAll(stuck); err != nil {
		t.Fatalf("release held artifact: %v", err)
	}
	if err := removeSessionFiles(dir, path); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if sessionstore.IsCleanupPending(path) {
		t.Error("cleanup marker survived a sweep that had nothing left to remove")
	}
	for _, p := range append([]string{path}, store.SessionSidecarFiles(path)...) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("artifact survived the completed delete: %s (err=%v)", p, err)
		}
	}
}

// Nothing may be erased when the delete is refused: the guard has to run before
// the first os.Remove, not alongside it.
func TestRemoveSessionFilesIsAllOrHidden(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	s := sessionstore.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "keep me"})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if err := removeSessionFiles(dir, path); err != nil {
		t.Fatalf("removeSessionFiles: %v", err)
	}
	if sessionstore.IsCleanupPending(path) {
		t.Error("a clean delete must not leave a cleanup marker behind")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("transcript survived a clean delete")
	}
}
