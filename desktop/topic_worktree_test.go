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
			WorktreeRoot:  filepath.Dir(isolatedRoot),
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
}
