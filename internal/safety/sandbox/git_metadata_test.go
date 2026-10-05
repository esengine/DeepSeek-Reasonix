//go:build !windows

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
}

// gitIn runs host git with no global or system configuration.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = isolatedGitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func isolatedGitEnv() []string {
	return append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
}

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(testenv.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func newRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "initial")
}

func metadataPaths(roots, writable []string) (paths, pins []string) {
	meta := gitMetadataWithin(roots, writable)
	all := meta.Paths
	for _, common := range meta.Commons {
		all = append(all, gitGroupPaths(common, gitGroupMaxEntries)...)
	}
	for _, p := range all {
		switch {
		case p.Pin:
			pins = append(pins, p.Path)
		case p.Tree:
			paths = append(paths, p.Path+string(filepath.Separator))
		default:
			paths = append(paths, p.Path)
		}
	}
	return paths, pins
}

func requirePaths(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Fatalf("missing %s in %v", w, got)
		}
	}
}

func TestGitMetadataOfOrdinaryRepository(t *testing.T) {
	requireGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	git := filepath.Join(ws, ".git")
	paths, pins := metadataPaths([]string{ws}, []string{ws})
	requirePaths(t, paths, filepath.Join(git, "config"), filepath.Join(git, "config.worktree"),
		filepath.Join(git, "commondir"), filepath.Join(git, "hooks")+string(filepath.Separator))
	requirePaths(t, pins, ws, git)
	for _, p := range paths {
		if strings.HasPrefix(p, filepath.Join(git, "objects")) || strings.HasPrefix(p, filepath.Join(git, "refs")) || p == filepath.Join(git, "HEAD") {
			t.Fatalf("ordinary git state must stay writable: %s", p)
		}
	}
}

func TestGitMetadataDiscoversFromSubdirectory(t *testing.T) {
	requireGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	sub := filepath.Join(ws, "pkg", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	paths, _ := metadataPaths([]string{sub}, []string{ws})
	requirePaths(t, paths, filepath.Join(ws, ".git", "config"))
	if paths, _ := metadataPaths([]string{sub}, []string{sub}); len(paths) != 0 {
		t.Fatalf("metadata outside every writable directory needs no rule: %v", paths)
	}
}

func TestGitMetadataFollowsGitFileToSeparateGitDir(t *testing.T) {
	requireGit(t)
	root := realTempDir(t)
	ws := filepath.Join(root, "ws")
	store := filepath.Join(root, "store.git")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, ws, "init", "-q", "--separate-git-dir", store)
	paths, pins := metadataPaths([]string{ws}, []string{root})
	requirePaths(t, paths, filepath.Join(ws, ".git"), filepath.Join(store, "config"), filepath.Join(store, "hooks")+string(filepath.Separator))
	requirePaths(t, pins, store)
}

// A `.git` file names whatever git will read. Pointing it at a decoy moves the
// protection with it, and the pointer itself is protected, so there is no
// second, unprotected config git would read instead.
func TestGitMetadataProtectsWhatThePointerNames(t *testing.T) {
	root := realTempDir(t)
	ws := filepath.Join(root, "ws")
	decoy := filepath.Join(root, "decoy")
	for _, d := range []string{ws, decoy} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, ".git"), []byte("gitdir: ../decoy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, _ := metadataPaths([]string{ws}, []string{root})
	requirePaths(t, paths, filepath.Join(ws, ".git"), filepath.Join(decoy, "config"), filepath.Join(decoy, "hooks")+string(filepath.Separator))
}

func TestGitMetadataResolvesSymlinkedGitDir(t *testing.T) {
	requireGit(t)
	root := realTempDir(t)
	real := filepath.Join(root, "real")
	newRepo(t, real)
	ws := filepath.Join(root, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, ".git"), filepath.Join(ws, ".git")); err != nil {
		t.Fatal(err)
	}
	paths, pins := metadataPaths([]string{ws}, []string{root})
	requirePaths(t, paths, filepath.Join(real, ".git", "config"))
	requirePaths(t, pins, filepath.Join(ws, ".git"))
}

