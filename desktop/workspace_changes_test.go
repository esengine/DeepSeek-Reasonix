package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
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

func TestWorkspaceGitBranchForMetaDoesNotBlockOnColdProbe(t *testing.T) {
	resetWorkspaceGitBranchMetaCacheForTest(t)
	origProbe := workspaceGitBranchForMetaProbe
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseProbe := func() { releaseOnce.Do(func() { close(release) }) }
	workspaceGitBranchForMetaProbe = func(string) string {
		close(started)
		<-release
		return "feature/async"
	}
	defer func() {
		releaseProbe()
		workspaceGitBranchForMetaProbe = origProbe
	}()

	start := time.Now()
	if got := workspaceGitBranchForMeta("/tmp/reasonix-cold-probe"); got != "" {
		t.Fatalf("cold branch = %q, want empty while async refresh runs", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("cold metadata branch probe blocked for %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background branch refresh did not start")
	}

	releaseProbe()
	eventuallyBranchForMeta(t, "/tmp/reasonix-cold-probe", "feature/async")
}

func TestWorkspaceGitBranchForMetaReturnsStaleDuringRefresh(t *testing.T) {
	resetWorkspaceGitBranchMetaCacheForTest(t)
	workspaceGitBranchCache.Lock()
	workspaceGitBranchCache.entries[filepath.Clean("/tmp/reasonix-stale-probe")] = workspaceGitBranchCacheEntry{
		branch:  "feature/stale",
		expires: time.Now().Add(-time.Second),
	}
	workspaceGitBranchCache.Unlock()

	origProbe := workspaceGitBranchForMetaProbe
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseProbe := func() { releaseOnce.Do(func() { close(release) }) }
	workspaceGitBranchForMetaProbe = func(string) string {
		close(started)
		<-release
		return "feature/fresh"
	}
	defer func() {
		releaseProbe()
		workspaceGitBranchForMetaProbe = origProbe
	}()

	start := time.Now()
	if got := workspaceGitBranchForMeta("/tmp/reasonix-stale-probe"); got != "feature/stale" {
		t.Fatalf("stale branch = %q, want feature/stale", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("stale metadata branch probe blocked for %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background branch refresh did not start")
	}

	releaseProbe()
	eventuallyBranchForMeta(t, "/tmp/reasonix-stale-probe", "feature/fresh")
}

func resetWorkspaceGitBranchMetaCacheForTest(t *testing.T) {
	t.Helper()
	workspaceGitBranchCache.Lock()
	workspaceGitBranchCache.entries = map[string]workspaceGitBranchCacheEntry{}
	workspaceGitBranchCache.Unlock()
}

func eventuallyBranchForMeta(t *testing.T, base, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := workspaceGitBranchForMeta(base); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("branch did not refresh to %q", want)
}
