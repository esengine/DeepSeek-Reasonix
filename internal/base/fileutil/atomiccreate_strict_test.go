package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicCreateStrictFlushesOnlyItsOwnPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "initial.json")
	syncs := 0
	t.Cleanup(SetSyncParentDirForTest(func(actual string) error {
		if actual != path {
			t.Errorf("unexpected publication %s", actual)
		}
		syncs++
		return errors.New("unsupported directory flush")
	}))
	if err := AtomicCreateFileStrict(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicCreateFileStrict(path, []byte("replacement"), 0600); err == nil {
		t.Fatal("overwrote a concurrent initial choice")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "first" || syncs != 1 {
		t.Fatalf("publication changed: %q %v syncs=%d", body, err, syncs)
	}
}
