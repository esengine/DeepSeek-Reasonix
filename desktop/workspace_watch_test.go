package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"reasonix/internal/event"
)

func contentWorkspaceMutation(paths []string, allPaths bool) event.WorkspaceMutation {
	return event.WorkspaceMutation{Paths: paths, AllPaths: allPaths, Content: true, Tree: true, WorkingTree: true}
}

func TestWorkspaceChangeHubSharesRootRevisionsAndIsolatesSessions(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.tabs["a"] = &WorkspaceTab{ID: "a", WorkspaceRoot: root}
	app.tabs["b"] = &WorkspaceTab{ID: "b", WorkspaceRoot: root}

	beforeA := app.WorkspaceRevisionForTab("a")
	beforeB := app.WorkspaceRevisionForTab("b")
	app.workspaceHub.observeAgentMutation("a", contentWorkspaceMutation([]string{"pkg/main.go"}, false))
	afterA := app.WorkspaceRevisionForTab("a")
	afterB := app.WorkspaceRevisionForTab("b")
	if afterA.Revisions.Content <= beforeA.Revisions.Content || afterB.Revisions.Content != afterA.Revisions.Content {
		t.Fatalf("root content revision not shared: before=%+v afterA=%+v afterB=%+v", beforeA, afterA, afterB)
	}
	if afterA.Revisions.Session <= beforeA.Revisions.Session || afterB.Revisions.Session != beforeB.Revisions.Session {
		t.Fatalf("session revision leaked across tabs: beforeA=%+v beforeB=%+v afterA=%+v afterB=%+v", beforeA, beforeB, afterA, afterB)
	}
}

func TestWorkspaceChangeHubCapsOpaqueMutation(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.workspaceHub.observeAgentMutation("a", contentWorkspaceMutation(nil, true))
	key := canonicalWorkspaceRoot(root)
	app.workspaceHub.mu.Lock()
	r := app.workspaceHub.roots[key]
	allPaths := r != nil && r.allPaths
	app.workspaceHub.mu.Unlock()
	if !allPaths {
		t.Fatal("opaque mutation did not become allPaths invalidation")
	}
}

func TestWorkspaceChangeHubFilesystemWritePublishesContentRevision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	initial := app.WorkspaceRevisionForTab("a").Revisions.Content
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The watcher callback is asynchronous; wait without imposing a fixed
	// sleep so slow CI filesystems get the same bounded opportunity.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if app.WorkspaceRevisionForTab("a").Revisions.Content > initial {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("filesystem write did not advance content revision")
}

func TestWorkspaceChangeHubDoesNotDropFilesystemWriteAfterAgentMutation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })

	before := app.WorkspaceRevisionForTab("a").Revisions.Content
	app.workspaceHub.observeAgentMutation("a", contentWorkspaceMutation([]string{"file.txt"}, false))
	key := canonicalWorkspaceRoot(root)
	app.workspaceHub.observeFilesystem(key, fsnotify.Event{Name: path, Op: fsnotify.Write})
	after := app.WorkspaceRevisionForTab("a").Revisions.Content
	if after != before+2 {
		t.Fatalf("content revision = %d, want %d (agent and filesystem writes are independently observable)", after, before+2)
	}
}

func TestWorkspaceChangeHubRejectsRelativeTraversalMetadata(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.workspaceHub.observeAgentMutation("a", contentWorkspaceMutation([]string{"../outside.txt"}, false))

	key := canonicalWorkspaceRoot(root)
	app.workspaceHub.mu.Lock()
	r := app.workspaceHub.roots[key]
	allPaths := r != nil && r.allPaths
	_, leaked := r.pending["../outside.txt"]
	app.workspaceHub.mu.Unlock()
	if !allPaths || leaked {
		t.Fatalf("relative traversal was not safely degraded: allPaths=%v leaked=%v", allPaths, leaked)
	}
}

func TestWorkspaceChangeHubAdvancesOnlyDeclaredAgentResources(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	before := app.WorkspaceRevisionForTab("a").Revisions

	app.workspaceHub.observeAgentMutation("a", event.WorkspaceMutation{WorkingTree: true, GitMeta: true})
	after := app.WorkspaceRevisionForTab("a").Revisions
	if after.Content != before.Content || after.Tree != before.Tree {
		t.Fatalf("git-only invalidation advanced content/tree: before=%+v after=%+v", before, after)
	}
	if after.WorkingTree != before.WorkingTree+1 || after.GitMeta != before.GitMeta+1 || after.Session != before.Session+1 {
		t.Fatalf("git-only revisions not advanced independently: before=%+v after=%+v", before, after)
	}
}

