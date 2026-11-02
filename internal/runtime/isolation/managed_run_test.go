package isolation

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestManagedCandidateKeepsIdentityAcrossRestart(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	write(t, run.WorkspaceRoot(), "a.txt", "retained\n")
	if err := run.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(managed, root, opened(t, root))
	if !slices.Contains(restarted.Pending(), run.ID()) {
		t.Fatal("original result ID lost")
	}
	e, err := restarted.Apply(t.Context(), run.ID())
	if err != nil || e.ID != run.ID() {
		t.Fatalf("apply original ID: %v, %v", e, err)
	}
	if got := read(t, root, "a.txt"); got != "retained\n" {
		t.Fatal(got)
	}
	if len(restarted.Pending()) != 0 {
		t.Fatal("settled candidate remains pending")
	}
}

func TestManagedCandidateUnknownCannotSettleAfterRestart(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	write(t, run.WorkspaceRoot(), "a.txt", "unknown writer\n")
	if err := run.RetainUnknown(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(managed, root, opened(t, root))
	for _, invoke := range []func() error{
		func() error { _, err := restarted.Paths(run.ID()); return err },
		func() error { _, err := restarted.Apply(t.Context(), run.ID()); return err },
		func() error { _, err := restarted.Discard(t.Context(), run.ID()); return err },
	} {
		if err := invoke(); !errors.Is(err, ErrCandidateUnsettled) {
			t.Fatalf("unknown scope admitted: %v", err)
		}
	}
	restarted.SweepExpired(t.Context(), -time.Hour)
	if !slices.Contains(restarted.Pending(), run.ID()) {
		t.Fatal("unknown result forgotten")
	}
	if _, err := os.Stat(filepath.Join(run.WorkspaceRoot(), "a.txt")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "a.txt"); got != "one\n" {
		t.Fatal("unknown result applied", got)
	}
}

func TestManagedCandidateUnstartedCanBeDiscardedAfterRestart(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(managed, root, opened(t, root))
	if _, err := restarted.Discard(t.Context(), run.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(run.WorkspaceRoot()); !os.IsNotExist(err) {
		t.Fatalf("unstarted worktree remains: %v", err)
	}
	if _, err := restarted.Discard(t.Context(), run.ID()); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func TestManagedCandidateAppliedCleanupRetriesWithoutRepublishing(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := run.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	write(t, run.WorkspaceRoot(), "a.txt", "published\n")
	if err := run.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if out, err := s.repo.Command(t.Context(), args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("worktree", "lock", run.WorkspaceRoot())
	if _, err := s.Apply(t.Context(), run.ID()); !errors.Is(err, ErrCandidateCleanupPending) {
		t.Fatalf("cleanup failure hidden: %v", err)
	}
	if got := read(t, root, "a.txt"); got != "published\n" {
		t.Fatal(got)
	}
	if !slices.Contains(s.Pending(), run.ID()) {
		t.Fatal("failed cleanup forgot original ID")
	}
	git("worktree", "unlock", run.WorkspaceRoot())
	write(t, root, "a.txt", "later user edit\n")
	restarted := NewStore(managed, root, opened(t, root))
	if _, err := restarted.Apply(t.Context(), run.ID()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "a.txt"); got != "later user edit\n" {
		t.Fatal("cleanup republished old result", got)
	}
	if len(restarted.Pending()) != 0 {
		t.Fatal("cleanup did not settle")
	}
}

func TestManagedCandidatePartialRemovalRetainsRecoverableIdentity(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r, found, err := s.managedRecord(run.ID())
	if err != nil || !found {
		t.Fatal(err)
	}
	r.Phase, r.Disposition = candidateCleanup, "discarded"
	if err := s.saveManagedRecord(r, false); err != nil {
		t.Fatal(err)
	}
	// A removal that erased the worktree but not its admin entry is retryable;
	// the external journal does not depend on the deleted .git marker.
	if err := os.RemoveAll(run.WorkspaceRoot()); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(managed, root, opened(t, root))
	if !slices.Contains(restarted.Pending(), run.ID()) {
		t.Fatal("partial deletion lost ID")
	}
	if _, err := restarted.Discard(t.Context(), run.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(run.entry.Cand.Repo.GitDir); !os.IsNotExist(err) {
		t.Fatalf("admin remains: %v", err)
	}
}

func TestManagedCandidatePartialDirectoryRemovalCanRetry(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := s.managedRecord(run.ID())
	if err != nil {
		t.Fatal(err)
	}
	r.Phase, r.Disposition = candidateCleanup, "discarded"
	if err := s.saveManagedRecord(r, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(run.WorkspaceRoot(), ".git")); err != nil {
		t.Fatal(err)
	}
	peer := NewStore(managed, root, s.repo)
	if _, err := peer.Discard(t.Context(), run.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(run.WorkspaceRoot()); !os.IsNotExist(err) {
		t.Fatalf("partial directory remains: %v", err)
	}
}

func TestManagedCandidateCleanupRejectsReplacedNativeDirectory(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	run, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := s.managedRecord(run.ID())
	if err != nil {
		t.Fatal(err)
	}
	r.Phase, r.Disposition = candidateCleanup, "discarded"
	if err := s.saveManagedRecord(r, false); err != nil {
		t.Fatal(err)
	}
	path := run.WorkspaceRoot()
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "unrelated"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(managed, root, s.repo).Discard(t.Context(), run.ID()); err == nil {
		t.Fatal("replaced directory admitted")
	}
	if raw, err := os.ReadFile(filepath.Join(path, "unrelated")); err != nil || string(raw) != "keep" {
		t.Fatalf("replacement damaged %s %v", raw, err)
	}
}
