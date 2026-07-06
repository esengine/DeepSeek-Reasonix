package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
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

// TestWorkspaceChangesWaitsForAsyncControllerBuild verifies #4544: when
// tab.Ctrl is still nil (async controller build in progress),
// WorkspaceChanges waits for the build to finish before returning.
func TestWorkspaceChangesWaitsForAsyncControllerBuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	workspace, err := os.MkdirTemp("", "workspace-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(workspace)

	sessionPath := filepath.Join(workspace, "s.jsonl")
	ckptDir := strings.TrimSuffix(sessionPath, ".jsonl") + ".ckpt"
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := "old"
	now := time.Now()
	seedCheckpoint(t, ckptDir, checkpoint.Checkpoint{
		Turn: 0, Time: now, Prompt: "edit a file",
		Files: []checkpoint.FileSnap{{Path: "a.txt", Content: &content}},
	})

	// Create a controller that already has the checkpoints loaded.
	ctrl := control.New(control.Options{
		SessionDir:  workspace,
		SessionPath: sessionPath,
	})

	// Create a tab with Ctrl: nil to simulate the state right after
	// activateTopic returns — the async build hasn't finished yet.
	app := &App{
		tabs:        map[string]*WorkspaceTab{"test": {ID: "test", WorkspaceRoot: workspace}},
		activeTabID: "test",
	}

	// Start async build in a goroutine, simulating startTabControllerBuild.
	go func() {
		time.Sleep(300 * time.Millisecond)
		app.mu.Lock()
		app.tabs["test"].Ctrl = ctrl
		app.tabs["test"].Ready = true
		app.mu.Unlock()
	}()

	start := time.Now()
	got := app.WorkspaceChanges("test")
	elapsed := time.Since(start)

	if elapsed < 250*time.Millisecond {
		t.Fatalf("WorkspaceChanges returned too fast (%v), did not wait for Ctrl", elapsed)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("WorkspaceChanges took too long (%v), the wait loop should exit within ~300ms", elapsed)
	}
	if len(got.Files) == 0 {
		t.Fatal("WorkspaceChanges returned no files — Ctrl was nil and checkpoint data wasn't loaded")
	}
	if got.Files[0].Path != "a.txt" {
		t.Fatalf("first file = %q, want a.txt", got.Files[0].Path)
	}
}

// TestWorkspaceChangesControllerBuildFailure verifies that when a tab's
// async build finishes with an error (Ready==true, Ctrl==nil), the wait
// loop exits early instead of polling for 5 seconds.
func TestWorkspaceChangesControllerBuildFailure(t *testing.T) {
	app := &App{
		tabs:        map[string]*WorkspaceTab{"test": {ID: "test", WorkspaceRoot: t.TempDir(), Ready: true}},
		activeTabID: "test",
	}

	start := time.Now()
	got := app.WorkspaceChanges("test")
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("WorkspaceChanges waited too long (%v) on a failed build", elapsed)
	}
	if len(got.Files) != 0 {
		t.Fatalf("expected no files for failed build, got %d", len(got.Files))
	}
}
