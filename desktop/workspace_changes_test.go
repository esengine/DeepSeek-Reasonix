package main

import (
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"
)

func TestWorkspaceGitDisablesDaemonSpawns(t *testing.T) {
	cmd := workspaceGit("-C", "repo", "status", "--porcelain=v1")
	want := []string{"git", "-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-C", "repo", "status", "--porcelain=v1"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
	if runtime.GOOS == "windows" && cmd.SysProcAttr == nil {
		t.Fatal("workspaceGit must hide the console window on Windows")
	}
}

func TestWorkspaceGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatal(err)
		}
	}()

	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	runGit(t, "checkout", "-b", "feature/status")

	if got := workspaceGitBranch(repo); got != "feature/status" {
		t.Fatalf("branch = %q, want feature/status", got)
	}
}

func TestWorkspaceGitBranchReflectsImmediateCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatal(err)
		}
	}()

	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	runGit(t, "checkout", "-b", "feature/one")

	if got := workspaceGitBranch(repo); got != "feature/one" {
		t.Fatalf("branch before checkout = %q, want feature/one", got)
	}
	runGit(t, "checkout", "-b", "feature/two")
	if got := workspaceGitBranch(repo); got != "feature/two" {
		t.Fatalf("branch after checkout = %q, want feature/two", got)
	}
}

func TestWorkspaceGitBranchDetachedHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatal(err)
		}
	}()

	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	runGit(t, "config", "user.email", "test@example.com")
	runGit(t, "config", "user.name", "Test User")
	if err := os.WriteFile("tracked.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, "add", "tracked.txt")
	runGit(t, "commit", "-m", "init")
	short := gitOutput(t, "rev-parse", "--short", "HEAD")
	runGit(t, "checkout", "--detach", "HEAD")

	if got := workspaceGitBranch(repo); got != "@"+short {
		t.Fatalf("branch = %q, want @%s", got, short)
	}
}

func TestWorkspaceGitBranchNonGitDirectory(t *testing.T) {
	if got := workspaceGitBranch(t.TempDir()); got != "" {
		t.Fatalf("branch = %q, want empty", got)
	}
}
