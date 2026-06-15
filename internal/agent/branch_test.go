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
