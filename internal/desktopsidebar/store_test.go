package desktopsidebar

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateTopicAndBuildTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))

	root := filepath.Join(home, "proj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(root); err != nil {
		t.Fatal(err)
	}
	meta, err := CreateTopic("project", root, "设计稿")
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID == "" || meta.Title != "设计稿" {
		t.Fatalf("meta=%+v", meta)
	}
	tree := BuildTree(nil, []SessionHint{{
		WorkspaceRoot: root,
		TopicID:       meta.ID,
		TopicTitle:    "设计稿",
		Turns:         2,
		Path:          filepath.Join(home, "s.jsonl"),
	}})
	if len(tree) != 1 {
		t.Fatalf("tree len=%d", len(tree))
	}
	if tree[0].Kind != "project" || tree[0].Root != root {
		t.Fatalf("project node=%+v", tree[0])
	}
	found := false
	for _, c := range tree[0].Children {
		if c.TopicID == meta.ID && c.Label == "设计稿" {
			found = true
		}
	}
	if !found {
		t.Fatalf("topic not in children: %+v", tree[0].Children)
	}
	if err := RenameTopic(root, meta.ID, "新标题"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTopic(root, meta.ID); err != nil {
		t.Fatal(err)
	}
	if err := RemoveWorkspace(root); err != nil {
		t.Fatal(err)
	}
}
