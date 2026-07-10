package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

// forkRecoveryBranch builds a real conflict-recovery branch: a diverged disk
// parent plus a stale in-memory session, forked through SaveRecoveryBranch —
// the exact artifacts GC will meet in the field.
func forkRecoveryBranch(t *testing.T, dir, name string) (parentPath, branchPath string, branchMsgs []provider.Message) {
	t.Helper()
	parentPath = filepath.Join(dir, name+".jsonl")
	disk := NewSession("sys")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk " + name})
	if err := disk.Save(parentPath); err != nil {
		t.Fatalf("Save parent: %v", err)
	}
	stale := NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local " + name})
	info, err := stale.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: parentPath})
	if err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}
	return parentPath, info.Path, stale.Snapshot()
}

// coverBranchInParent rewrites the parent so it contains the branch's content
// plus later turns — the "original session went on and kept everything the
// fork preserved" shape that makes the fork redundant.
func coverBranchInParent(t *testing.T, parentPath string, branchMsgs []provider.Message) {
	t.Helper()
	merged := NewSession("")
	merged.Messages = append([]provider.Message(nil), branchMsgs...)
	merged.Add(provider.Message{Role: provider.RoleAssistant, Content: "answered after recovery"})
	if err := merged.Save(parentPath); err != nil {
		t.Fatalf("Save covering parent: %v", err)
	}
}

func TestReclaimableRecoveryBranchesCollectsOnlyCoveredIdleForks(t *testing.T) {
	dir := t.TempDir()
	later := time.Now().Add(48 * time.Hour)

	// Covered + idle + unleased: reclaimable.
	_, covered, coveredMsgs := forkRecoveryBranch(t, dir, "covered")
	coverBranchInParent(t, filepath.Join(dir, "covered.jsonl"), coveredMsgs)

	// Diverged parent: the fork holds turns that exist nowhere else — kept.
	forkRecoveryBranch(t, dir, "diverged")

	// Continued on: one follow-up turn on the branch disqualifies it forever.
	continuedParent, continuedBranch, continuedMsgs := forkRecoveryBranch(t, dir, "continued")
	coverBranchInParent(t, continuedParent, continuedMsgs)
	cont, err := LoadSession(continuedBranch)
	if err != nil {
		t.Fatalf("LoadSession continued branch: %v", err)
	}
	cont.Add(provider.Message{Role: provider.RoleAssistant, Content: "user kept chatting here"})
	if err := cont.Save(continuedBranch); err != nil {
		t.Fatalf("Save continued branch: %v", err)
	}

	got, err := ReclaimableRecoveryBranches(dir, later, RecoveryGCGracePeriod)
	if err != nil {
		t.Fatalf("ReclaimableRecoveryBranches: %v", err)
	}
	if len(got) != 1 || got[0] != covered {
		t.Fatalf("reclaimable = %v, want only %q", got, covered)
	}
}