func TestGitMetadataOfLinkedWorktreeAndSubmodule(t *testing.T) {
	requireGit(t)
	root := realTempDir(t)
	main := filepath.Join(root, "main")
	newRepo(t, main)
	lib := filepath.Join(root, "lib")
	newRepo(t, lib)
	gitIn(t, main, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	linked := filepath.Join(root, "linked")
	gitIn(t, main, "worktree", "add", "-q", "-b", "linked", linked)
	common := filepath.Join(main, ".git")
	wtDir := filepath.Join(common, "worktrees", "linked")

	paths, _ := metadataPaths([]string{linked}, []string{root})
	requirePaths(t, paths,
		filepath.Join(linked, ".git"),
		filepath.Join(wtDir, "commondir"), filepath.Join(wtDir, "config.worktree"),
		filepath.Join(common, "config"), filepath.Join(common, "hooks")+string(filepath.Separator),
		filepath.Join(common, "modules", "vendor", "lib", "config"),
		filepath.Join(common, "modules", "vendor", "lib", "hooks")+string(filepath.Separator))

	paths, _ = metadataPaths([]string{main}, []string{main})
	requirePaths(t, paths, filepath.Join(wtDir, "config"), filepath.Join(wtDir, "config.worktree"))
}

func TestGitMetadataWithoutRepository(t *testing.T) {
	ws := realTempDir(t)
	if paths, pins := metadataPaths([]string{ws}, []string{ws}); len(paths)+len(pins) != 0 {
		t.Fatalf("no repository, no rules: %v %v", paths, pins)
	}
	if got := GitMetadataPaths(Spec{Mode: "off", WriteRoots: []string{ws}}); len(got) != 0 {
		t.Fatalf("an unconfined spec needs no git rules: %v", got)
	}
}

func TestGitPointerRejectsMalformedFiles(t *testing.T) {
	dir := realTempDir(t)
	for name, body := range map[string]string{
		"noprefix": "../elsewhere\n",
		"empty":    "gitdir: \n",
		"large":    "gitdir: " + strings.Repeat("a", gitFileMaxBytes) + "\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, ok := readGitPointer(path, "gitdir: "); ok {
			t.Fatalf("%s: accepted %q", name, got)
		}
	}
	ws := filepath.Join(dir, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".git"), []byte("not a pointer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, _ := metadataPaths([]string{ws}, []string{ws})
	if !slices.Equal(paths, []string{filepath.Join(ws, ".git")}) {
		t.Fatalf("an unreadable pointer still pins itself: %v", paths)
	}
}

// Every symlink on the way from a `.git` pointer to its gitdir is pinned:
// swapping one would redirect git without touching a protected path.
func TestGitMetadataPinsSymlinkComponentsOfThePointer(t *testing.T) {
	requireGit(t)
	base := realTempDir(t)
	root := filepath.Join(base, "ws")
	code := filepath.Join(root, "code")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, code, "init", "-q", "--separate-git-dir", store)
	if err := os.Symlink("hop", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("store", filepath.Join(root, "hop")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(code, ".git"), []byte("gitdir: ../link\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, pins := metadataPaths([]string{code}, []string{root})
	requirePaths(t, pins, filepath.Join(root, "link"), filepath.Join(root, "hop"), store)
	requirePaths(t, paths, filepath.Join(store, "config"))
}

func TestGitGroupPathsCollapseWhenOverBudget(t *testing.T) {
	requireGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	common := filepath.Join(ws, ".git")
	plant := func(n int) {
		for i := range n {
			m := filepath.Join(common, "modules", fmt.Sprintf("g%d", i%10), fmt.Sprintf("m%d", i))
			if err := os.MkdirAll(m, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(m, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	plant(3)
	configs := func(paths []gitProtectedPath) (n int, tree bool) {
		for _, p := range paths {
			if filepath.Base(p.Path) == "config" {
				n++
			}
			tree = tree || (p.Tree && p.Path == filepath.Join(common, "modules"))
		}
		return n, tree
	}
	if n, tree := configs(gitGroupPaths(common, gitGroupMaxEntries)); n != 3 || tree {
		t.Fatalf("each submodule gitdir is protected one by one: %d configs, tree %v", n, tree)
	}
	plant(gitGroupMaxEntries + 1)
	if n, tree := configs(gitGroupPaths(common, gitGroupMaxEntries)); n != gitGroupMaxEntries || !tree {
		t.Fatalf("past the limit the group is one read-only tree: %d configs, tree %v", n, tree)
	}
	if g := gitGroupsOf(common, gitGroupMaxMounts); g.ModulesOver {
		t.Fatal("the mount budget is larger than the rule budget")
	}
}

// A symlinked group directory or gitdir entry is never followed: binding
// through it would protect, or expose, whatever it names.
func TestGitGroupsDoNotFollowSymlinks(t *testing.T) {
	requireGit(t)
	base := realTempDir(t)
	ws := filepath.Join(base, "ws")
	newRepo(t, ws)
	src := filepath.Join(ws, "src")
	if err := os.MkdirAll(filepath.Join(src, "m"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "m", "HEAD"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(ws, ".git")
	if err := os.Symlink(src, filepath.Join(common, "modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(common, "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, filepath.Join(common, "worktrees", "planted")); err != nil {
		t.Fatal(err)
	}
	for _, p := range gitGroupPaths(common, gitGroupMaxEntries) {
		if strings.HasPrefix(p.Path, src) || strings.Contains(p.Path, "planted") || strings.HasPrefix(p.Path, filepath.Join(common, "modules")) {
			t.Fatalf("followed a symlink: %v", p)
		}
	}
}

func TestCheckGitMetadataRefusesAHardLinkedConfig(t *testing.T) {
	requireGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}}
	if err := CheckGitMetadata(spec); err != nil {
		t.Fatalf("an ordinary repository is not refused: %v", err)
	}
	if err := os.Link(filepath.Join(ws, ".git", "config"), filepath.Join(ws, "cfg")); err != nil {
		t.Fatal(err)
	}
	err := CheckGitMetadata(spec)
	if !errors.Is(err, ErrGitMetadataLinked) || !strings.Contains(err.Error(), filepath.Join(ws, ".git", "config")) {
		t.Fatalf("a hard-linked config must be refused with its identity: %v", err)
	}
	if err := os.Remove(filepath.Join(ws, "cfg")); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(ws, ".git", "hooks", "post-merge")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(hook, filepath.Join(ws, "hook")); err != nil {
		t.Fatal(err)
	}
	if err := CheckGitMetadata(spec); !errors.Is(err, ErrGitMetadataLinked) {
		t.Fatalf("a hard-linked hook must be refused: %v", err)
	}
}
