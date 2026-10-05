//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestBwrapGitMetadataArgsPinDirectoriesThenMountReadOnly(t *testing.T) {
	requireGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	git := filepath.Join(ws, ".git")
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}, MinimalWrites: true}
	args := bwrapBaseArgs(spec)
	root := indexArgs(args, "--bind", ws, ws)
	pin := indexArgs(args, "--bind", git, git)
	config := indexArgs(args, "--ro-bind", filepath.Join(git, "config"), filepath.Join(git, "config"))
	hooks := indexArgs(args, "--ro-bind", filepath.Join(git, "hooks"), filepath.Join(git, "hooks"))
	if root < 0 || pin <= root || config <= pin || hooks <= pin {
		t.Fatalf("want write root, then .git pinned, then config and hooks read-only: %v", args)
	}
	if indexArgs(args[root+3:], "--bind", ws, ws) >= 0 {
		t.Fatalf("the write root must not be bound again over its own mounts: %v", args)
	}
	if indexArgs(args, "--ro-bind", filepath.Join(git, "config.worktree"), filepath.Join(git, "config.worktree")) >= 0 {
		t.Fatalf("an absent path cannot be a mount destination: %v", args)
	}
	if got := bwrapGitMetadataArgs(Spec{Mode: "off", WriteRoots: []string{ws}}); len(got) != 0 {
		t.Fatalf("an unconfined spec needs no git mounts: %v", got)
	}
}

func TestBwrapGitMetadataArgsStayBoundedWithManySubmodules(t *testing.T) {
	requireGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	for i := range 2000 {
		m := filepath.Join(ws, ".git", "modules", fmt.Sprintf("g%d", i%20), fmt.Sprintf("m%d", i))
		if err := os.MkdirAll(filepath.Join(m, "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(m, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	args := bwrapGitMetadataArgs(Spec{Mode: "enforce", WriteRoots: []string{ws}, MinimalWrites: true})
	modules := filepath.Join(ws, ".git", "modules")
	if len(args) > 100 || indexArgs(args, "--ro-bind", modules, modules) < 0 {
		t.Fatalf("an over-budget modules/ must collapse to one read-only bind: %d args", len(args))
	}
}
