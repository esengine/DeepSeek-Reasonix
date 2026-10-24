//go:build !windows

package checkenv

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/filelock"
)

func TestRecoveryReclaimsOnlyUnownedUnjournaledDirectories(t *testing.T) {
	storage := t.TempDir()
	create := func(name string, files ...string) string {
		t.Helper()
		directory := filepath.Join(storage, name)
		if err := os.MkdirAll(filepath.Join(directory, "source", "nested"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if err := os.WriteFile(filepath.Join(directory, file), []byte(`{"version":1}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chmod(filepath.Join(directory, "source", "nested"), 0500); err != nil {
			t.Fatal(err)
		}
		return directory
	}
	abandoned := create("check-abandoned", "owner.lock")
	unlocked := create("check-unlocked")
	held := create("check-held", "owner.lock")
	journaled := create("check-journaled", "owner.lock", "container.json")
	foreign := create("unrelated", "owner.lock")
	release, err := filelock.TryAcquire(filepath.Join(held, "owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if err := recoverContainerJournals(t.Context(), storage, "", nil); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{abandoned, unlocked} {
		if _, err := os.Lstat(removed); !os.IsNotExist(err) {
			t.Errorf("abandoned verification directory retained: %s %v", removed, err)
		}
	}
	for _, kept := range []string{held, journaled, foreign} {
		if _, err := os.Lstat(filepath.Join(kept, "source")); err != nil {
			t.Errorf("directory another owner or recovery needs was removed: %s %v", kept, err)
		}
	}
	for _, directory := range []string{held, journaled, foreign} {
		removeFrozenTree(directory)
	}
}
