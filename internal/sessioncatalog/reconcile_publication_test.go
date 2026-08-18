package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
)

func TestReconcilePublishesOnlyCompletedDirectorySnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	const sessionCount = 65
	for i := range sessionCount {
		path := filepath.Join(dir, fmt.Sprintf("chat-%03d.jsonl", i))
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := agent.SaveBranchMeta(path, agent.BranchMeta{
			Scope:         "project",
			WorkspaceRoot: "/workspace",
			TopicID:       fmt.Sprintf("topic-%03d", i),
			TopicTitle:    fmt.Sprintf("Topic %03d", i),
			SchemaVersion: agent.BranchMetaCountsVersion,
			Turns:         1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	events := make(chan string, sessionCount+1)
	catalog, err := Open(ctx, Options{
		InMemory:      true,
		DisableRepair: true,
		OnRevision: func(_ uint64, _ []string, reason string) {
			events <- reason
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })

	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{
		Path:          dir,
		Scope:         "project",
		WorkspaceRoot: "/workspace",
	}); err != nil {
		t.Fatal(err)
	}
	close(events)
	published := make([]string, 0, len(events))
	for reason := range events {
		published = append(published, reason)
	}
	if len(published) != 1 || published[0] != "reconcile_complete" {
		t.Fatalf("published reasons = %q, want only reconcile_complete", published)
	}

	page, err := catalog.ListTopics(ctx, TopicPageRequest{
		Scope:         "project",
		WorkspaceRoot: "/workspace",
		Limit:         sessionCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != sessionCount {
		t.Fatalf("final page items = %d, want %d", len(page.Items), sessionCount)
	}
}
