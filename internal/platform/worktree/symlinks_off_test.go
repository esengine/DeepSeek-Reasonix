package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// plainLinkRepo is a repository with core.symlinks=false, as Git for Windows
// installs by default: "link" is a 120000 entry Git checked out as a regular
// file holding its target text.
func plainLinkRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := testenv.TempDir(t)
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "config", "core.symlinks", "false")
	writeRepoFile(t, repo, "a.txt", "one\n")
	writeRepoFile(t, repo, "link", "a.txt")
	gitIn(t, repo, "add", "-A")
	blob := strings.TrimSpace(gitIn(t, repo, "hash-object", "-w", "--no-filters", "link"))
	gitIn(t, repo, "update-index", "--cacheinfo", "120000,"+blob+",link")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	if err := os.Remove(filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "checkout", "--", "link")
	requirePlainLink(t, repo, "a.txt")
	return repo
}

func requirePlainLink(t *testing.T, dir, target string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(dir, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("link is %v, want a regular file holding the link text", info.Mode())
	}
	if got := readRepoFile(t, dir, "link"); got != target {
		t.Fatalf("link holds %q, want %q", got, target)
	}
}

func plainLinkCandidate(t *testing.T, repo string) (Snapshot, Candidate) {
	t.Helper()
	ctx := context.Background()
	snap, err := TakeSnapshot(ctx, opened(t, repo))
	if err != nil {
		t.Fatalf("TakeSnapshot = %v", err)
	}
	c, err := CreateCandidate(ctx, snap, filepath.Join(testenv.TempDir(t), "isolated"))
	if err != nil {
		t.Fatalf("CreateCandidate = %v", err)
	}
	t.Cleanup(func() { _ = RemoveCandidate(ctx, snap, c) })
	return snap, c
}

func TestSnapshotKeepsAPlainFileLinkWhenSymlinksAreOff(t *testing.T) {
	repo := plainLinkRepo(t)
	snap, c := plainLinkCandidate(t, repo)
	head := strings.Fields(gitIn(t, repo, "ls-tree", "HEAD", "link"))
	got := strings.Fields(gitIn(t, repo, "ls-tree", snap.Tree, "link"))
	if !slices.Equal(got, head) || got[0] != "120000" {
		t.Fatalf("snapshot recorded link as %v, want %v", got, head)
	}
	requirePlainLink(t, c.WorktreeRoot, "a.txt")
}

func TestLiveBlobsHashAPlainFileLinkAsTheLink(t *testing.T) {
	repo := plainLinkRepo(t)
	source := opened(t, repo)
	ctx := context.Background()
	want, err := treeBlobs(ctx, source, "HEAD", []string{"link", "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	live, err := liveBlobs(ctx, source, []string{"link", "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if live["link"] != want["link"] || live["a.txt"] != want["a.txt"] {
		t.Fatalf("live = %v, want %v", live, want)
	}
}

func TestApplyPathsPublishesALinkAsAPlainFileWhenSymlinksAreOff(t *testing.T) {
	repo := plainLinkRepo(t)
	snap, c := plainLinkCandidate(t, repo)
	writeRepoFile(t, c.WorktreeRoot, "link", "b.txt")
	writeRepoFile(t, c.WorktreeRoot, "a.txt", "candidate\n")
	tree, changes, err := CandidateTree(context.Background(), snap, c)
	if err != nil {
		t.Fatal(err)
	}
	if mode := strings.Fields(gitIn(t, repo, "ls-tree", tree, "link"))[0]; mode != "120000" {
		t.Fatalf("candidate recorded link as %s", mode)
	}
	if err := ApplyPaths(context.Background(), snap, tree, changes); err != nil {
		t.Fatalf("ApplyPaths = %v", err)
	}
	requirePlainLink(t, repo, "b.txt")
	if got := readRepoFile(t, repo, "a.txt"); got != "candidate\n" {
		t.Fatalf("a.txt = %q", got)
	}
	if status := gitIn(t, repo, "status", "--porcelain", "--", "link"); !strings.HasPrefix(status, " M") {
		t.Fatalf("git reads the published link as %q, want a modified link", status)
	}
}

func TestApplyPathsJudgesAPlainFileLinkTheWorkspaceChanged(t *testing.T) {
	for _, tc := range []struct {
		name, workspace string
		conflict        bool
	}{
		{name: "already at the target", workspace: "b.txt"},
		{name: "changed elsewhere", workspace: "c.txt", conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := plainLinkRepo(t)
			snap, c := plainLinkCandidate(t, repo)
			writeRepoFile(t, c.WorktreeRoot, "link", "b.txt")
			tree, changes, err := CandidateTree(context.Background(), snap, c)
			if err != nil {
				t.Fatal(err)
			}
			writeRepoFile(t, repo, "link", tc.workspace)
			err = ApplyPaths(context.Background(), snap, tree, changes)
			var conflict *ConflictError
			switch {
			case tc.conflict && (!errors.As(err, &conflict) || !slices.Equal(conflict.Paths, []string{"link"})):
				t.Fatalf("ApplyPaths = %v, want a conflict on link", err)
			case !tc.conflict && err != nil:
				t.Fatalf("ApplyPaths = %v", err)
			}
			requirePlainLink(t, repo, tc.workspace)
		})
	}
}
