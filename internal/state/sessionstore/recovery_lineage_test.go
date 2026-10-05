package sessionstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

// forkChain builds root ← A ← B ← C: the pile a lane-per-Session build left
// behind, each file carrying the transcript as of that turn. A live writer now
// keeps one lane, so the chain is staged with an explicit lane per turn — the
// GC still has to clean up what is already on disk. The root moves on to
// content no runtime had, so parent coverage never holds for any of them.
func forkChain(t *testing.T, dir string) (root string, branches []string) {
	t.Helper()
	root = filepath.Join(dir, "session.jsonl")
	seed := NewSession("sys")
	seed.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	seed.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
	if err := seed.Save(root); err != nil {
		t.Fatalf("save root: %v", err)
	}
	outside, err := LoadSession(root)
	if err != nil {
		t.Fatalf("outside load: %v", err)
	}
	outside.Add(provider.Message{Role: provider.RoleUser, Content: "outside"})
	if err := outside.Save(root); err != nil {
		t.Fatalf("outside save: %v", err)
	}

	parent := root
	for turn := 2; turn <= 4; turn++ {
		live := NewSession("sys")
		for _, m := range seed.Snapshot() {
			if m.Role != provider.RoleSystem {
				live.Add(m)
			}
		}
		live.Add(provider.Message{Role: provider.RoleUser, Content: "turn"})
		live.Add(provider.Message{Role: provider.RoleAssistant, Content: "reply"})
		live.recoveryLane = newSessionWriterID()
		info, err := live.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: parent})
		if err != nil {
			t.Fatalf("conflict %d: %v", turn, err)
		}
		if info.Path == parent {
			t.Fatalf("conflict %d rewrote its parent in place", turn)
		}
		branches = append(branches, info.Path)
		parent = info.Path
		seed = live
	}
	return root, branches
}

// A branch its successor holds in full preserves nothing of its own. Only
// parent coverage used to count, and the parent is the older file — so a
// conflict chain piled up in the sidebar untouched.
func TestSupersededRecoveryBranchIsReclaimable(t *testing.T) {
	dir := testenv.TempDir(t)
	_, branches := forkChain(t, dir)
	if len(branches) != 3 {
		t.Fatalf("branches = %d, want 3", len(branches))
	}
	latest := branches[len(branches)-1]

	for _, superseded := range branches[:len(branches)-1] {
		if RecoveryBranchCoveredByParent(superseded, dir) {
			t.Fatalf("%s: parent coverage should not hold; the root diverged", filepath.Base(superseded))
		}
		if !recoveryBranchRedundant(superseded, dir) {
			t.Fatalf("%s: branch held in full by its successor was not judged redundant", filepath.Base(superseded))
		}
	}
	if recoveryBranchRedundant(latest, dir) {
		t.Fatal("the newest branch holds turns no other session has; it must be kept")
	}

	got, err := ReclaimableRecoveryBranches(dir, time.Now().Add(RecoveryGCGracePeriod), RecoveryGCGracePeriod)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != len(branches)-1 {
		t.Fatalf("reclaimable = %v, want the %d superseded branches", got, len(branches)-1)
	}
	for _, path := range got {
		if err := TrashCoveredRecoveryBranch(path, dir); err != nil {
			t.Fatalf("trash %s: %v", filepath.Base(path), err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still on disk after trashing (err=%v)", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(latest); err != nil {
		t.Fatalf("trashing the superseded branches took the newest one with it: %v", err)
	}
}

// The successor proof reads content, not lineage metadata: a branch someone
// continued on — or that an outside writer appended to — has turns of its own,
// whatever its parent_id says.
func TestContinuedRecoveryBranchIsNotSuperseded(t *testing.T) {
	dir := testenv.TempDir(t)
	_, branches := forkChain(t, dir)
	continued := branches[0]

	live, err := LoadSession(continued)
	if err != nil {
		t.Fatalf("load branch: %v", err)
	}
	live.Add(provider.Message{Role: provider.RoleUser, Content: "kept working here"})
	if err := live.Save(continued); err != nil {
		t.Fatalf("continue branch: %v", err)
	}

	if recoveryBranchRedundant(continued, dir) {
		t.Fatal("a branch that was continued on must never be judged redundant")
	}
	if err := TrashCoveredRecoveryBranch(continued, dir); err == nil {
		t.Fatal("trash accepted a branch holding unique turns")
	}
	if _, err := os.Stat(continued); err != nil {
		t.Fatalf("continued branch was removed: %v", err)
	}
}

// Two copies of one transcript each hold everything the other does. Coverage
// alone would let them prove each other redundant and remove both.
func TestIdenticalRecoveryCopiesKeepOne(t *testing.T) {
	dir := testenv.TempDir(t)
	root := filepath.Join(dir, "session.jsonl")
	seed := NewSession("sys")
	seed.Add(provider.Message{Role: provider.RoleUser, Content: "root only"})
	if err := seed.Save(root); err != nil {
		t.Fatalf("save root: %v", err)
	}

	copies := []string{filepath.Join(dir, "session-recovery-aaaa.jsonl"), filepath.Join(dir, "session-recovery-bbbb.jsonl")}
	for _, path := range copies {
		live := NewSession("sys")
		live.Add(provider.Message{Role: provider.RoleUser, Content: "unsaved"})
		live.Add(provider.Message{Role: provider.RoleAssistant, Content: "work"})
		if err := live.Save(path); err != nil {
			t.Fatalf("save copy: %v", err)
		}
		digest, err := DigestSessionMessages(live.Snapshot())
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if err := SaveBranchMeta(path, BranchMeta{
			ID: BranchID(path), ParentID: BranchID(root), Recovered: true,
			RecoveryDigest: digestString(digest), RecoveryDepth: 1,
		}); err != nil {
			t.Fatalf("save meta: %v", err)
		}
	}

	got, err := ReclaimableRecoveryBranches(dir, time.Now().Add(RecoveryGCGracePeriod), RecoveryGCGracePeriod)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("reclaimable = %v, want exactly one of the two identical copies", got)
	}
	if err := TrashCoveredRecoveryBranch(got[0], dir); err != nil {
		t.Fatalf("trash: %v", err)
	}
	survivor := copies[0]
	if got[0] == survivor {
		survivor = copies[1]
	}
	if _, err := os.Stat(survivor); err != nil {
		t.Fatalf("the surviving copy is gone: %v", err)
	}
	if recoveryBranchRedundant(survivor, dir) {
		t.Fatal("the last copy of these turns must not be judged redundant")
	}
}
