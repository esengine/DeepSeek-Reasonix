package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// failIndexPublishIn makes publishing MEMORY.md in dir fail, after the fact moved
// into .archive and before the index is published. That failure must not decide
// whether the fact is archived.
func failIndexPublishIn(t *testing.T, dir string) {
	t.Helper()
	indexPath := filepath.Join(dir, indexFile)
	withFault(t, func(op, path string) error {
		if op == "publish-index" && path == indexPath {
			return &os.PathError{Op: "rename", Path: path, Err: os.ErrPermission}
		}
		return nil
	})
}

func stopInjecting(string) { memoryFault = nil }

// assertIndexesDescribeTheirFiles holds the invariant to one direction: every
// managed MEMORY.md line names a file that is active in that directory.
func assertIndexesDescribeTheirFiles(t *testing.T, dir string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, indexFile))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		mt := indexLineRe.FindStringSubmatch(line)
		if mt == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, mt[1]+".md")); err != nil {
			t.Fatalf("MEMORY.md in %s links %s.md, which is not an active fact: %v", dir, mt[1], err)
		}
	}
}

func TestArchiveReportsFailureWhenTheIndexCannotBePublished(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	if _, err := s.Save(Memory{Name: "alpha", Description: "goes away", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	failIndexPublishIn(t, dir)

	if _, err := NewForgetTool(s).Execute(context.Background(), json.RawMessage(`{"name":"alpha"}`)); err == nil {
		t.Fatal("forget reported success although MEMORY.md was never published")
	}
	stopInjecting(dir)
	assertIndexesDescribeTheirFiles(t, dir)
	if _, err := s.Archive("alpha"); err != nil {
		t.Fatalf("retry after a failed archive: %v", err)
	}
	assertIndexesDescribeTheirFiles(t, dir)
}

// The released code could leave the file inside .archive while MEMORY.md still
// named it. Retrying by the stable id the tools are told to use must reconcile
// that store instead of reporting success and repairing nothing.
func TestArchiveByIDRepairsAnIndexLineLeftStale(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	if _, err := s.Save(Memory{Name: "alpha", Description: "goes away", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	active, ok := mustStore(t, s).Read("alpha")
	if !ok {
		t.Fatal("alpha is not active after save")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(dir, ".archive", "20260101-000000.000-alpha.md")
	if err := os.Rename(filepath.Join(dir, "alpha.md"), moved); err != nil {
		t.Fatal(err)
	}
	got, err := s.Archive(active.ID)
	if err != nil {
		t.Fatalf("retry by stable id: %v", err)
	}
	if got != moved {
		t.Fatalf("retry by stable id reported %q, want the archived copy %q", got, moved)
	}
	assertIndexesDescribeTheirFiles(t, dir)
}

// A name that was archived long ago, with no stale managed line left behind, is
// simply absent: reporting a path for it makes forget claim a deletion it did
// not perform.
func TestArchiveOfAHistoricNameWithoutAStaleLineReportsNothingArchived(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	if _, err := s.Save(Memory{Name: "alpha", Description: "old", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive("alpha"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Archive("alpha")
	if err != nil {
		t.Fatalf("second archive of an absent fact: %v", err)
	}
	if got != "" {
		t.Fatalf("archiving an already-absent fact reported %q; nothing was archived by this call", got)
	}
	assertIndexesDescribeTheirFiles(t, dir)
}

// An index read that fails after the fact has already moved would leave the
// archive and the index disagreeing; the operation must instead end either fully
// archived or untouched.
func TestArchiveLeavesNoHalfCommitWhenAnIndexReadFails(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	if _, err := s.Save(Memory{Name: "alpha", Description: "goes away", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	reads := 0
	withFault(t, func(op, path string) error {
		if op != "read-index" {
			return nil
		}
		reads++
		if reads >= 2 {
			return &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
		}
		return nil
	})
	_, err := s.Archive("alpha")
	memoryFault = nil
	_, stillActive := mustStore(t, s).Read("alpha")
	assertIndexesDescribeTheirFiles(t, dir)
	if err == nil && stillActive {
		t.Fatal("archive reported success but the fact is still active")
	}
	if err != nil && !stillActive {
		t.Fatal("a failed archive moved the fact anyway: the error and the disk disagree")
	}
	if err != nil && stillActive {
		return // failed and untouched: the allowed outcome
	}
	if err == nil {
		if _, archived := os.Stat(filepath.Join(dir, ".archive")); archived != nil {
			t.Fatalf("archive reported success with no archive directory: %v", archived)
		}
	}
}

// A repair that cannot write the index must surface that failure rather than
// reporting that nothing needed repairing.
func TestRepairArchivedRefSurfacesAFlushFailure(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	if _, err := s.Save(Memory{Name: "alpha", Description: "gone", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	active, ok := mustStore(t, s).Read("alpha")
	if !ok {
		t.Fatal("alpha is not active after save")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "alpha.md"), filepath.Join(dir, ".archive", "20260101-000000.000-alpha.md")); err != nil {
		t.Fatal(err)
	}
	failIndexPublishIn(t, dir)
	_, _, err := s.repairArchivedRef(active.ID)
	stopInjecting(dir)
	if err == nil {
		t.Fatal("repair reported no error although the index could not be written")
	}
}

// A legacy name with a stale line is cleared by the caller's own path, but it
// must not be reported as a repaired archive: several historical archives can
// share a name, and only a stable id identifies the one that half-committed.
func TestArchiveByLegacyNameDoesNotClaimAHistoricalArchive(t *testing.T) {
	dir := testenv.TempDir(t)
	s := Store{Dir: dir}
	if _, err := s.Save(Memory{Name: "alpha", Description: "gone", Type: TypeProject, Body: "body"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "alpha.md"), filepath.Join(dir, ".archive", "20260101-000000.000-alpha.md")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Archive("alpha")
	if err != nil {
		t.Fatalf("archive by legacy name: %v", err)
	}
	if got != "" {
		t.Fatalf("a legacy-name retry claimed the historical archive %q instead of reporting nothing", got)
	}
	assertIndexesDescribeTheirFiles(t, dir)
}
