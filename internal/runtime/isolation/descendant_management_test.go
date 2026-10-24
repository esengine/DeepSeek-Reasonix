package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func descendantFixture(t *testing.T) (*Store, *Entry, *Store, *Entry) {
	t.Helper()
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.endRun(parent) })
	children := NewStore(s.managedRoot, parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	child, err := s.BeginChild(t.Context(), parent, children, "child")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { children.endRun(child) })
	return s, parent, children, child
}

func TestRootCatalogAndPreviewReachOnlyRegisteredDescendants(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	unregistered, err := children.BeginOwned(t.Context(), parent.Cand.WorkspaceRoot, "unregistered")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { children.endRun(unregistered) })
	s.endRun(parent)
	write(t, child.Cand.WorkspaceRoot, "child.txt", "retained child\n")
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.Catalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range catalog.Results {
		if row.ID == unregistered.ID {
			t.Fatal("unregistered candidate was exposed through a related workspace")
		}
		if row.ID == child.ID {
			found = true
			if row.ParentID != parent.ID || !sameDir(row.TargetWorkspace, parent.Cand.WorkspaceRoot) {
				t.Fatalf("descendant lost its direct target identity: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("retained child is unreachable from its root catalog")
	}
	if _, err := s.PreviewDiscard(t.Context(), unregistered.ID); refusalCodeOf(err) != CodeUnknownID {
		t.Fatalf("unregistered candidate acquired root management authority: %v", err)
	}
	preview, err := s.PreviewDiscard(t.Context(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardResultExpected(t.Context(), child.ID, preview.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(child.Cand.WorktreeRoot); !os.IsNotExist(err) {
		t.Fatalf("child cleanup: %v", err)
	}
	if _, err := os.Stat(parent.Cand.WorktreeRoot); err != nil {
		t.Fatalf("discard erased direct parent: %v", err)
	}
	resumed, err := s.Resume(t.Context(), parent.ID, "parent")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(context.Background(), resumed) })
}

func TestRootDescendantPreviewRechecksBacklinkWithoutRewritingRecords(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	s.endRun(parent)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	path := entryRecordPath(child)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	lineage := *child.Lineage
	backlink := *lineage.Parent
	backlink.Token = "another-registration"
	lineage.Parent = &backlink
	fields["lineage"], _ = json.Marshal(lineage)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewDiscard(t.Context(), child.ID); err == nil {
		t.Fatal("forged backlink reached destructive preview")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("failed descendant resolution rewrote the child record")
	}
}

func TestRootManagementTraversesDirectParentsAndPreservesSource(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	grandchildren := NewStore(s.managedRoot, child.Cand.WorkspaceRoot, child.Cand.Repo)
	grandchild, err := children.BeginChild(t.Context(), child, grandchildren, "grandchild")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { grandchildren.endRun(grandchild) })
	write(t, grandchild.Cand.WorkspaceRoot, "grandchild.txt", "retained\n")
	if _, err := grandchildren.Finish(t.Context(), grandchild); err != nil {
		t.Fatal(err)
	}
	s.endRun(parent)
	children.endRun(child)
	preview, err := s.PreviewDiscard(t.Context(), grandchild.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DiscardResultExpected(t.Context(), grandchild.ID, preview.Revision); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{s.workspace, parent.Cand.WorkspaceRoot, child.Cand.WorkspaceRoot} {
		if _, err := os.Stat(filepath.Join(root, "grandchild.txt")); !os.IsNotExist(err) {
			t.Fatalf("descendant management wrote into an ancestor: %s %v", root, err)
		}
	}
}

func TestParentMarkerPromotionPreservesImmutableChildBacklinks(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	parent, err := s.BeginOwned(t.Context(), repo, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(entryPath(parent.Cand), filepath.Join(filepath.Dir(parent.Cand.WorktreeRoot), legacyEntryFile)); err != nil {
		t.Fatal(err)
	}
	parent, err = s.Resume(t.Context(), parent.ID, "parent")
	if err != nil {
		t.Fatal(err)
	}
	children := NewStore(s.managedRoot, parent.Cand.WorkspaceRoot, parent.Cand.Repo)
	child, err := s.BeginChild(t.Context(), parent, children, "child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	if _, err := children.Discard(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), parent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Discard(t.Context(), parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Discard(t.Context(), parent.ID); err != nil {
		t.Fatalf("terminal promotion invalidated the settled child's immutable backlink: %v", err)
	}
	sibling, err := s.BeginOwned(t.Context(), repo, "sibling")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), sibling); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewDiscard(t.Context(), sibling.ID); err != nil {
		t.Fatalf("promoted parent poisoned an unrelated result's management route: %v", err)
	}
	catalog, err := s.Catalog(t.Context())
	if err != nil || len(catalog.Diagnostics) != 0 {
		t.Fatalf("valid terminal promotion made the dependency graph unsupported: %+v %v", catalog.Diagnostics, err)
	}
}

func TestRunningParentCannotUseTerminalMarkerAlias(t *testing.T) {
	s, parent, children, child := descendantFixture(t)
	s.endRun(parent)
	if _, err := children.Finish(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	path := entryRecordPath(child)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	lineage := *child.Lineage
	link := *lineage.Parent
	link.Record = filepath.Join(filepath.Dir(entryPath(parent.Cand)), legacyEntryFile)
	lineage.Parent = &link
	fields["lineage"], _ = json.Marshal(lineage)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PreviewDiscard(t.Context(), child.ID); err == nil {
		t.Fatal("a running parent's unrelated marker acquired terminal-promotion authority")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("rejected marker alias rewrote the child record")
	}
}
