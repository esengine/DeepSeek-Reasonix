package agent

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestRepairSessionListingProjectionDoesNotWaitForMetaLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta-busy.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "question"}})
	unlockMeta, err := LockSessionMetaPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockMeta()

	started := time.Now()
	_, err = RepairSessionListingProjection(context.Background(), path)
	if !errors.Is(err, ErrSessionListingRepairBusy) {
		t.Fatalf("repair err = %v, want busy", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("meta-busy repair waited %v", elapsed)
	}

	unlockSave, ok := tryLockSessionSavePath(path)
	if !ok {
		t.Fatal("repair retained the foreground save lock after yielding")
	}
	unlockSave()
	unlockFile, err := tryLockSessionFile(path)
	if err != nil {
		t.Fatalf("repair retained the compatibility file lock after yielding: %v", err)
	}
	unlockFile()
}
