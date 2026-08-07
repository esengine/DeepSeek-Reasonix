package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/worktree"
)

func TestTopicWorktreeAvailabilityDelegatesWithoutRequiringGit(t *testing.T) {
	original := inspectTopicWorktree
	t.Cleanup(func() { inspectTopicWorktree = original })
	inspectTopicWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: false, Reason: "Git is not installed", RepoRoot: root}
	}
	got := NewApp().TopicWorktreeAvailability("project")
	if got.Available || got.Reason != "Git is not installed" || got.RepoRoot != "project" {
		t.Fatalf("availability = %+v", got)
	}
}

func TestCreateTopicWorktreeBindsUnderSourceProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source-project")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := config.DeliveryWorktreeDir()
	isolatedRoot := filepath.Join(managed, "repo", "id", "source-project")
	if err := os.MkdirAll(isolatedRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	originalInspect := inspectTopicWorktree
	originalCreate := createTopicWorktree
	t.Cleanup(func() {
		inspectTopicWorktree = originalInspect
		createTopicWorktree = originalCreate
	})
	inspectTopicWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root, Branch: "main"}
	}
	createTopicWorktree = func(_ context.Context, gotSource, gotManaged string) (worktree.Result, error) {
		wantSource, err := filepath.Abs(source)
		if err != nil {
			t.Fatal(err)
		}
		if gotSource != wantSource {
			t.Fatalf("source = %q, want %q", gotSource, wantSource)
		}
		if gotManaged != managed {
			t.Fatalf("managed root = %q, want %q", gotManaged, managed)
		}
		return worktree.Result{
			WorkspaceRoot: isolatedRoot,
			WorktreeRoot:  isolatedRoot,
			SourceRoot:    wantSource,
			Branch:        "reasonix/topic-test",
			Head:          "abc123",
			SourceDirty:   true,
		}, nil
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	result, err := app.CreateTopicWorktree(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Branch != "reasonix/topic-test" || !result.SourceDirty || result.WorkspaceRoot != isolatedRoot {
		t.Fatalf("result = %+v", result)
	}
	if result.TopicID == "" {
		t.Fatal("expected topic id")
	}
	wantSource, err := filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceRoot != wantSource {
		t.Fatalf("sourceRoot = %q, want %q", result.SourceRoot, wantSource)
	}
	if result.Tab.WorkspaceRoot != isolatedRoot {
		t.Fatalf("tab workspace = %q, want %q", result.Tab.WorkspaceRoot, isolatedRoot)
	}
	if result.Tab.SourceRoot != wantSource {
		t.Fatalf("tab sourceRoot = %q, want %q", result.Tab.SourceRoot, wantSource)
	}
	if !result.Tab.IsolatedWorktree || !result.Tab.Active {
		t.Fatalf("opened tab = %+v", result.Tab)
	}

	binding, ok := loadTopicWorktreeBinding(wantSource, result.TopicID)
	if !ok {
		t.Fatal("expected topic worktree binding")
	}
	if binding.WorkspaceRoot != isolatedRoot || binding.Branch != "reasonix/topic-test" || binding.Head != "abc123" {
		t.Fatalf("binding = %+v", binding)
	}

	projects := app.ListProjectTree()
	for _, project := range projects {
		if sameProjectRoot(project.Root, isolatedRoot) {
			t.Fatalf("managed worktree registered as sibling project: %+v", project)
		}
	}
	foundTopic := false
	for _, project := range projects {
		if !sameProjectRoot(project.Root, wantSource) {
			continue
		}
		for _, topic := range project.Children {
			if topic.TopicID == result.TopicID {
				foundTopic = true
				if !topic.IsolatedWorktree {
					t.Fatalf("topic node missing isolated badge: %+v", topic)
				}
			}
		}
	}
	if !foundTopic {
		t.Fatalf("topic %s not listed under source project", result.TopicID)
	}

	marker := readTopicWorktreeCreatedHead(result.WorktreeRoot)
	if marker != "abc123" {
		t.Fatalf("created-head marker = %q, want abc123", marker)
	}
}

func TestRegisterProjectRootSkipsManagedWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	managed := config.DeliveryWorktreeDir()
	isolated := filepath.Join(managed, "repo", "id", "proj")
	if err := os.MkdirAll(isolated, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := addProject(source, ""); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicWorktreeBinding(source, TopicWorktreeBinding{
		TopicID:       "topic-bound",
		SourceRoot:    source,
		WorkspaceRoot: isolated,
		WorktreeRoot:  isolated,
		Head:          "abc",
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.registerProjectRoot(isolated)
	app.registerProjectRoot(source)

	for _, project := range app.ListProjectTree() {
		if sameProjectRoot(project.Root, isolated) {
			t.Fatalf("topic-managed path registered: %+v", project)
		}
	}
	found := false
	for _, project := range app.ListProjectTree() {
		if sameProjectRoot(project.Root, source) {
			found = true
		}
	}
	if !found {
		t.Fatal("source project was not registered")
	}
}

func TestRegisterProjectRootAllowsDeliveryManagedWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	managed := config.DeliveryWorktreeDir()
	isolated := filepath.Join(managed, "repo", "id", "delivery-proj")
	if err := os.MkdirAll(isolated, 0o755); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.registerProjectRoot(isolated)
	found := false
	for _, project := range app.ListProjectTree() {
		if sameProjectRoot(project.Root, isolated) {
			found = true
		}
	}
	if !found {
		t.Fatal("delivery managed worktree should register as a first-class project")
	}
}

func TestResolveProjectOpenRootsMapsManagedPathToSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := addProject(source, ""); err != nil {
		t.Fatal(err)
	}
	managed := config.DeliveryWorktreeDir()
	isolated := filepath.Join(managed, "repo", "id", "source")
	if err := os.MkdirAll(isolated, 0o755); err != nil {
		t.Fatal(err)
	}
	topicID := "topic-1"
	if err := saveTopicWorktreeBinding(source, TopicWorktreeBinding{
		TopicID:       topicID,
		SourceRoot:    source,
		WorkspaceRoot: isolated,
		WorktreeRoot:  filepath.Dir(isolated),
		Branch:        "reasonix/topic-1",
		Head:          "deadbeef",
	}); err != nil {
		t.Fatal(err)
	}

	logical, runtime := resolveProjectOpenRoots(isolated, topicID)
	if !sameProjectRoot(logical, source) {
		t.Fatalf("logical = %q, want %q", logical, source)
	}
	if !sameProjectRoot(runtime, isolated) {
		t.Fatalf("runtime = %q, want %q", runtime, isolated)
	}
}

func TestTopicSessionMatchAcceptsLegacyWorktreePin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := config.DeliveryWorktreeDir()
	isolated := filepath.Join(managed, "repo", "id", "source")
	if err := os.MkdirAll(isolated, 0o755); err != nil {
		t.Fatal(err)
	}
	topicID := "topic-legacy"
	if err := saveTopicWorktreeBinding(source, TopicWorktreeBinding{
		TopicID:       topicID,
		SourceRoot:    source,
		WorkspaceRoot: isolated,
		WorktreeRoot:  filepath.Dir(isolated),
		Head:          "abc",
	}); err != nil {
		t.Fatal(err)
	}

	match := topicSessionMatch{scope: "project", workspaceRoot: isolated}
	if !topicSessionMatchMatchesTarget(match, "project", source, topicID) {
		t.Fatal("expected legacy worktree BranchMeta to match source target")
	}
	if topicSessionMatchMatchesTarget(match, "project", source, "other") {
		t.Fatal("unexpected match for unrelated topic")
	}
}

