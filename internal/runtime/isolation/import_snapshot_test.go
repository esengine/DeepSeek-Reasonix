package isolation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/platform/worktree"
	"reasonix/internal/state/mutation"
)

func TestImportedSnapshotIsIndependentAndKeepsOriginalComparisonBase(t *testing.T) {
	source := gitRepo(t)
	baseline, err := worktree.TakeSnapshot(t.Context(), opened(t, source))
	if err != nil {
		t.Fatal(err)
	}
	live, err := worktree.CreateCandidate(t.Context(), baseline, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer worktree.RemoveCandidate(t.Context(), baseline, live)
	write(t, live.WorkspaceRoot, "a.txt", "root result\n")
	frozen, err := worktree.TakeSnapshot(t.Context(), live.Repo)
	if err != nil {
		t.Fatal(err)
	}
	write(t, source, "other.txt", "independent source work\n")
	coord, err := mutation.New(source, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), source, opened(t, source)).WithMutationCoordinator(coord)
	request := SnapshotImport{ID: "iso_1234abcd", Owner: "root-session", Baseline: baseline,
		Origin: SnapshotOrigin{Version: 1, PublicationID: "publication", EnvironmentID: "environment", EnvironmentGeneration: "generation", Materialization: frozen}}
	e, err := s.ImportSnapshotResult(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if e.Snap != baseline || e.Cand.WorktreeRoot == live.WorktreeRoot || e.Tree != frozen.Tree || e.State != Pending {
		t.Fatalf("import lost snapshot boundaries: %+v", e)
	}
	write(t, live.WorkspaceRoot, "a.txt", "root continued\n")
	if got := read(t, e.Cand.WorkspaceRoot, "a.txt"); got != "root result\n" {
		t.Fatal(got)
	}
	again, err := s.ImportSnapshotResult(t.Context(), request)
	if err != nil || again.Cand != e.Cand || again.RunGeneration != e.RunGeneration {
		t.Fatalf("retry duplicated result: %+v %v", again, err)
	}
	if _, err := s.Resume(t.Context(), e.ID, request.Owner); err == nil {
		t.Fatal("immutable root snapshot resumed as a mutable task")
	}
	raw, err := os.ReadFile(entryRecordPath(e))
	if err != nil {
		t.Fatal(err)
	}
	var record durableEntry
	if json.Unmarshal(raw, &record) != nil || record.Version != 3 {
		t.Fatal("old readers can accept imported protocol")
	}
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if got := read(t, source, "other.txt"); got != "independent source work\n" {
		t.Fatal(got)
	}
	if got := read(t, live.WorkspaceRoot, "a.txt"); got != "root continued\n" {
		t.Fatal("integration removed or overwrote live environment", got)
	}
	if _, err := os.Stat(filepath.Join(live.WorkspaceRoot, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ImportedResultIntegrated(t.Context(), request); err != nil || !ok {
		t.Fatalf("missing terminal publication receipt: %v %v", ok, err)
	}
}
