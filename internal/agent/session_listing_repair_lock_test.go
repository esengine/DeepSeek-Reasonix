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

	repairResult := make(chan error, 1)
	go func() {
		_, repairErr := RepairSessionListingProjection(context.Background(), path)
		repairResult <- repairErr
	}()
	select {
	case err = <-repairResult:
	case <-time.After(5 * time.Second):
		t.Fatal("meta-busy repair did not yield to the foreground owner")
	}
	if !errors.Is(err, ErrSessionListingRepairBusy) {
		t.Fatalf("repair err = %v, want busy", err)
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
