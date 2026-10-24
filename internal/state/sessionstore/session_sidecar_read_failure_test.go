package sessionstore

// A branch-meta sidecar that cannot be read is not an absent one. These tests
// cover the two listing paths that used to fold a read error into the zero
// value: the recovery-archive lineage walk, which then archived a partial row
// and reported success, and ListBranches, which dropped the branch outright.
import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func TestAuditUnreadableSiblingMetaIsNotAnAbsentSibling(t *testing.T) {
	paths := seedRecoveryArchiveLineage(t)
	control, err := RecoveryLineagePaths(paths[0])
	if err != nil || len(control) != 3 {
		t.Fatalf("control: intact lineage = %v (err=%v), want all three copies", control, err)
	}

	metaPath := BranchMetaPath(paths[1])
	b, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	sibs, err := RecoveryLineagePaths(paths[0])
	if err != nil {
		return // reporting the unreadable sibling is an acceptable contract
	}
	if len(sibs) != 3 {
		t.Fatalf("lineage = %d of 3 copies (%v), want the unreadable sibling still counted, or an error", len(sibs), sibs)
	}
}

// The transaction that consumes the lineage must not report success while a
// copy of the same row stays active.
func TestAuditUnreadableSiblingMetaDoesNotReportArchiveSuccess(t *testing.T) {
	paths := seedRecoveryArchiveLineage(t)
	metaPath := BranchMetaPath(paths[1])
	b, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SetSessionLineageArchived(paths[0], true); err != nil {
		return // reporting the unreadable sibling is an acceptable contract
	}
	t.Fatalf("SetSessionLineageArchived reported success while %s could not be read; the copy is still active",
		filepath.Base(paths[1]))
}

// Control: an intact lineage still reports success and archives every copy, so
// the read-failure discipline cannot be satisfied by refusing every archive.
func TestSetSessionLineageArchivedStillArchivesAnIntactLineage(t *testing.T) {
	paths := seedRecoveryArchiveLineage(t)
	if err := SetSessionLineageArchived(paths[0], true); err != nil {
		t.Fatalf("intact lineage archive = %v, want success", err)
	}
	for _, path := range paths {
		meta, ok, err := LoadBranchMeta(path)
		if err != nil || !ok {
			t.Fatalf("load archived copy %s: ok=%v err=%v", filepath.Base(path), ok, err)
		}
		if !meta.Archived {
			t.Errorf("copy %s is still active after the lineage archive reported success", filepath.Base(path))
		}
	}
}

// Control: a *missing* sidecar is a legitimate state and keeps today's
// include-with-defaults behaviour.
func TestListBranchesStillDefaultsAMissingSidecar(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "20260803-140947-legacy-recovery-cccc.jsonl")
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "kept", RawContent: "kept"})
	if err := live.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(BranchMetaPath(path)); err != nil {
		t.Fatal(err)
	}

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, br := range branches {
		if br.Path != path {
			continue
		}
		if br.ID != BranchID(path) {
			t.Fatalf("defaulted branch ID = %q, want %q", br.ID, BranchID(path))
		}
		if br.CreatedAt.IsZero() {
			t.Fatal("a branch with no sidecar must keep a default CreatedAt, not a zero one")
		}
		return
	}
	t.Fatalf("a missing sidecar dropped %s from the listing", filepath.Base(path))
}

// An undecodable sidecar must be treated exactly like a missing one: defaults
// plus inclusion. Dropping it made interruptedTurnContinuedOnRecoveryBranch
// answer "the turn did not continue there" and strip the turn's messages.
func TestAuditUnreadableMetaDropsTheBranchInsteadOfDefaulting(t *testing.T) {
	dir := testenv.TempDir(t)

	// A recovery copy whose sidecar was never written.
	missing := filepath.Join(dir, "20260803-140947-legacy-recovery-aaaa.jsonl")
	live := NewSession("sys")
	live.Add(provider.Message{Role: provider.RoleUser, Content: "kept", RawContent: "kept"})
	if err := live.SaveSnapshot(missing); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(BranchMetaPath(missing)); err != nil {
		t.Fatal(err)
	}

	// The same transcript with a sidecar that cannot be decoded.
	corrupt := filepath.Join(dir, "20260803-140947-legacy-recovery-bbbb.jsonl")
	if err := live.SaveSnapshot(corrupt); err != nil {
		t.Fatal(err)
	}
	metaPath := BranchMetaPath(corrupt)
	b, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	branches, err := ListBranches(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, br := range branches {
		seen[br.Path] = true
	}
	if !seen[missing] {
		t.Fatalf("control: a missing sidecar must still list %s", filepath.Base(missing))
	}
	if !seen[corrupt] {
		t.Fatalf("branches = %d, an undecodable sidecar dropped %s from the list while a missing one kept it",
			len(branches), filepath.Base(corrupt))
	}
}

// A sidecar whose bytes are all NUL is what NTFS leaves when power is lost
// between extending a file and flushing its data: it records nothing, so it is
// an absent sidecar rather than one that blocks every lineage walk.
func TestNULFilledSiblingMetaReadsAsAbsent(t *testing.T) {
	paths := seedRecoveryArchiveLineage(t)
	if err := os.WriteFile(BranchMetaPath(paths[1]), make([]byte, 512), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ok, err := LoadBranchMeta(paths[1]); err != nil || ok {
		t.Fatalf("LoadBranchMeta(NUL-filled) ok=%v err=%v, want absent", ok, err)
	}
	if err := SetSessionLineageArchived(paths[0], true); err != nil {
		t.Fatalf("archive beside a NUL-filled sidecar = %v, want success", err)
	}
	if err := SetSessionArchived(paths[1], true); err != nil {
		t.Fatalf("archive the NUL-filled copy itself = %v, want its sidecar rewritten", err)
	}
	if meta, ok, err := LoadBranchMeta(paths[1]); err != nil || !ok || !meta.Archived {
		t.Fatalf("rewritten sidecar: meta=%+v ok=%v err=%v", meta, ok, err)
	}
}
