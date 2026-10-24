package rootenv

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/state/sessionstore"
)

func environmentRefs(t *testing.T, source, id string) []string {
	t.Helper()
	var refs []string
	for ref := range strings.FieldsSeq(git(t, source, "for-each-ref", "--format=%(refname)", "refs/reasonix/root-environments/")) {
		name := strings.TrimPrefix(ref, "refs/reasonix/root-environments/")
		if name == id || strings.HasPrefix(name, id+"-") {
			refs = append(refs, ref)
		}
	}
	return refs
}

func registeredWorktree(t *testing.T, source, root string) bool {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err == nil {
		root = filepath.Join(resolved, filepath.Base(root))
	}
	for line := range strings.Lines(git(t, source, "worktree", "list", "--porcelain")) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok && filepath.Clean(path) == root {
			return true
		}
	}
	return false
}

func assertReclaimed(t *testing.T, s *Store, source string, r Record) {
	t.Helper()
	if _, err := os.Stat(s.environmentDir(r.ID)); !os.IsNotExist(err) {
		t.Fatalf("%s %s: environment directory retained: %v", r.ID, r.State, err)
	}
	if refs := environmentRefs(t, source, r.ID); len(refs) != 0 {
		t.Fatalf("%s %s: pinned refs retained: %v", r.ID, r.State, refs)
	}
	if r.Candidate.WorktreeRoot != "" && registeredWorktree(t, source, r.Candidate.WorktreeRoot) {
		t.Fatalf("%s %s: worktree still registered", r.ID, r.State)
	}
}

func TestReclaimRemovesEnvironmentsThatNeverBecameUsable(t *testing.T) {
	s, source := fixture(t)
	write(t, filepath.Join(source, "dirty"), "source input\n")
	s.preparationProbe = func(r Record) error {
		if r.State == "materializing" {
			return errors.New("interrupted before checkout")
		}
		return nil
	}
	planned, err := s.Prepare(t.Context(), "planned", nil, nil)
	if err == nil {
		t.Fatal("probe did not interrupt preparation")
	}
	s.preparationProbe = nil
	late, err := s.Prepare(t.Context(), "late", []string{"../outside"}, nil)
	if err == nil {
		t.Fatal("unsafe environment copy was prepared")
	}
	unbound, err := s.Prepare(t.Context(), "unbound", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	unbound.Close()
	abandoned := []Record{planned.Record(), late.Record(), unbound.Record()}
	if abandoned[1].State != "recovery_required" || !registeredWorktree(t, source, abandoned[1].Candidate.WorktreeRoot) {
		t.Fatalf("late failure did not leave a checkout: %+v", abandoned[1])
	}
	for _, r := range abandoned {
		if len(environmentRefs(t, source, r.ID)) == 0 {
			t.Fatalf("%s left no pinned ref to reclaim", r.State)
		}
	}
	retained, err := s.ReclaimAbandoned(t.Context())
	if err != nil || len(retained) != 0 {
		t.Fatalf("reclaim = %+v, %v", retained, err)
	}
	for _, r := range abandoned {
		assertReclaimed(t, s, source, r)
	}
	if raw, err := os.ReadFile(filepath.Join(source, "dirty")); err != nil || string(raw) != "source input\n" {
		t.Fatalf("source changed: %q %v", raw, err)
	}
}

func TestReclaimRetainsWhatAnOwnerOrUnintegratedWorkStillHolds(t *testing.T) {
	s, source := fixture(t)
	live, err := s.Prepare(t.Context(), "live", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "bound.jsonl"))
	bound, err := s.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.BindSession(t.Context(), path, attachmentAuthority(t, path)); err != nil {
		t.Fatal(err)
	}
	bound.Close()
	if err := os.Remove(sessionstore.ExecutionEnvironmentPath(path)); err != nil {
		t.Fatal(err)
	}
	dirty, err := s.Prepare(t.Context(), "dirty", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dirty.Record().Candidate.WorkspaceRoot, "work"), "not integrated\n")
	dirty.Close()
	retained, err := s.ReclaimAbandoned(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(retained) != 1 || retained[0].ID != dirty.Record().ID || !errors.Is(retained[0].Err, ErrUnintegrated) {
		t.Fatalf("retained = %+v", retained)
	}
	for _, r := range []Record{live.Record(), bound.Record(), dirty.Record()} {
		if _, err := os.Stat(r.Candidate.WorktreeRoot); err != nil {
			t.Fatalf("%s removed: %v", r.SessionID, err)
		}
		if !slices.Contains(environmentRefs(t, source, r.ID), "refs/reasonix/root-environments/"+r.ID) {
			t.Fatalf("%s lost its baseline anchor", r.SessionID)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(dirty.Record().Candidate.WorkspaceRoot, "work")); err != nil || string(raw) != "not integrated\n" {
		t.Fatalf("unintegrated work = %q %v", raw, err)
	}
	reopened, err := s.Open(t.Context(), bound.Record().ID, path)
	if err != nil {
		t.Fatal("bound environment no longer opens:", err)
	}
	reopened.Close()
}
