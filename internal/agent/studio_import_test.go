package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeStudioImport(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		"v4-0001.jsonl":              []byte(`{"role":"user","content":"hello from studio"}` + "\n"),
		"v4-0001.jsonl.meta":         []byte(`{"title":"imported"}`),
		"v4-0001.lease.lock":         {},
		"v4-0001.lease.json":         []byte(`{"owner":"studio"}`),
		"v4-0001.lock":               {},
		"v4-0001.display-index.json": []byte(`{}`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestStudioImportsAreNotListedAsSessions(t *testing.T) {
	dir := t.TempDir()
	writeStudioImport(t, dir)
	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		t.Fatalf("listed %+v from a directory holding only a Studio import", b)
	}
}

func TestMaintenanceLeavesAStudioImportByteForByte(t *testing.T) {
	dir := t.TempDir()
	want := writeStudioImport(t, dir)
	old := time.Now().Add(-48 * time.Hour)
	for name := range want {
		_ = os.Chtimes(filepath.Join(dir, name), old, old)
	}
	if err := ReconcileSessionSidecars(dir); err != nil {
		t.Fatal(err)
	}
	if got, err := ReclaimableRecoveryBranches(dir, time.Now(), time.Minute); err != nil || len(got) != 0 {
		t.Fatalf("recovery GC picked %v (%v)", got, err)
	}
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s is gone: %v", name, err)
		}
		if string(got) != string(data) {
			t.Fatalf("%s changed", name)
		}
	}
}