func TestTabEventSinkForwardsImmediateWorkspaceMutation(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	sink := event.Sync(&tabEventSink{tabID: "a", app: app})
	before := app.WorkspaceRevisionForTab("a").Revisions

	event.RecordWorkspaceMutation(sink, event.WorkspaceMutation{
		ToolID: "write", ToolName: "write_file", Paths: []string{"file.go"}, Content: true, Tree: true, WorkingTree: true,
	})
	after := app.WorkspaceRevisionForTab("a").Revisions
	if after.Content != before.Content+1 || after.Tree != before.Tree+1 || after.WorkingTree != before.WorkingTree+1 || after.Session != before.Session+1 {
		t.Fatalf("tab sink did not forward immediate workspace mutation: before=%+v after=%+v", before, after)
	}
}

func TestWorkspaceChangeHubUsesTrailingPublishGeneration(t *testing.T) {
	root := t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.WorkspaceRevisionForTab("a")

	key := canonicalWorkspaceRoot(root)
	app.workspaceHub.mu.Lock()
	r := app.workspaceHub.roots[key]
	app.workspaceHub.schedulePublishLocked(r)
	firstTimer, firstGeneration := r.timer, r.publishGen
	app.workspaceHub.schedulePublishLocked(r)
	secondTimer, secondGeneration := r.timer, r.publishGen
	app.workspaceHub.mu.Unlock()
	if firstTimer == secondTimer || secondGeneration != firstGeneration+1 {
		t.Fatalf("quiet window was not reset: timersSame=%v generations=%d/%d", firstTimer == secondTimer, firstGeneration, secondGeneration)
	}
}

func TestWorkspaceChangeHubReleasesRootAfterTabWorkspaceSwitch(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: rootA}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	app.WorkspaceRevisionForTab("a")
	app.mu.Lock()
	app.tabs["a"].WorkspaceRoot = rootB
	app.mu.Unlock()
	app.WorkspaceRevisionForTab("a")
	app.workspaceHub.reconcileRoots()

	app.workspaceHub.mu.Lock()
	_, oldExists := app.workspaceHub.roots[canonicalWorkspaceRoot(rootA)]
	_, newExists := app.workspaceHub.roots[canonicalWorkspaceRoot(rootB)]
	app.workspaceHub.mu.Unlock()
	if oldExists || !newExists {
		t.Fatalf("root lifecycle after switch: oldExists=%v newExists=%v", oldExists, newExists)
	}
}

func TestGitMetadataDirsForWorkspaceUsesHardenedGitCommand(t *testing.T) {
	// Source-level contract: both startup rev-parse probes must go through
	// gitcmd.Command so Windows gets HideWindow + CREATE_NO_WINDOW without
	// forking a second unhardened path.
	source, err := os.ReadFile("workspace_watch.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, `gitcmd.Command(ctx, root, "rev-parse", flag)`) {
		t.Fatal("gitMetadataDirsForWorkspace must call gitcmd.Command for rev-parse probes")
	}
	if strings.Contains(text, `exec.CommandContext(ctx, "git"`) || strings.Contains(text, `exec.Command("git"`) {
		t.Fatal("workspace_watch must not invoke raw git exec for metadata probes")
	}
}

func TestWorkspaceChangeHubRecursivelyWatchesGitMetadataOnly(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	gitDir := filepath.Join(root, ".git")
	for _, rel := range []string{"refs/heads", "logs/refs/heads", "worktrees/linked", "objects/pack"} {
		if err := os.MkdirAll(filepath.Join(gitDir, rel), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{tabs: map[string]*WorkspaceTab{"a": {ID: "a", WorkspaceRoot: root}}}
	app.workspaceHub = newWorkspaceChangeHub(app)
	t.Cleanup(func() { app.workspaceHub.close() })
	view := app.WorkspaceRevisionForTab("a")
	if view.WatchState == "unavailable" {
		t.Fatalf("watcher unavailable: %+v", view)
	}

	key := canonicalWorkspaceRoot(root)
	gitDir = canonicalWorkspaceRoot(gitDir)
	app.workspaceHub.mu.Lock()
	r := app.workspaceHub.roots[key]
	recursive := r != nil && r.watcher != nil && r.watcher.SupportsRecursive()
	_, rootWatched := r.watched[key]
	_, refsWatched := r.watched[filepath.Join(gitDir, "refs", "heads")]
	_, logsWatched := r.watched[filepath.Join(gitDir, "logs", "refs", "heads")]
	_, worktreeWatched := r.watched[filepath.Join(gitDir, "worktrees", "linked")]
	_, objectsWatched := r.watched[filepath.Join(gitDir, "objects")]
	app.workspaceHub.mu.Unlock()
	if recursive {
		if !rootWatched || refsWatched || logsWatched || worktreeWatched || objectsWatched {
			t.Fatalf("recursive workspace watch root=%v refs=%v logs=%v worktrees=%v objects=%v", rootWatched, refsWatched, logsWatched, worktreeWatched, objectsWatched)
		}
		return
	}
	if !refsWatched || !logsWatched || !worktreeWatched || objectsWatched {
		t.Fatalf("git watches refs=%v logs=%v worktrees=%v objects=%v", refsWatched, logsWatched, worktreeWatched, objectsWatched)
	}
}