func TestReclaimableRecoveryBranchesRespectsGraceLeaseAndMissingParent(t *testing.T) {
	dir := t.TempDir()
	later := time.Now().Add(48 * time.Hour)

	parentPath, branchPath, branchMsgs := forkRecoveryBranch(t, dir, "guarded")
	coverBranchInParent(t, parentPath, branchMsgs)

	// Fresh fork inside the grace window: kept.
	if got, err := ReclaimableRecoveryBranches(dir, time.Now(), RecoveryGCGracePeriod); err != nil || len(got) != 0 {
		t.Fatalf("within grace = %v err=%v, want none", got, err)
	}

	// Lease held (by this very process): kept.
	lease, err := TryAcquireSessionLease(branchPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	if !SessionLeaseHeld(branchPath) {
		t.Fatal("SessionLeaseHeld = false while this process holds the lease")
	}
	if got, err := ReclaimableRecoveryBranches(dir, later, RecoveryGCGracePeriod); err != nil || len(got) != 0 {
		t.Fatalf("with lease held = %v err=%v, want none", got, err)
	}
	lease.Release()
	if SessionLeaseHeld(branchPath) {
		t.Fatal("SessionLeaseHeld = true after release")
	}

	// Released + idle: reclaimable now.
	if got, err := ReclaimableRecoveryBranches(dir, later, RecoveryGCGracePeriod); err != nil || len(got) != 1 || got[0] != branchPath {
		t.Fatalf("after release = %v err=%v, want %q", got, err, branchPath)
	}

	// Parent gone: content is no longer covered anywhere — kept.
	for _, suffix := range []string{"", ".events.jsonl", ".meta"} {
		if err := os.Remove(parentPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove parent artifact %s: %v", suffix, err)
		}
	}
	if got, err := ReclaimableRecoveryBranches(dir, later, RecoveryGCGracePeriod); err != nil || len(got) != 0 {
		t.Fatalf("parent missing = %v err=%v, want none", got, err)
	}
}

func TestRecoveryBranchCoveredByParentReadsActualContent(t *testing.T) {
	dir := t.TempDir()

	_, divergedBranch, _ := forkRecoveryBranch(t, dir, "diverged-proof")
	if RecoveryBranchCoveredByParent(divergedBranch, dir) {
		t.Fatal("diverged parent reported as covering its recovery branch")
	}

	coveredParent, coveredBranch, coveredMsgs := forkRecoveryBranch(t, dir, "covered-proof")
	coverBranchInParent(t, coveredParent, coveredMsgs)
	if !RecoveryBranchCoveredByParent(coveredBranch, dir) {
		t.Fatal("parent containing the recovery transcript did not cover the branch")
	}

	// Restore the fork metadata after continuing the transcript. This models a
	// stale sidecar that still claims content_digest == recovery_digest even
	// though the actual branch has changed.
	staleMeta, ok, err := LoadBranchMeta(coveredBranch)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta stale branch: ok=%v err=%v", ok, err)
	}
	continued, err := LoadSession(coveredBranch)
	if err != nil {
		t.Fatalf("LoadSession stale branch: %v", err)
	}
	continued.Add(provider.Message{Role: provider.RoleAssistant, Content: "continued after sidecar snapshot"})
	if err := continued.Save(coveredBranch); err != nil {
		t.Fatalf("Save continued branch: %v", err)
	}
	if err := SaveBranchMetaPreserveUpdated(coveredBranch, staleMeta); err != nil {
		t.Fatalf("restore stale branch meta: %v", err)
	}
	if RecoveryBranchCoveredByParent(coveredBranch, dir) {
		t.Fatal("stale sidecar authorized cleanup of changed branch content")
	}

	missingParent, missingBranch, missingMsgs := forkRecoveryBranch(t, dir, "missing-proof")
	coverBranchInParent(t, missingParent, missingMsgs)
	if !RecoveryBranchCoveredByParent(missingBranch, dir) {
		t.Fatal("missing-parent fixture was not covered before parent removal")
	}
	for _, suffix := range []string{"", ".events.jsonl", ".meta"} {
		if err := os.Remove(missingParent + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove parent artifact %s: %v", suffix, err)
		}
	}
	if RecoveryBranchCoveredByParent(missingBranch, dir) {
		t.Fatal("missing parent reported as covering its recovery branch")
	}
}

func TestRecoveryParentGuardBlocksRewindAfterValidation(t *testing.T) {
	dir := t.TempDir()
	parentPath, branchPath, branchMsgs := forkRecoveryBranch(t, dir, "rewind-race")
	coverBranchInParent(t, parentPath, branchMsgs)

	guard, err := TryAcquireRecoveryParentGuard(branchPath, dir)
	if err != nil {
		t.Fatalf("TryAcquireRecoveryParentGuard: %v", err)
	}
	// Force the dangerous ordering: coverage validation has completed, then a
	// concurrent rewind tries to take the parent's save lock before purge. The
	// guard must keep that lock unavailable until the caller finishes deleting
	// the redundant branch.
	if lock, err := tryTakeSessionLockFile(store.SessionLockFile(parentPath)); !errors.Is(err, errSessionFileLockHeld) {
		if lock != nil {
			lock.Unlock()
		}
		guard.Release()
		t.Fatalf("parent save lock after coverage validation = %v, want errSessionFileLockHeld", err)
	}
	guard.Release()

	parent, err := LoadSession(parentPath)
	if err != nil {
		t.Fatalf("LoadSession parent: %v", err)
	}
	parent.Messages = append([]provider.Message(nil), parent.Messages[:1]...)
	if err := parent.SaveRewrite(parentPath); err != nil {
		t.Fatalf("SaveRewrite after guard release: %v", err)
	}
	if RecoveryBranchCoveredByParent(branchPath, dir) {
		t.Fatal("rewound parent still reported as covering the recovery branch")
	}
}

func TestRecoveryParentGuardRefusesInFlightParentRewrite(t *testing.T) {
	dir := t.TempDir()
	parentPath, branchPath, branchMsgs := forkRecoveryBranch(t, dir, "rewrite-busy")
	coverBranchInParent(t, parentPath, branchMsgs)

	unlock, err := lockSessionFile(parentPath)
	if err != nil {
		t.Fatalf("lockSessionFile: %v", err)
	}
	defer unlock()
	if guard, err := TryAcquireRecoveryParentGuard(branchPath, dir); !errors.Is(err, ErrSessionLeaseHeld) {
		if guard != nil {
			guard.Release()
		}
		t.Fatalf("guard during parent rewrite err = %v, want ErrSessionLeaseHeld", err)
	}
}
