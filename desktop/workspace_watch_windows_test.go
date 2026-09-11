//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/gitcmd"
)

const workspaceGitCreateNoWindow = 0x08000000

func TestWorkspaceGitProbesHideConsoleWindowOnWindows(t *testing.T) {
	// Both startup probes use the same gitcmd.Command construction path.
	// Assert HideWindow + CREATE_NO_WINDOW for each rev-parse flag.
	for _, flag := range []string{"--git-dir", "--git-common-dir"} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := gitcmd.Command(ctx, t.TempDir(), "rev-parse", flag)
		cancel()
		if cmd.SysProcAttr == nil {
			t.Fatalf("%s: SysProcAttr is nil", flag)
		}
		if !cmd.SysProcAttr.HideWindow {
			t.Fatalf("%s: HideWindow is false", flag)
		}
		if cmd.SysProcAttr.CreationFlags&workspaceGitCreateNoWindow == 0 {
			t.Fatalf("%s: CREATE_NO_WINDOW not set; CreationFlags=%#x", flag, cmd.SysProcAttr.CreationFlags)
		}
	}
}

func TestWorkspaceWatcherDoesNotLockNestedParentOnWindows(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "parent")
	if err := os.MkdirAll(filepath.Join(from, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	if state := app.WorkspaceRevisionForTab("a").WatchState; state == "unavailable" {
		t.Fatalf("watcher unavailable: %s", state)
	}
	before := app.WorkspaceRevisionForTab("a").Revisions.Content

	to := filepath.Join(root, "renamed")
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename parent while workspace watcher is active: %v", err)
	}
	waitForWorkspaceContentRevision(t, app, before)
	before = waitForWorkspaceRevisionToSettle(t, app)
	if err := os.WriteFile(filepath.Join(to, "child", "after.txt"), []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceContentRevision(t, app, before)
	before = waitForWorkspaceRevisionToSettle(t, app)
	if err := os.RemoveAll(to); err != nil {
		t.Fatalf("delete renamed parent while workspace watcher is active: %v", err)
	}
	waitForWorkspaceContentRevision(t, app, before)
}

func TestWorkspaceWatcherUsesOneRecursiveWorkspaceHandleOnWindows(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "one", "two", "three"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.WorkspaceRevisionForTab("a")

	key := canonicalWorkspaceRoot(root)
	app.workspaceHub.mu.Lock()
	r := app.workspaceHub.roots[key]
	dirs := 0
	if r != nil {
		dirs = r.dirs
	}
	app.workspaceHub.mu.Unlock()
	if dirs != 1 {
		t.Fatalf("workspace handles = %d, want 1 recursive root handle", dirs)
	}
}

func TestWindowsWorkspaceWatcherPreservesUnicodeEventPath(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	w, err := newWorkspaceWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Add(root, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(deep, "unicode-\u6587\u4ef6-\U0001F600.txt")
	if err := os.WriteFile(target, []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case ev := <-w.Events():
			if filepath.Clean(ev.Name) == target {
				return
			}
		case err := <-w.Errors():
			t.Fatalf("watcher error: %v", err)
		case <-timer.C:
			t.Fatalf("no event for exact Unicode path %q", target)
		}
	}
}

func TestWorkspaceWatcherReportsPreexistingDeepFileChangesOnWindows(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "one", "two", "three")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	before := app.WorkspaceRevisionForTab("a").Revisions.Content
	if err := os.WriteFile(filepath.Join(deep, "file.txt"), []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceContentRevision(t, app, before)
}

func TestWorkspaceWatcherReportsFilesInNewDeepDirectoriesOnWindows(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	before := app.WorkspaceRevisionForTab("a").Revisions.Content
	deep := filepath.Join(root, "new", "nested")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceContentRevision(t, app, before)
	before = waitForWorkspaceRevisionToSettle(t, app)
	if err := os.WriteFile(filepath.Join(deep, "later.txt"), []byte("later"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceContentRevision(t, app, before)
}

func TestWorkspaceWatcherIgnoresGeneratedSubtreeChurnOnWindows(t *testing.T) {
	root := t.TempDir()
	generated := filepath.Join(root, "node_modules", "pkg")
	if err := os.MkdirAll(generated, 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	before := app.WorkspaceRevisionForTab("a").Revisions.Content
	for i := range 256 {
		name := filepath.Join(generated, fmt.Sprintf("generated-%03d.js", i))
		if err := os.WriteFile(name, []byte("generated"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	view := app.WorkspaceRevisionForTab("a")
	after := view.Revisions.Content
	if after != before {
		t.Fatalf("generated subtree advanced content revision: before=%d after=%d", before, after)
	}
	if view.WatchState != "active" {
		t.Fatalf("generated subtree degraded recursive watcher: %s", view.WatchState)
	}
}

func TestWorkspaceWatcherCloseReleasesNestedDirectoriesOnWindows(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "parent")
	if err := os.MkdirAll(filepath.Join(from, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	app.WorkspaceRevisionForTab("a")
	app.workspaceHub.close()
	if err := os.Rename(from, filepath.Join(root, "after-close")); err != nil {
		t.Fatalf("rename after workspace watcher close: %v", err)
	}
}

func TestWorkspaceWatcherSwitchReleasesOldWorkspaceOnWindows(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	from := filepath.Join(rootA, "parent")
	if err := os.MkdirAll(filepath.Join(from, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: rootA}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.WorkspaceRevisionForTab("a")
	app.mu.Lock()
	app.tabs["a"].WorkspaceRoot = rootB
	app.mu.Unlock()
	app.WorkspaceRevisionForTab("a")
	app.workspaceHub.reconcileRoots()

	if err := os.Rename(from, filepath.Join(rootA, "after-switch")); err != nil {
		t.Fatalf("rename in released workspace: %v", err)
	}
	before := app.WorkspaceRevisionForTab("a").Revisions.Content
	if err := os.WriteFile(filepath.Join(rootB, "active.txt"), []byte("active"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceContentRevision(t, app, before)
}

func TestWorkspaceWatcherKeepsGitMetadataScopeOnWindows(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	before := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
	ref := filepath.Join(root, ".git", "refs", "heads", "watch-test")
	if err := os.WriteFile(ref, []byte("0000000000000000000000000000000000000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceGitRevision(t, app, before)
	before = waitForWorkspaceGitRevisionToSettle(t, app)
	objectDir := filepath.Join(root, ".git", "objects", "ff")
	if err := os.MkdirAll(objectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(objectDir, "ignored"), []byte("object"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	after := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
	if after != before {
		t.Fatalf("git object churn advanced metadata revision: before=%d after=%d", before, after)
	}
}

func TestWorkspaceWatcherTracksExternalGitMetadataOnWindows(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	gitDir := filepath.Join(base, "git-dir")
	if out, err := exec.Command("git", "init", "--separate-git-dir", gitDir, root).CombinedOutput(); err != nil {
		t.Fatalf("git init --separate-git-dir: %v: %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, "parent", "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	before := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
	ref := filepath.Join(gitDir, "refs", "heads", "watch-test")
	if err := os.WriteFile(ref, []byte("0000000000000000000000000000000000000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForWorkspaceGitRevision(t, app, before)
	if err := os.Rename(filepath.Join(root, "parent"), filepath.Join(root, "renamed")); err != nil {
		t.Fatalf("external Git watches locked a workspace child: %v", err)
	}
}

func waitForWorkspaceContentRevision(t *testing.T, app *App, before uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := app.WorkspaceRevisionForTab("a").Revisions.Content
		if got > before {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("content revision did not advance beyond %d", before)
	return before
}

func waitForWorkspaceRevisionToSettle(t *testing.T, app *App) uint64 {
	t.Helper()
	last := app.WorkspaceRevisionForTab("a").Revisions.Content
	stableSince := time.Now()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		got := app.WorkspaceRevisionForTab("a").Revisions.Content
		if got != last {
			last = got
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= 100*time.Millisecond {
			return got
		}
	}
	t.Fatalf("content revision did not settle")
	return last
}

func waitForWorkspaceGitRevision(t *testing.T, app *App, before uint64) uint64 {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
		if got > before {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("git metadata revision did not advance beyond %d", before)
	return before
}

func waitForWorkspaceGitRevisionToSettle(t *testing.T, app *App) uint64 {
	t.Helper()
	last := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
	stableSince := time.Now()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		got := app.WorkspaceRevisionForTab("a").Revisions.GitMeta
		if got != last {
			last = got
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= 100*time.Millisecond {
			return got
		}
	}
	t.Fatalf("git metadata revision did not settle")
	return last
}
