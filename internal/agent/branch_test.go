package agent

import (
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

func TestBranchMetaRoundTripAndList(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.jsonl")
	childPath := filepath.Join(dir, "child.jsonl")

	root := NewSession("sys")
	root.Add(provider.Message{Role: provider.RoleUser, Content: "root prompt"})
	if err := root.Save(rootPath); err != nil {
		t.Fatal(err)
	}
	if err := TouchBranchMeta(rootPath); err != nil {
		t.Fatal(err)
	}

	child := NewSession("sys")
	child.Add(provider.Message{Role: provider.RoleUser, Content: "child prompt"})
	if err := child.Save(childPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(childPath, BranchMeta{Name: "experiment", ParentID: BranchID(rootPath), ForkTurn: 2}); err != nil {
		t.Fatal(err)
	}

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatalf("branches = %d, want 2", len(branches))
	}
	var rootFound, childFound bool
	for _, b := range branches {
		if b.ID == "root" {
			rootFound = true
		}
		if b.ParentID == "root" && b.Name == "experiment" {
			childFound = true
		}
	}
	if !rootFound {
		t.Fatal("root branch not found")
	}
	if !childFound {
		t.Fatalf("child with parent root and name experiment not found among %+v", branches)
	}
}

func TestBranchMetaModelRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	s := NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	// Model should be absent initially.
	if m, ok := LoadSessionModel(path); ok {
		t.Fatalf("LoadSessionModel on fresh session = %q, want empty", m)
	}

	// Set model and verify it is persisted.
	if err := SetBranchModel(path, "openrouter/anthropic/claude-sonnet"); err != nil {
		t.Fatal(err)
	}
	m, ok := LoadSessionModel(path)
	if !ok {
		t.Fatal("LoadSessionModel failed after SetBranchModel")
	}
	if m != "openrouter/anthropic/claude-sonnet" {
		t.Fatalf("model = %q, want %q", m, "openrouter/anthropic/claude-sonnet")
	}

	// Update model and verify.
	if err := SetBranchModel(path, "deepseek/deepseek-v4-flash"); err != nil {
		t.Fatal(err)
	}
	m, ok = LoadSessionModel(path)
	if !ok {
		t.Fatal("LoadSessionModel failed after update")
	}
	if m != "deepseek/deepseek-v4-flash" {
		t.Fatalf("model = %q, want %q", m, "deepseek/deepseek-v4-flash")
	}

	// LoadSessionModel returns false for a non-existent file.
	if _, ok := LoadSessionModel(filepath.Join(dir, "nonexistent.jsonl")); ok {
		t.Fatal("LoadSessionModel on nonexistent path returned ok")
	}
}
