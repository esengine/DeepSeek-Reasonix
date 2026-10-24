package workspacelease

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestClaimAttemptAndProbeDoNotRetainPartialClaims(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	paths := []string{filepath.Join(root, "a")}
	if _, err := ProbeClaimPaths(root, locks, paths); err != nil {
		t.Fatal(err)
	}
	first, _, err := TryClaimPaths(root, locks, "first", paths)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	for _, probe := range []bool{false, true} {
		var conflict *ConflictError
		if probe {
			conflict, err = ProbeClaimPaths(root, locks, paths)
		} else {
			_, conflict, err = TryClaimPaths(root, locks, "second", paths)
		}
		if !errors.Is(err, ErrClaimBusy) || conflict == nil || conflict.SessionID != "first" {
			t.Fatalf("missing immediate conflict: %+v %v", conflict, err)
		}
	}
	first()
	second, _, err := TryClaimPaths(root, locks, "second", paths)
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	first()
	if conflict, err := ProbeClaimPaths(root, locks, paths); !errors.Is(err, ErrClaimBusy) || conflict.SessionID != "second" {
		t.Fatalf("old release cleared a new claim: %+v %v", conflict, err)
	}
	if _, _, err := TryClaimPaths(root, locks, "invalid", nil); !errors.Is(err, ErrInvalidClaim) {
		t.Fatal(err)
	}
}

func TestClaimAttemptHonorsLegacyWholeWorkspaceLock(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	o, err := New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	release, err := tryLockFile(o.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, _, err := TryClaimPaths(root, locks, "new", []string{filepath.Join(root, "file")}); !errors.Is(err, ErrClaimBusy) {
		t.Fatalf("bypassed old exclusive lock: %v", err)
	}
}