func TestReclaimOrphanTopicWorktreeRequiresCreatedHead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	managed := config.DeliveryWorktreeDir()
	path := filepath.Join(managed, "repo", "id")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".git"), []byte("gitdir: /tmp/fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var removed []string
	originalRemove := removePristineTopicWorktree
	t.Cleanup(func() { removePristineTopicWorktree = originalRemove })
	removePristineTopicWorktree = func(_ context.Context, _, worktreeRoot, createdHead string) error {
		removed = append(removed, worktreeRoot+"|"+createdHead)
		return nil
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	// Fake .git is not a live worktree; symbolic-ref fails and reclaim must
	// not remove anything even after a marker is written.
	writeTopicWorktreeCreatedHead(path, "abc123")
	app.reclaimOrphanTopicWorktrees()
	if len(removed) != 0 {
		t.Fatalf("reclaim removed %v without a valid topic branch", removed)
	}
	if got := readTopicWorktreeCreatedHead(path); got != "abc123" {
		t.Fatalf("marker = %q", got)
	}
}

func TestRemoveTopicWorktreeBindingWritesCreatedHead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(isolated, 0o755); err != nil {
		t.Fatal(err)
	}
	topicID := "topic-rm"
	if err := saveTopicWorktreeBinding(source, TopicWorktreeBinding{
		TopicID:       topicID,
		SourceRoot:    source,
		WorkspaceRoot: isolated,
		WorktreeRoot:  isolated,
		Head:          "head-on-unbind",
	}); err != nil {
		t.Fatal(err)
	}
	if err := removeTopicWorktreeBinding(source, topicID); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadTopicWorktreeBinding(source, topicID); ok {
		t.Fatal("binding still present after remove")
	}
	if got := readTopicWorktreeCreatedHead(isolated); got != "head-on-unbind" {
		t.Fatalf("created-head after unbind = %q", got)
	}
}

