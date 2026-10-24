package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/fileutil"
)

func TestApplyCrashRecoversBeforeRetry(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate a\n")
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
	tree, changes := finish()
	crashApplyAt(t, filepath.Join(snap.RepoRoot, "b.txt"), func() {
		_ = ApplyPaths(context.Background(), snap, tree, changes)
	})
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate a\n" {
		t.Fatalf("crash did not follow first publication: %q", got)
	}
	if err := ApplyPaths(authorizedRecovery(t, snap), snap, tree, changes); err != nil {
		t.Fatal(err)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "candidate b\n" {
		t.Fatalf("retry did not finish: %q", got)
	}
}

func TestApplyRecoveryPreservesExternalEdit(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate a\n")
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
	tree, changes := finish()
	crashApplyAt(t, filepath.Join(snap.RepoRoot, "b.txt"), func() {
		_ = ApplyPaths(context.Background(), snap, tree, changes)
	})
	writeRepoFile(t, repo, "a.txt", "external edit\n")
	if err := ApplyPaths(authorizedRecovery(t, snap), snap, tree, changes); !errors.Is(err, ErrApplyRecovery) {
		t.Fatalf("retry = %v, want retained recovery", err)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "external edit\n" {
		t.Fatalf("recovery overwrote external edit: %q", got)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "two\n" {
		t.Fatalf("blocked recovery started new writes: %q", got)
	}
}

func authorizedRecovery(t *testing.T, snap Snapshot) context.Context {
	t.Helper()
	return WithRecoveryAuthorization(t.Context(), func(paths []string) error {
		for _, path := range paths {
			if !fileutil.AtOrUnder(path, snap.RepoRoot) {
				return ErrOutsideWorkspace
			}
		}
		return nil
	})
}

func TestRecoveryRechecksScopeAfterJournalLock(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate a\n")
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
	tree, changes := finish()
	checked := false
	ctx := WithRecoveryAuthorization(t.Context(), func(paths []string) error {
		checked = true
		for _, path := range paths {
			if path != filepath.Join(snap.RepoRoot, "b.txt") {
				return ErrOutsideWorkspace
			}
		}
		return nil
	})
	crashApplyAt(t, filepath.Join(snap.RepoRoot, "b.txt"), func() { _ = ApplyPaths(t.Context(), snap, tree, changes) })
	err := ApplyPaths(ctx, snap, tree, []Change{{Status: "M", Path: "b.txt"}})
	if !checked || !errors.Is(err, ErrApplyRecovery) {
		t.Fatalf("late journal escaped scope: checked=%v err=%v", checked, err)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate a\n" {
		t.Fatalf("unclaimed path rolled back: %q", got)
	}
	if got := readRepoFile(t, repo, "b.txt"); got != "two\n" {
		t.Fatalf("new apply continued: %q", got)
	}
}

func TestRecoveryPreviewCannotRecoverAChangedOrCompletedJournal(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "a.txt", "candidate a\n")
	writeRepoFile(t, c.WorkspaceRoot, "b.txt", "candidate b\n")
	tree, changes := finish()
	crashApplyAt(t, filepath.Join(snap.RepoRoot, "b.txt"), func() { _ = ApplyPaths(t.Context(), snap, tree, changes) })
	preview, err := PreviewRecovery(snap.Repo)
	if err != nil || preview == nil {
		t.Fatalf("no recovery preview: %+v %v", preview, err)
	}
	j, err := readApplyJournal(snap.Repo)
	if err != nil {
		t.Fatal(err)
	}
	j.AttemptID = "replacement-attempt"
	if err := j.save(journalPath(snap.Repo)); err != nil {
		t.Fatal(err)
	}
	if err := RecoverIntegrationExpected(authorizedRecovery(t, snap), snap.Repo, preview.Revision); !errors.Is(err, ErrRecoveryChanged) {
		t.Fatalf("stale preview authorized replacement: %v", err)
	}
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate a\n" {
		t.Fatal(got)
	}
	fresh, err := PreviewRecovery(snap.Repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecoverIntegrationExpected(authorizedRecovery(t, snap), snap.Repo, fresh.Revision); err != nil {
		t.Fatal(err)
	}
	if err := RecoverIntegrationExpected(authorizedRecovery(t, snap), snap.Repo, fresh.Revision); !errors.Is(err, ErrRecoveryChanged) {
		t.Fatalf("removed journal accepted stale action: %v", err)
	}
}

func TestApplyPathsDoesNotFollowParentSymlink(t *testing.T) {
	repo, snap, c, finish := isolatedCandidate(t)
	writeRepoFile(t, c.WorkspaceRoot, "new/file.txt", "candidate\n")
	tree, changes := finish()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(repo, "new")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := ApplyPaths(context.Background(), snap, tree, changes); err == nil {
		t.Fatal("applied through external parent symlink")
	}
	if _, err := os.Stat(filepath.Join(external, "file.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrote outside workspace: %v", err)
	}
}

func crashApplyAt(t *testing.T, path string, run func()) {
	t.Helper()
	prior := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = prior }()
	fileutil.CrashPoint = func(op, destination string) {
		if op == "atomic-write" && destination == path {
			panic("simulated process loss")
		}
	}
	crashed := false
	func() {
		defer func() { crashed = recover() != nil }()
		run()
	}()
	if !crashed {
		t.Fatal("publication did not reach injected crash boundary")
	}
}
