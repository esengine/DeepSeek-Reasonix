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
	merged, err := LoadSession(parentPath)
	if err != nil {
		t.Fatalf("Load covering parent: %v", err)
	}
	merged.Replace(append([]provider.Message(nil), branchMsgs...))
	merged.Add(provider.Message{Role: provider.RoleAssistant, Content: "answered after recovery"})
	if err := merged.SaveRewrite(parentPath); err != nil {
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
	for _, artifact := range append([]string{parentPath}, store.SessionSidecarFiles(parentPath)...) {
		if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove parent artifact %s: %v", artifact, err)
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
	for _, artifact := range append([]string{missingParent}, store.SessionSidecarFiles(missingParent)...) {
		if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove parent artifact %s: %v", artifact, err)
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
	if lock, err := tryTakeSessionLockFile(store.SessionLockFile(parentPath)); !errors.Is(err, ErrSessionFileLockHeld) {
		if lock != nil {
			lock.Unlock()
		}
		guard.Release()
		t.Fatalf("parent save lock after coverage validation = %v, want ErrSessionFileLockHeld", err)
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

func ageRecoveryBranchForGC(t *testing.T, path string) {
	t.Helper()
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta: ok=%v err=%v", ok, err)
	}
	meta.UpdatedAt = time.Now().Add(-2 * RecoveryGCGracePeriod)
	if err := SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		t.Fatalf("age recovery meta: %v", err)
	}
}

func TestTrashReclaimableRecoveryBranchUsesRecoverableDesktopLayout(t *testing.T) {
	dir := t.TempDir()
	parentPath, branchPath, branchMsgs := forkRecoveryBranch(t, dir, "trash-covered")
	coverBranchInParent(t, parentPath, branchMsgs)
	ageRecoveryBranchForGC(t, branchPath)

	if err := TrashReclaimableRecoveryBranch(branchPath, dir); err != nil {
		t.Fatalf("TrashReclaimableRecoveryBranch: %v", err)
	}
	if _, err := os.Stat(branchPath); !os.IsNotExist(err) {
		t.Fatalf("live recovery transcript still exists: %v", err)
	}
	if IsCleanupPending(branchPath) {
		t.Fatal("cleanup-pending marker remained after completed trash move")
	}
	itemDir := filepath.Join(dir, recoveryTrashDir, filepath.Base(branchPath))
	for _, path := range []string{
		filepath.Join(itemDir, filepath.Base(branchPath)),
		filepath.Join(itemDir, filepath.Base(BranchMetaPath(branchPath))),
		filepath.Join(itemDir, filepath.Base(store.SessionEventLog(branchPath))),
		filepath.Join(itemDir, recoveryTrashMetaFile),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("recoverable trash artifact %s: %v", path, err)
		}
	}
	if _, err := os.Stat(parentPath); err != nil {
		t.Fatalf("parent session changed by recovery trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(itemDir, recoveryTrashPendingFile)); !os.IsNotExist(err) {
		t.Fatalf("completed trash entry retained pending marker: %v", err)
	}
}

func TestTrashReclaimableRecoveryBranchEnforcesGraceAtFinalGuard(t *testing.T) {
	dir := t.TempDir()
	parentPath, branchPath, branchMsgs := forkRecoveryBranch(t, dir, "trash-fresh")
	coverBranchInParent(t, parentPath, branchMsgs)

	if err := TrashReclaimableRecoveryBranch(branchPath, dir); !errors.Is(err, ErrRecoveryBranchNotIdle) {
		t.Fatalf("fresh recovery trash err = %v, want ErrRecoveryBranchNotIdle", err)
	}
	if _, err := os.Stat(branchPath); err != nil {
		t.Fatalf("fresh recovery branch was not preserved: %v", err)
	}
}

func TestInterruptedRecoveryTrashStageReconcilesBeforePublication(t *testing.T) {
	dir := t.TempDir()
	_, branchPath, _ := forkRecoveryBranch(t, dir, "staged-crash")
	key := filepath.Base(branchPath)
	stageDir, err := reserveRecoveryTrashStage(dir)
	if err != nil {
		t.Fatalf("reserveRecoveryTrashStage: %v", err)
	}
	if err := prepareRecoveryTrashStage(branchPath, key, stageDir); err != nil {
		t.Fatalf("prepareRecoveryTrashStage: %v", err)
	}
	if IsCleanupPending(branchPath) {
		t.Fatal("live cleanup marker should not be used by the staging protocol")
	}
	if _, err := os.Stat(branchPath); !os.IsNotExist(err) {
		t.Fatalf("live transcript still exists after staging: %v", err)
	}
	if store.IsSessionTranscriptName(filepath.Base(stageDir)) {
		t.Fatalf("staging directory is Desktop-visible by name: %s", stageDir)
	}
	for _, target := range []string{
		filepath.Join(stageDir, key),
		filepath.Join(stageDir, recoveryTrashPendingFile),
	} {
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("staged recovery artifact %s: %v", target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stageDir, recoveryTrashMetaFile)); !os.IsNotExist(err) {
		t.Fatalf("incomplete stage became Desktop-visible through trash metadata: %v", err)
	}

	// Simulate a process crash after the transcript rename but before any
	// sidecars moved. Startup reconciliation must finish the hidden stage and
	// publish one complete Desktop trash item without the hard-delete callback.
	calledFallback := false
	if err := ReconcileCleanupPending(dir, func(CleanupPendingInfo) error {
		calledFallback = true
		return errors.New("hard-delete fallback must not run")
	}); err != nil {
		t.Fatalf("ReconcileCleanupPending: %v", err)
	}
	if calledFallback {
		t.Fatal("recovery trash stage reached hard-delete fallback")
	}
	if _, err := os.Stat(stageDir); !os.IsNotExist(err) {
		t.Fatalf("staging directory remained after publication: %v", err)
	}
	itemDir := filepath.Join(dir, recoveryTrashDir, key)
	for _, target := range []string{
		filepath.Join(itemDir, key),
		filepath.Join(itemDir, filepath.Base(BranchMetaPath(branchPath))),
		filepath.Join(itemDir, filepath.Base(store.SessionEventLog(branchPath))),
		filepath.Join(itemDir, recoveryTrashMetaFile),
	} {
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("reconciled recovery artifact %s: %v", target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(itemDir, recoveryTrashPendingFile)); !os.IsNotExist(err) {
		t.Fatalf("published trash entry retained pending marker: %v", err)
	}
}

func TestReconcileCleanupPendingFinishesRecoveryTrashWithoutHardDeleteCallback(t *testing.T) {
	dir := t.TempDir()
	_, branchPath, _ := forkRecoveryBranch(t, dir, "trash-interrupted")
	key := filepath.Base(branchPath)
	itemName, itemDir, err := reserveRecoveryTrashItemDir(dir, key)
	if err != nil {
		t.Fatalf("reserveRecoveryTrashItemDir: %v", err)
	}
	if err := prepareRecoveryTrashEntry(branchPath, key, itemDir); err != nil {
		t.Fatalf("prepare trash entry before simulated crash: %v", err)
	}
	if err := MarkCleanupPending(branchPath, recoveryTrashOperationPrefix+itemName); err != nil {
		t.Fatalf("MarkCleanupPending: %v", err)
	}

	calledFallback := false
	if err := ReconcileCleanupPending(dir, func(CleanupPendingInfo) error {
		calledFallback = true
		return errors.New("hard-delete fallback must not run")
	}); err != nil {
		t.Fatalf("ReconcileCleanupPending: %v", err)
	}
	if calledFallback {
		t.Fatal("recovery-trash marker reached hard-delete fallback")
	}
	if IsCleanupPending(branchPath) {
		t.Fatal("cleanup-pending marker remained after reconciliation")
	}
	if _, err := os.Stat(filepath.Join(itemDir, recoveryTrashMetaFile)); err != nil {
		t.Fatalf("reconciled trash metadata: %v", err)
	}
}
