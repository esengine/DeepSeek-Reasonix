package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// What the workspace gains after the session opened — a nested repository
// with its own local filter — never runs, and the snapshot stays the source's.

func plainGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// nestedRepoWithFilter makes dir a repository whose f.txt is filtered by a
// locally configured marker-writing program, and returns the marker.
func nestedRepoWithFilter(t *testing.T, dir string) (marker string) {
	t.Helper()
	plainGit(t, dir, "init", "-q")
	writeRepoFile(t, dir, ".gitattributes", "f.txt filter=pwn\n")
	writeRepoFile(t, dir, "f.txt", "a\n")
	plainGit(t, dir, "add", "-A")
	plainGit(t, dir, "commit", "-q", "-m", "nested")
	marker = filepath.Join(testenv.TempDir(t), "executed")
	payload := filepath.Join(testenv.TempDir(t), "payload.sh")
	if err := os.WriteFile(payload, []byte("#!/bin/sh\necho ran >> '"+marker+"'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plainGit(t, dir, "config", "filter.pwn.clean", payload)
	plainGit(t, dir, "config", "filter.pwn.smudge", payload)
	writeRepoFile(t, dir, "f.txt", "b\n")
	return marker
}

func requirePOSIXPayload(t *testing.T) {
	t.Helper()
	requireGit(t)
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
}

func TestSnapshotDoesNotEnterGitlinkRepository(t *testing.T) {
	requirePOSIXPayload(t)
	repo := initRepo(t)
	session := opened(t, repo)
	sub := filepath.Join(repo, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := nestedRepoWithFilter(t, sub)
	gitlink := plainGit(t, sub, "rev-parse", "HEAD")
	plainGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+gitlink+",sub")
	writeRepoFile(t, repo, "new.txt", "new\n")

	snap, err := TakeSnapshot(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	markerAbsent(t, marker, "TakeSnapshot")
	tree := plainGit(t, repo, "ls-tree", "-r", snap.Tree)
	for _, want := range []string{"160000 commit " + gitlink + "\tsub", "\tnew.txt", "\tREADME.md"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("snapshot tree:\n%s\nwant %q", tree, want)
		}
	}
	if strings.Contains(tree, "sub/") {
		t.Fatalf("snapshot tree:\n%s\nwant the gitlink's contents left out", tree)
	}
	cand, err := CreateCandidate(context.Background(), snap, testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, changes, err := CandidateTree(context.Background(), snap, cand); err != nil || len(changes) != 0 {
		t.Fatalf("untouched candidate = %v, %v; want no changes", changes, err)
	}
	markerAbsent(t, marker, "CandidateTree")
}

// The workspace is a subdirectory of a repository and gains its own .git.
func TestSnapshotKeepsRepositoryResolvedAtOpen(t *testing.T) {
	requirePOSIXPayload(t)
	repo := initRepo(t)
	pkg := filepath.Join(repo, "pkg")
	writeRepoFile(t, repo, "pkg/f.txt", "a\n")
	plainGit(t, repo, "add", "-A")
	plainGit(t, repo, "commit", "-q", "-m", "pkg")
	session := opened(t, pkg)
	marker := nestedRepoWithFilter(t, pkg)

	snap, err := TakeSnapshot(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	markerAbsent(t, marker, "TakeSnapshot")
	if snap.RepoRoot != session.WorkTree || snap.Prefix != "pkg" {
		t.Fatalf("snapshot of %s/%s, want %s/pkg", snap.RepoRoot, snap.Prefix, session.WorkTree)
	}
	if got := plainGit(t, repo, "show", snap.Tree+":pkg/f.txt"); got != "b" {
		t.Fatalf("pkg/f.txt in snapshot = %q, want the working copy", got)
	}
}

// A same-size edit made in the second the index was last written is still in
// the snapshot, however much later the snapshot is taken.
func TestSnapshotSeesSameSizeEditFromTheIndexSecond(t *testing.T) {
	requireGit(t)
	repo := initRepo(t)
	session := opened(t, repo)
	index := filepath.Join(repo, ".git", "index")
	for attempt := 0; ; attempt++ {
		writeRepoFile(t, repo, "f.txt", "a\n")
		plainGit(t, repo, "add", "f.txt")
		writeRepoFile(t, repo, "f.txt", "b\n")
		idx, err := os.Stat(index)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Stat(filepath.Join(repo, "f.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if idx.ModTime().Unix() == file.ModTime().Unix() {
			break
		}
		if attempt == 5 {
			t.Skip("could not land the edit in the index's second")
		}
	}
	time.Sleep(1100 * time.Millisecond)
	snap, err := TakeSnapshot(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if got := plainGit(t, repo, "show", snap.Tree+":f.txt"); got != "b" {
		t.Fatalf("f.txt in snapshot = %q, want the same-size edit", got)
	}
}
