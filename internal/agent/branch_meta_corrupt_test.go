package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMetaRaw writes raw bytes to the branch-meta sidecar next to sessionPath.
func writeMetaRaw(t *testing.T, sessionPath string, data []byte) {
	t.Helper()
	if err := os.WriteFile(BranchMetaPath(sessionPath), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadBranchMetaZeroFilledIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	// All-NUL torn write (like a Windows non-atomic copy truncated by a
	// forced reboot — #6325).
	writeMetaRaw(t, path, make([]byte, 1043))

	meta, ok, err := LoadBranchMeta(path)
	if err != nil {
		t.Fatalf("zero-filled meta should be treated as absent, got err=%v", err)
	}
	if ok {
		t.Fatal("zero-filled meta should report ok=false")
	}
	if meta.ID != "" {
		t.Fatalf("absent meta should have empty ID, got %q", meta.ID)
	}
}

func TestLoadBranchMetaAllWhitespaceIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeMetaRaw(t, path, []byte("   \t\r\n   \n"))

	_, ok, err := LoadBranchMeta(path)
	if err != nil {
		t.Fatalf("whitespace-only meta should be treated as absent, got err=%v", err)
	}
	if ok {
		t.Fatal("whitespace-only meta should report ok=false")
	}
}

func TestLoadBranchMetaGenuineCorruptionStillErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	// Partial JSON (contains real content, not all-zero/whitespace) must still
	// surface as an error so we do not silently swallow real corruption.
	writeMetaRaw(t, path, []byte(`{"id":"broken`)) // truncated JSON, non-zero

	_, _, err := LoadBranchMeta(path)
	if err == nil {
		t.Fatal("genuine partial JSON should still return an error")
	}
}

func TestEnsureBranchMetaRebuildsCorruptMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Corrupt the sidecar with all-zero bytes, then EnsureBranchMeta should
	// rebuild it instead of failing.
	writeMetaRaw(t, path, make([]byte, 4096))

	meta, err := EnsureBranchMeta(path)
	if err != nil {
		t.Fatalf("EnsureBranchMeta should rebuild corrupt meta, got err=%v", err)
	}
	if meta.ID != BranchID(path) {
		t.Fatalf("rebuilt meta ID = %q, want %q", meta.ID, BranchID(path))
	}
	// After rebuild, the sidecar must be valid JSON.
	loaded, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("rebuilt meta should load, ok=%v err=%v", ok, err)
	}
	if loaded.ID != BranchID(path) {
		t.Fatalf("rebuilt loaded ID = %q", loaded.ID)
	}
}
