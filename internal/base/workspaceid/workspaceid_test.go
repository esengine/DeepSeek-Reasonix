package workspaceid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func sharesRepo(a, b string) bool {
	repo := RepoDir(a)
	return repo != "" && repo == RepoDir(b)
}

func TestKeyPathWhenNotARepository(t *testing.T) {
	dir := testenv.TempDir(t)
	key := Key(dir)
	if !strings.HasPrefix(key, PathPrefix) {
		t.Fatalf("Key(%q) = %q, want a %s key", dir, key, PathPrefix)
	}
	if RepoDir(dir) != "" {
		t.Fatalf("RepoDir(%q) = %q, want empty", dir, RepoDir(dir))
	}
}

func TestKeyRepoForRootAndSubdirectory(t *testing.T) {
	repo := testenv.TempDir(t)
	mkdir(t, filepath.Join(repo, ".git"))
	sub := filepath.Join(repo, "internal", "agent")
	mkdir(t, sub)

	rootKey := Key(repo)
	if !strings.HasPrefix(rootKey, RepoPrefix) {
		t.Fatalf("Key(repo) = %q, want a %s key", rootKey, RepoPrefix)
	}
	if got := Key(sub); got != rootKey {
		t.Fatalf("Key(subdir) = %q, want the repository key %q", got, rootKey)
	}
}

// A linked worktree must land on the same key as the tree it was cut from, or
// a switch flipped in the main tree would silently not apply in the worktree.
func TestKeyLinkedWorktreeSharesTheRepositoryKey(t *testing.T) {
	base := testenv.TempDir(t)
	repo := filepath.Join(base, "repo")
	gitDir := filepath.Join(repo, ".git")
	mkdir(t, gitDir)
	mkdir(t, filepath.Join(gitDir, "worktrees", "studio"))

	tree := filepath.Join(base, "trees", "studio")
	mkdir(t, tree)
	writeFile(t, filepath.Join(tree, ".git"),
		"gitdir: "+filepath.Join(gitDir, "worktrees", "studio")+"\n")

	if got, want := Key(tree), Key(repo); got != want {
		t.Fatalf("worktree key = %q, main tree key = %q, want equal", got, want)
	}
	if !sharesRepo(tree, repo) {
		t.Fatal("sharesRepo(worktree, repo) = false, want true")
	}
}

func TestKeyRelativeGitdirPointer(t *testing.T) {
	base := testenv.TempDir(t)
	repo := filepath.Join(base, "repo")
	mkdir(t, filepath.Join(repo, ".git", "worktrees", "wt"))
	tree := filepath.Join(base, "wt")
	mkdir(t, tree)
	writeFile(t, filepath.Join(tree, ".git"), "gitdir: ../repo/.git/worktrees/wt\n")

	if got, want := Key(tree), Key(repo); got != want {
		t.Fatalf("relative-pointer worktree key = %q, want %q", got, want)
	}
}

// A submodule is its own repository: its gitdir has no worktrees element, so it
// must not converge onto the superproject's key.
func TestKeySubmoduleStaysItsOwnRepository(t *testing.T) {
	base := testenv.TempDir(t)
	super := filepath.Join(base, "super")
	mkdir(t, filepath.Join(super, ".git", "modules", "vendor"))
	sub := filepath.Join(super, "vendor")
	mkdir(t, sub)
	writeFile(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/vendor\n")

	if Key(sub) == Key(super) {
		t.Fatalf("submodule shares the superproject key %q", Key(sub))
	}
	if !strings.HasPrefix(Key(sub), RepoPrefix) {
		t.Fatalf("Key(submodule) = %q, want a %s key", Key(sub), RepoPrefix)
	}
}

// Two plain folders are two projects. Walking up for a repository must not
// collapse them onto the drive the walk ended on.
func TestKeyDistinctPerFolderOutsideRepositories(t *testing.T) {
	a, b := testenv.TempDir(t), testenv.TempDir(t)
	if Key(a) == Key(b) {
		t.Fatalf("two plain folders share key %q", Key(a))
	}
	if Key(a) != Key(a+string(filepath.Separator)) {
		t.Fatal("Key is not stable across spellings of one folder")
	}
}

func TestKeyEmptyRoot(t *testing.T) {
	if got := Key("  "); got != "" {
		t.Fatalf("Key(blank) = %q, want empty", got)
	}
}

func TestSharesRepoIsFalseOutsideRepositories(t *testing.T) {
	a, b := testenv.TempDir(t), testenv.TempDir(t)
	if sharesRepo(a, b) {
		t.Fatal("SharesRepo on two plain folders = true, want false")
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