func TestTabSessionDirPrefersSourceRoot(t *testing.T) {
	tab := &WorkspaceTab{
		SourceRoot:    "/proj/source",
		WorkspaceRoot: "/managed/wt",
	}
	got := tabSessionDir(tab)
	want := desktopSessionDir("/proj/source")
	if got != want {
		t.Fatalf("tabSessionDir = %q, want %q", got, want)
	}
}

func TestCreateTopicWorktreeSessionReopensUnderSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source-project")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := config.DeliveryWorktreeDir()
	isolatedRoot := filepath.Join(managed, "repo", "id", "source-project")
	if err := os.MkdirAll(isolatedRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	originalInspect := inspectTopicWorktree
	originalCreate := createTopicWorktree
	t.Cleanup(func() {
		inspectTopicWorktree = originalInspect
		createTopicWorktree = originalCreate
	})
	inspectTopicWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root, Branch: "main"}
	}
	createTopicWorktree = func(_ context.Context, gotSource, _ string) (worktree.Result, error) {
		wantSource, err := filepath.Abs(source)
		if err != nil {
			t.Fatal(err)
		}
		return worktree.Result{
			WorkspaceRoot: isolatedRoot,
			WorktreeRoot:  isolatedRoot,
			SourceRoot:    wantSource,
			Branch:        "reasonix/topic-reopen",
			Head:          "abc123",
		}, nil
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	result, err := app.CreateTopicWorktree(source)
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := result.Tab.SessionPath
	if sessionPath == "" {
		t.Fatal("expected session path on created tab")
	}

	// Wait for the async controller build to settle on the pinned session.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		tab := app.tabs[result.Tab.ID]
		path := ""
		ready := false
		if tab != nil {
			path = tab.currentSessionPath()
			ready = tab.Ready && tab.Ctrl != nil
		}
		app.mu.RUnlock()
		if ready && path != "" {
			sessionPath = path
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	sourceSessionDir := desktopSessionDir(result.SourceRoot)
	if !strings.HasPrefix(filepath.Clean(sessionPath), filepath.Clean(sourceSessionDir)+string(filepath.Separator)) {
		t.Fatalf("session path %q is not under source session dir %q", sessionPath, sourceSessionDir)
	}

	found, _ := app.findTopicSessionForTarget("project", result.SourceRoot, result.TopicID)
	if found != sessionPath {
		t.Fatalf("findTopicSessionForTarget = %q, want %q", found, sessionPath)
	}

	// Drop in-memory tabs so OpenProjectTab must rediscover the session from disk.
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{}
	app.tabOrder = nil
	app.activeTabID = ""
	app.mu.Unlock()

	reopened, err := app.OpenProjectTab(result.SourceRoot, result.TopicID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.SessionPath != sessionPath {
		t.Fatalf("reopen session = %q, want %q (blank topic regression)", reopened.SessionPath, sessionPath)
	}
	if reopened.WorkspaceRoot != isolatedRoot {
		t.Fatalf("reopen workspace = %q, want isolated %q", reopened.WorkspaceRoot, isolatedRoot)
	}
	if reopened.SourceRoot != result.SourceRoot {
		t.Fatalf("reopen sourceRoot = %q, want %q", reopened.SourceRoot, result.SourceRoot)
	}
}

func TestReclaimOrphanTopicWorktreeWithRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	managed := config.DeliveryWorktreeDir()
	created, err := worktree.CreateKind(context.Background(), repo, managed, worktree.BranchKindTopic)
	if err != nil {
		t.Fatal(err)
	}
	writeTopicWorktreeCreatedHead(created.WorktreeRoot, created.Head)

	// Bound: reclaim must leave it alone.
	if err := addProject(repo, ""); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicWorktreeBinding(repo, TopicWorktreeBinding{
		TopicID:       "bound",
		SourceRoot:    repo,
		WorkspaceRoot: created.WorkspaceRoot,
		WorktreeRoot:  created.WorktreeRoot,
		Head:          created.Head,
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.reclaimOrphanTopicWorktrees()
	if _, err := os.Stat(created.WorktreeRoot); err != nil {
		t.Fatalf("bound worktree reclaimed: %v", err)
	}

	// Unbind + pristine: reclaim should remove.
	if err := removeTopicWorktreeBinding(repo, "bound"); err != nil {
		t.Fatal(err)
	}
	app.reclaimOrphanTopicWorktrees()
	if _, err := os.Stat(created.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("unbound pristine worktree still present: %v", err)
	}
	if _, err := os.Stat(topicWorktreeCreatedHeadPath(created.WorktreeRoot)); !os.IsNotExist(err) {
		t.Fatalf("created-head sidecar still present after reclaim")
	}

	// Advanced orphan must survive.
	created2, err := worktree.CreateKind(context.Background(), repo, managed, worktree.BranchKindTopic)
	if err != nil {
		t.Fatal(err)
	}
	writeTopicWorktreeCreatedHead(created2.WorktreeRoot, created2.Head)
	cmd := exec.Command("git", "-C", created2.WorktreeRoot, "commit", "--allow-empty", "-m", "advance")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("advance: %v\n%s", err, out)
	}
	app.reclaimOrphanTopicWorktrees()
	if _, err := os.Stat(created2.WorktreeRoot); err != nil {
		t.Fatalf("advanced orphan reclaimed: %v", err)
	}
}

func TestTopicWorktreeListProjectTreeMarksOpenUnderSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source-project")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := config.DeliveryWorktreeDir()
	isolatedRoot := filepath.Join(managed, "repo", "id", "source-project")
	if err := os.MkdirAll(isolatedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	originalInspect := inspectTopicWorktree
	originalCreate := createTopicWorktree
	t.Cleanup(func() {
		inspectTopicWorktree = originalInspect
		createTopicWorktree = originalCreate
	})
	inspectTopicWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root, Branch: "main"}
	}
	createTopicWorktree = func(_ context.Context, gotSource, _ string) (worktree.Result, error) {
		abs, _ := filepath.Abs(source)
		return worktree.Result{
			WorkspaceRoot: isolatedRoot,
			WorktreeRoot:  isolatedRoot,
			SourceRoot:    abs,
			Branch:        "reasonix/topic-open",
			Head:          "abc123",
		}, nil
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	result, err := app.CreateTopicWorktree(source)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.RLock()
		tab := app.tabs[result.Tab.ID]
		ready := tab != nil && tab.Ready && tab.Ctrl != nil
		app.mu.RUnlock()
		if ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	found := false
	for _, project := range app.ListProjectTree() {
		if !sameProjectRoot(project.Root, result.SourceRoot) {
			continue
		}
		for _, topic := range project.Children {
			if topic.TopicID != result.TopicID {
				continue
			}
			found = true
			if !topic.Open {
				t.Fatalf("topic under source not marked open: %+v", topic)
			}
		}
	}
	if !found {
		t.Fatal("topic missing under source project in ListProjectTree")
	}
}

func TestOpenProjectTabAcceptsManagedTopicPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source-project")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := config.DeliveryWorktreeDir()
	isolatedRoot := filepath.Join(managed, "repo", "id", "source-project")
	if err := os.MkdirAll(isolatedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	originalInspect := inspectTopicWorktree
	originalCreate := createTopicWorktree
	t.Cleanup(func() {
		inspectTopicWorktree = originalInspect
		createTopicWorktree = originalCreate
	})
	inspectTopicWorktree = func(_ context.Context, root string) worktree.Availability {
		return worktree.Availability{Available: true, RepoRoot: root, Branch: "main"}
	}
	createTopicWorktree = func(_ context.Context, gotSource, _ string) (worktree.Result, error) {
		abs, _ := filepath.Abs(source)
		return worktree.Result{
			WorkspaceRoot: isolatedRoot,
			WorktreeRoot:  isolatedRoot,
			SourceRoot:    abs,
			Branch:        "reasonix/topic-managed-open",
			Head:          "abc123",
		}, nil
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	result, err := app.CreateTopicWorktree(source)
	if err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	app.tabs = map[string]*WorkspaceTab{}
	app.tabOrder = nil
	app.activeTabID = ""
	app.mu.Unlock()

	reopened, err := app.OpenProjectTab(isolatedRoot, result.TopicID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.SourceRoot != result.SourceRoot {
		t.Fatalf("sourceRoot = %q, want %q", reopened.SourceRoot, result.SourceRoot)
	}
	if reopened.WorkspaceRoot != isolatedRoot {
		t.Fatalf("workspace = %q, want %q", reopened.WorkspaceRoot, isolatedRoot)
	}
	for _, project := range app.ListProjectTree() {
		if sameProjectRoot(project.Root, isolatedRoot) {
			t.Fatalf("opening via managed path registered sibling: %+v", project)
		}
	}
}

func TestReclaimOrphanLegacyInTreeCreatedHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	managed := config.DeliveryWorktreeDir()
	created, err := worktree.CreateKind(context.Background(), repo, managed, worktree.BranchKindTopic)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the old in-tree marker that dirties git status.
	if err := os.WriteFile(filepath.Join(created.WorktreeRoot, ".reasonix-topic-created-head"), []byte(created.Head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.reclaimOrphanTopicWorktrees()
	if _, err := os.Stat(created.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("legacy in-tree marker blocked reclaim: %v", err)
	}
}

func TestTopicWorktreeLogicalRootUsedForMetaAndTitles(t *testing.T) {
	tab := &WorkspaceTab{
		Scope:         "project",
		SourceRoot:    "/proj/source",
		WorkspaceRoot: "/managed/wt",
		TopicID:       "topic-1",
		TopicTitle:    "Title",
	}
	if got := tabLogicalProjectRoot(tab); got != "/proj/source" {
		t.Fatalf("logical root = %q", got)
	}

	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath, err := createEmptySessionFile(desktopSessionDir(source), "")
	if err != nil {
		t.Fatal(err)
	}
	tab.SourceRoot = source
	tab.WorkspaceRoot = filepath.Join(config.DeliveryWorktreeDir(), "x", "y", "z")
	tab.SessionPath = sessionPath

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.saveTabSessionMeta(tab, sessionPath); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := agent.LoadBranchMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("meta ok=%v err=%v", ok, err)
	}
	if !sameProjectRoot(meta.WorkspaceRoot, source) {
		t.Fatalf("BranchMeta.WorkspaceRoot = %q, want source %q", meta.WorkspaceRoot, source)
	}
}

func TestFindTopicLocationPrefersSourceRoot(t *testing.T) {
	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.mu.Lock()
	app.tabs["t1"] = &WorkspaceTab{
		ID:            "t1",
		Scope:         "project",
		SourceRoot:    "/proj/source",
		WorkspaceRoot: "/managed/wt",
		TopicID:       "topic-loc",
	}
	app.mu.Unlock()
	scope, root, ok := app.findTopicLocation("topic-loc")
	if !ok || scope != "project" {
		t.Fatalf("location = %s %q ok=%v", scope, root, ok)
	}
	if root != normalizeProjectRoot("/proj/source") {
		t.Fatalf("root = %q, want source", root)
	}
}

func TestInheritTopicWorktreeBindingCopiesRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))

	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	isolated := filepath.Join(config.DeliveryWorktreeDir(), "r", "i", "p")
	if err := saveTopicWorktreeBinding(source, TopicWorktreeBinding{
		TopicID:       "old",
		SourceRoot:    source,
		WorkspaceRoot: isolated,
		WorktreeRoot:  isolated,
		Branch:        "reasonix/topic-old",
		Head:          "deadbeef",
	}); err != nil {
		t.Fatal(err)
	}
	inheritTopicWorktreeBinding(source, "old", "new")
	binding, ok := loadTopicWorktreeBinding(source, "new")
	if !ok {
		t.Fatal("expected inherited binding")
	}
	if binding.WorkspaceRoot != isolated || binding.Head != "deadbeef" {
		t.Fatalf("inherited = %+v", binding)
	}
	if _, ok := loadTopicWorktreeBinding(source, "old"); !ok {
		t.Fatal("source binding should remain")
	}
}
