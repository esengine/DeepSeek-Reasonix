package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTopicStateWritesSkipDeletedWorkspaceRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace-a")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicTitles(root, map[string]string{"topic_1": "kept"}); err != nil {
		t.Fatalf("saveTopicTitles on a live root: %v", err)
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := saveTopicTitles(root, map[string]string{"topic_1": "resurrected"}); err == nil {
		t.Fatal("writing topic titles into a deleted workspace must fail instead of recreating it")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("workspace root was recreated: %v", err)
	}
}

func TestTopicStateWritesStillCreateGlobalDir(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())

	if err := saveTopicTitles("", map[string]string{"topic_1": "global"}); err != nil {
		t.Fatalf("global topic titles must still be writable: %v", err)
	}
	if got := loadTopicTitles("")["topic_1"]; got != "global" {
		t.Fatalf("global topic title = %q, want %q", got, "global")
	}
}
