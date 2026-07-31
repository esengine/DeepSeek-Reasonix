//go:build windows

package repair

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenameRepairNodeNoReplaceWindowsPreservesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("destination"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := renameRepairNodeNoReplace(source, destination); err == nil {
		t.Fatal("Windows no-replace rename overwrote an existing destination")
	}
	for path, want := range map[string]string{source: "source", destination: "destination"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
		}
	}
}
