package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/fileutil"
)

type applyCrashFixture struct {
	Snapshot Snapshot
	Tree     string
	Changes  []Change
}

func TestApplyProcessCrashHelper(t *testing.T) {
	path := os.Getenv("REASONIX_APPLY_CRASH_FIXTURE")
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f applyCrashFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	fileutil.CrashPoint = func(op, path string) {
		if op == "atomic-write" && path == filepath.Join(f.Snapshot.RepoRoot, "b.txt") {
			os.Exit(77)
		}
	}
	if err := ApplyPaths(t.Context(), f.Snapshot, f.Tree, f.Changes); err != nil {
		t.Fatal(err)
	}
	t.Fatal("child did not reach the crash boundary")
}

func TestApplyRecoversAfterProcessExit(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate a\n")
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
	tree, changes := finish()
	raw, err := json.Marshal(applyCrashFixture{snap, tree, changes})
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(fixture, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestApplyProcessCrashHelper$")
	cmd.Env = append(os.Environ(), "REASONIX_APPLY_CRASH_FIXTURE="+fixture)
	out, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 77 {
		t.Fatalf("child exit = %v\n%s", err, out)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate a\n" {
		t.Fatalf("first publication missing: %q", got)
	}
	if err := CheckApplyRecovery(snap.Repo); !errors.Is(err, ErrApplyRecovery) {
		t.Fatalf("dead owner did not fence new writes: %v", err)
	}
	paths, err := ApplyRecoveryPaths(snap)
	if err != nil || len(paths) != 2 {
		t.Fatalf("recovery scope: %v %v", paths, err)
	}
	if err := ApplyPaths(authorizedRecovery(t, snap), snap, tree, changes); err != nil {
		t.Fatal(err)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "candidate b\n" {
		t.Fatalf("recovery: %q", got)
	}
	if err := CheckApplyRecovery(snap.Repo); err != nil {
		t.Fatalf("settled recovery remained fenced: %v", err)
	}
}
