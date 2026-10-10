package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

func seedRecoveryArchiveLineage(t *testing.T) []string {
	t.Helper()
	dir := testenv.TempDir(t)
	root := filepath.Join(dir, "20260803-140947-legacy.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "legacy prompt"})
	if err := base.Save(root); err != nil {
		t.Fatal(err)
	}
	parent := root
	var paths []string
	for i := range 3 {
		copy := NewSession("sys")
		copy.Add(provider.Message{Role: provider.RoleUser, Content: "legacy prompt"})
		copy.Add(provider.Message{Role: provider.RoleAssistant, Content: string(rune('a' + i))})
		info, err := copy.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: parent})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, info.Path)
		parent = info.Path
	}
	if err := store.RemoveSessionArtifacts(root); err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestSetSessionLineageArchivedRollsBackPartialWrites(t *testing.T) {
	paths := seedRecoveryArchiveLineage(t)
	failAt := paths[1]
	failed := false
	save := func(path string, meta BranchMeta, touchUpdated bool) error {
		if path == failAt && !failed {
			failed = true
			return errors.New("injected archive write failure")
		}
		return saveBranchMeta(path, meta, touchUpdated)
	}

	if err := setSessionLineageArchivedWith(paths[0], true, nil, save); err == nil {
		t.Fatal("lineage archive succeeded despite an injected write failure")
	}
	for _, path := range paths {
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok {
			t.Fatalf("load rolled-back sibling %s: ok=%v err=%v", filepath.Base(path), ok, err)
		}
		if meta.Archived {
			t.Errorf("sibling %s stayed archived after rollback", filepath.Base(path))
		}
		if _, err := os.Stat(BranchMetaPath(path)); err != nil {
			t.Fatalf("rolled-back sidecar %s is missing: %v", filepath.Base(BranchMetaPath(path)), err)
		}
	}
}

func TestSetSessionLineageArchivedLeavesAnotherLineageUntouched(t *testing.T) {
	paths := seedRecoveryArchiveLineage(t)
	dir := filepath.Dir(paths[0])
	otherRoot := filepath.Join(dir, "20260803-150000-other.jsonl")
	base := NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "other prompt"})
	if err := base.Save(otherRoot); err != nil {
		t.Fatal(err)
	}
	other := NewSession("sys")
	other.Add(provider.Message{Role: provider.RoleUser, Content: "other prompt"})
	other.Add(provider.Message{Role: provider.RoleAssistant, Content: "other reply"})
	info, err := other.SaveRecoveryBranch(RecoveryBranchOptions{OriginalPath: otherRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSessionArtifacts(otherRoot); err != nil {
		t.Fatal(err)
	}

	if err := SetSessionLineageArchived(paths[0], true); err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(info.Path)
	if err != nil || !ok {
		t.Fatalf("load unrelated sibling: ok=%v err=%v", ok, err)
	}
	if meta.Archived {
		t.Fatal("archiving one recovery lineage archived an unrelated conversation")
	}
}
