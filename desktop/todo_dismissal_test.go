package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
)

func TestDismissTodoBatchPersistsOnSessionAndParent(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	if err := os.WriteFile(parent, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(leaf, agent.BranchMeta{ID: "leaf", ParentID: "parent"}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {ID: "tab-1", SessionPath: leaf}}
	app.activeTabID = "tab-1"

	if err := app.DismissTodoBatchForTab("tab-1", `[{"content":"Ship"}]`); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{leaf, parent} {
		got, ok, err := agent.LoadBranchMeta(path)
		if err != nil || !ok {
			t.Fatalf("LoadBranchMeta(%s) = %+v ok=%v err=%v", path, got, ok, err)
		}
		if len(got.DismissedTodoBatches) != 1 || got.DismissedTodoBatches[0] != `[{"content":"Ship"}]` {
			t.Fatalf("%s dismissed batches = %#v", path, got.DismissedTodoBatches)
		}
	}
}

func TestMetaForTabSurfacesParentDismissedTodoBatches(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	if err := os.WriteFile(parent, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(parent, agent.BranchMeta{
		ID: "parent", DismissedTodoBatches: []string{`[{"content":"Ship"}]`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(leaf, agent.BranchMeta{ID: "leaf", ParentID: "parent"}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {ID: "tab-1", SessionPath: leaf, Ready: true}}
	app.activeTabID = "tab-1"

	got := app.dismissedTodoBatchesForSession(leaf)
	if len(got) != 1 || got[0] != `[{"content":"Ship"}]` {
		t.Fatalf("lineage batches = %#v", got)
	}

	meta := app.MetaForTab("tab-1")
	if len(meta.DismissedTodoBatches) != 1 || meta.DismissedTodoBatches[0] != `[{"content":"Ship"}]` {
		t.Fatalf("meta dismissed batches = %#v", meta.DismissedTodoBatches)
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"dismissedTodoBatches"`) {
		t.Fatalf("meta JSON missing dismissedTodoBatches: %s", raw)
	}
}

func TestDismissedTodoBatchesEmptySliceNotNull(t *testing.T) {
	meta := Meta{DismissedTodoBatches: []string{}}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"dismissedTodoBatches":null`) {
		t.Fatalf("empty dismissed batches encoded as null: %s", raw)
	}
}

func TestDismissTodoBatchSkipsMissingTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if err := app.DismissTodoBatchForTab("missing", "batch"); err == nil {
		t.Fatal("missing tab should fail")
	}
}

func TestMetaForTabDoesNotReadSiblingCoveringLeaf(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	if err := os.WriteFile(parent, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leaf, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(parent, agent.BranchMeta{ID: "parent"}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(leaf, agent.BranchMeta{
		ID: "leaf", ParentID: "parent", DismissedTodoBatches: []string{`[{"content":"Ship"}]`},
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {ID: "tab-1", SessionPath: parent, Ready: true}}
	app.activeTabID = "tab-1"
	if got := app.dismissedTodoBatchesForSession(parent); len(got) != 0 {
		t.Fatalf("read path saw covering-leaf batches = %#v", got)
	}
}

func TestParentSessionPathRejectsTraversal(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "escaped.jsonl")
	leaf := filepath.Join(dir, "leaf.jsonl")
	if err := os.WriteFile(leaf, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(leaf, agent.BranchMeta{ID: "leaf", ParentID: filepath.Join("..", "escaped")}); err != nil {
		t.Fatal(err)
	}
	if got := parentSessionPathForTodoDismissal(leaf); got != "" {
		t.Fatalf("traversal parent = %q", got)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {ID: "tab-1", SessionPath: leaf}}
	if err := app.DismissTodoBatchForTab("tab-1", `[{"content":"Ship"}]`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(agent.BranchMetaPath(outside)); !os.IsNotExist(err) {
		t.Fatalf("traversal wrote outside the session dir: %v", err)
	}
	if !safeTodoDismissalParentID("parent") || safeTodoDismissalParentID("../escaped") || safeTodoDismissalParentID(`foo/bar`) {
		t.Fatal("parent id sanitizer accepted a path-shaped id")
	}
}
