package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
)

func TestRenameTopicCatalogOnlyProjectTopic(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := robustTempDir(t)
	if err := addProject(root, "Catalog Only Project"); err != nil {
		t.Fatalf("add project: %v", err)
	}
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	topicID := "topic_catalog_only"
	// The session exists in the project session dir with branch metadata, but
	// no topic title index entry and no open tab: the catalog is the only
	// place that knows the topic.
	writeTopicSessionWithPrompt(t, dir, "catalog-only.jsonl", topicID, "旧标题", root, "catalog only prompt", time.Now())

	app := NewApp()
	installSessionCatalogForTest(t, app, dir, "project", root)

	if err := app.RenameTopic(topicID, "新标题"); err != nil {
		t.Fatalf("rename catalog-only topic: %v", err)
	}
	if got := loadTopicTitle(root, topicID); got != "新标题" {
		t.Fatalf("catalog-only topic title = %q, want 新标题", got)
	}
	if meta, ok, err := agent.LoadBranchMeta(filepath.Join(dir, "catalog-only.jsonl")); err != nil || !ok || meta.TopicTitle != "新标题" {
		t.Fatalf("branch meta title = %q ok=%v err=%v, want 新标题", meta.TopicTitle, ok, err)
	}
}

func TestRenameTopicUnknownTopicStillFails(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if err := app.RenameTopic("topic_does_not_exist", "标题"); err == nil {
		t.Fatal("rename of an unknown topic should fail")
	}
}
