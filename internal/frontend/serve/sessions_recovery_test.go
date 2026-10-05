package serve

import (
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// Each fork carries a different prefix of the parent, so both stay covered by
// it and the sweep may reclaim either. They land in separate files because one
// writer's branch is only taken over by a save that keeps every turn already
// in it: the shorter fork cannot, so it gets a lane of its own.
func recoveryFork(t *testing.T, root string, messages ...string) string {
	t.Helper()
	fork := sessionstore.NewSession("sys")
	for i, message := range messages {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		fork.Add(provider.Message{Role: role, Content: message})
	}
	info, err := fork.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatal(err)
	}
	return info.Path
}

// Folding the copies out of the listing leaves them on disk. Every other host
// sweeps them; serve kept a conflict loop's forks — one full transcript copy
// each — until the user found them by hand.
func TestSweepRecoveryBranchesTrashesCoveredForks(t *testing.T) {
	dir := testenv.TempDir(t)
	root := filepath.Join(dir, "20260815-161507-deepseek-v4-flash.jsonl")
	saveVisibilitySession(t, root, "今日热点", "好的")
	// The fork the session is still on is written first: the shorter one that
	// follows drops out of its lane rather than truncating it.
	open := recoveryFork(t, root, "今日热点", "好的")
	covered := recoveryFork(t, root, "今日热点")

	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	ctrl.SetSessionPath(open)
	srv := New(ctrl, NewBroadcaster(), config.ServeConfig{})

	if got := srv.sweepRecoveryBranches(0); got != 1 {
		t.Fatalf("swept %d branches, want only the idle covered fork", got)
	}
	if _, err := os.Stat(covered); !os.IsNotExist(err) {
		t.Fatalf("covered fork still in the session directory: %v", err)
	}
	for _, keep := range []string{root, open} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("%s must survive the sweep: %v", filepath.Base(keep), err)
		}
	}
}

// A branch someone continued on holds turns the parent never saw. The sweep
// proves coverage from content, so it must leave that branch where it is.
func TestSweepRecoveryBranchesKeepsContinuedFork(t *testing.T) {
	dir := testenv.TempDir(t)
	root := filepath.Join(dir, "20260815-161507-deepseek-v4-flash.jsonl")
	saveVisibilitySession(t, root, "今日热点", "好的")

	continued := sessionstore.NewSession("sys")
	continued.Add(provider.Message{Role: provider.RoleUser, Content: "今日热点"})
	info, err := continued.SaveConflictRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: root})
	if err != nil {
		t.Fatal(err)
	}
	continued.Add(provider.Message{Role: provider.RoleAssistant, Content: "只有分支里有的回答"})
	if err := continued.Save(info.Path); err != nil {
		t.Fatal(err)
	}

	ctrl := control.New(control.Options{SessionDir: dir})
	defer ctrl.Close()
	ctrl.SetSessionPath(root)
	srv := New(ctrl, NewBroadcaster(), config.ServeConfig{})

	if got := srv.sweepRecoveryBranches(0); got != 0 {
		t.Fatalf("swept %d branches, want none", got)
	}
	if _, err := os.Stat(info.Path); err != nil {
		t.Fatalf("continued fork must survive the sweep: %v", err)
	}
}
