package store

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The directory sidecar is the one every hand-rolled deleter dropped: a list of
// paths given to os.Remove skips it, so spilled output outlived the
// conversations it belonged to.
func TestRemoveSessionArtifactsTakesTheOutputsDir(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	write(t, path, "{}\n")
	write(t, SessionEventLog(path), "{}\n")
	extra := filepath.Join(dir, "session.acp.json")
	write(t, extra, "{}\n")

	outputs := SessionOutputsDir(path)
	if err := os.MkdirAll(outputs, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(outputs, "call-1.txt"), "spilled\n")

	if err := RemoveSessionArtifacts(path, extra); err != nil {
		t.Fatalf("RemoveSessionArtifacts: %v", err)
	}
	for _, gone := range []string{path, SessionEventLog(path), extra, outputs} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("survived the delete: %s (err=%v)", filepath.Base(gone), err)
		}
	}
}

// Nothing to delete is not a failure: most sessions never spilled, and a
// re-run of a completed delete must stay quiet.
func TestRemoveSessionArtifactsIsIdempotent(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "never-existed.jsonl")
	for range 2 {
		if err := RemoveSessionArtifacts(path); err != nil {
			t.Fatalf("RemoveSessionArtifacts on absent session: %v", err)
		}
	}
	if err := RemoveSessionArtifacts(""); err != nil {
		t.Errorf("empty path: %v", err)
	}
}

// A file that refuses to go must be reported, not swallowed: the front ends
// leave a cleanup marker on the strength of that error.
func TestRemoveSessionArtifactsReportsRefusals(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	write(t, path, "{}\n")

	// A non-empty directory where a file sidecar belongs: os.Remove refuses it
	// on every platform, standing in for a locked file on Windows.
	stuck := SessionEventLog(path)
	if err := os.MkdirAll(filepath.Join(stuck, "held"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(stuck, "held", "x"), "x")

	if err := RemoveSessionArtifacts(path); err == nil {
		t.Fatal("a sidecar that could not be removed must surface as an error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the refusal must not stop the rest of the delete")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Image blobs are part of the conversation: deleting it must not leave its
// pictures behind, and a replay finds them from the event log's name alone.
func TestRemoveSessionArtifactsTakesTheImageBlobs(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "session.jsonl")
	write(t, path, "{}\n")
	blobs := SessionBlobsDir(path)
	if got := SessionBlobsDirForEventLog(SessionEventLog(path)); got != blobs {
		t.Fatalf("blob dir from the event log = %s, want %s", got, blobs)
	}
	if err := os.MkdirAll(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(blobs, "0123"), "data:image/png;base64,AAAA")
	if err := RemoveSessionArtifacts(path); err != nil {
		t.Fatalf("RemoveSessionArtifacts: %v", err)
	}
	if _, err := os.Stat(blobs); !os.IsNotExist(err) {
		t.Fatalf("image blobs survived the delete: %v", err)
	}
}
