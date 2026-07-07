package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
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
