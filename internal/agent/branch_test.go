package agent

import (
	"path/filepath"
	"testing"
	"time"

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

func TestListBranchesSkipsCleanupPending(t *testing.T) {
	dir := t.TempDir()
	visiblePath := filepath.Join(dir, "visible.jsonl")
	pendingPath := filepath.Join(dir, "pending.jsonl")

	visible := NewSession("sys")
	visible.Add(provider.Message{Role: provider.RoleUser, Content: "visible prompt"})
	if err := visible.Save(visiblePath); err != nil {
		t.Fatal(err)
	}
	if err := TouchBranchMeta(visiblePath); err != nil {
		t.Fatal(err)
	}

	pending := NewSession("sys")
	pending.Add(provider.Message{Role: provider.RoleUser, Content: "pending prompt"})
	if err := pending.Save(pendingPath); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranchMeta(pendingPath, BranchMeta{Name: "pending experiment"}); err != nil {
		t.Fatal(err)
	}
	if err := MarkCleanupPending(pendingPath, "delete"); err != nil {
		t.Fatal(err)
	}

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 {
		t.Fatalf("branches = %d, want 1: %+v", len(branches), branches)
	}
	if branches[0].Path != visiblePath {
		t.Fatalf("listed branch path = %q, want %q", branches[0].Path, visiblePath)
	}
}

func TestSessionInFlightTurnMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in-flight.jsonl")
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "work"})
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := TouchBranchMeta(path); err != nil {
		t.Fatal(err)
	}
	before, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	updatedAt := before.UpdatedAt

	if err := MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}
	marked, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta marked ok=%v err=%v", ok, err)
	}
	if marked.InFlightTurn == nil {
		t.Fatal("in-flight turn marker missing")
	}
	if marked.InFlightTurn.StartMessageIndex != 1 || !marked.InFlightTurn.PreserveUser {
		t.Fatalf("in-flight marker = %+v, want index=1 preserveUser=true", marked.InFlightTurn)
	}
	if marked.InFlightTurn.StartedAt.IsZero() || time.Since(marked.InFlightTurn.StartedAt) > time.Minute {
		t.Fatalf("unexpected marker timestamp: %v", marked.InFlightTurn.StartedAt)
	}
	if !marked.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("MarkSessionInFlightTurn updated activity time: got %v want %v", marked.UpdatedAt, updatedAt)
	}

	if err := UpdateSessionMeta(path, "model-a", "preview", 1, true); err != nil {
		t.Fatal(err)
	}
	refreshed, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta refreshed ok=%v err=%v", ok, err)
	}
	if refreshed.InFlightTurn == nil {
		t.Fatal("UpdateSessionMeta dropped in-flight marker")
	}
	if refreshed.InFlightTurn.StartMessageIndex != 1 || !refreshed.InFlightTurn.PreserveUser {
		t.Fatalf("refreshed in-flight marker = %+v, want index=1 preserveUser=true", refreshed.InFlightTurn)
	}
	updatedAt = refreshed.UpdatedAt

	if err := ClearSessionInFlightTurn(path); err != nil {
		t.Fatal(err)
	}
	cleared, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta cleared ok=%v err=%v", ok, err)
	}
	if cleared.InFlightTurn != nil {
		t.Fatalf("in-flight marker survived clear: %+v", cleared.InFlightTurn)
	}
	if !cleared.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("ClearSessionInFlightTurn updated activity time: got %v want %v", cleared.UpdatedAt, updatedAt)
	}
}

func TestSessionModelRoundTripPreservesActivity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	session := NewSession("sys")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadSessionModel(path); ok {
		t.Fatal("fresh session should not have a stored model")
	}
	meta, err := EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := SetBranchModelPreserveUpdated(path, "openrouter/anthropic/claude-sonnet"); err != nil {
		t.Fatal(err)
	}
	model, ok := LoadSessionModel(path)
	if !ok || model != "openrouter/anthropic/claude-sonnet" {
		t.Fatalf("LoadSessionModel = %q, %v", model, ok)
	}
	updated, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if !updated.UpdatedAt.Equal(meta.UpdatedAt) {
		t.Fatalf("model write refreshed activity: before=%s after=%s", meta.UpdatedAt, updated.UpdatedAt)
	}
}
