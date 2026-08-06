package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

	app := NewApp()
	t.Cleanup(func() { app.shutdown(context.Background()) })
	app.registerProjectRoot(isolated)
	app.registerProjectRoot(source)

	for _, project := range app.ListProjectTree() {
		if sameProjectRoot(project.Root, isolated) {
			t.Fatalf("managed path registered: %+v", project)
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
