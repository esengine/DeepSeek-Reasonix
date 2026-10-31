package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestManagedCandidateCannotBypassFenceThroughLegacyOwners(t *testing.T) {
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
	if _, err := s.Finish(t.Context(), run.entry); err == nil {
		t.Fatal("legacy Finish crossed fence")
	}
	if err := s.Abort(t.Context(), run.entry); err == nil {
		t.Fatal("legacy Abort crossed fence")
	}
	s.SweepExpired(t.Context(), -time.Hour)
	if _, err := os.Stat(filepath.Join(run.WorkspaceRoot(), "a.txt")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "a.txt"); got != "one\n" {
		t.Fatal(got)
	}
}

func TestManagedCandidateSweepExpiresReadyButPreservesUnknown(t *testing.T) {
	root, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, root, opened(t, root))
	p, err := s.Prepare(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ready, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := p.BeginManaged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []*ManagedRun{ready, unknown} {
		if err := run.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		write(t, run.WorkspaceRoot(), "a.txt", "retained\n")
	}
	if err := ready.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := unknown.RetainUnknown(t.Context()); err != nil {
		t.Fatal(err)
	}
	peer := NewStore(managed, root, opened(t, root))
	peer.SweepExpired(t.Context(), -time.Hour)
	if ids := peer.Pending(); !slices.Equal(ids, []string{unknown.ID()}) {
		t.Fatal(ids)
	}
	if _, err := os.Stat(ready.WorkspaceRoot()); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(unknown.WorkspaceRoot()); err != nil {
		t.Fatal(err)
	}
}

func TestManagedCandidateApplyingCannotBeDiscardedOrExpired(t *testing.T) {
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
	write(t, run.WorkspaceRoot(), "a.txt", "target\n")
	write(t, run.WorkspaceRoot(), "b.txt", "target b\n")
	if err := run.Complete(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, _, err := s.managedRecord(run.ID())
	if err != nil {
		t.Fatal(err)
	}
	r.Phase = candidateApplying
	if err := s.saveManagedRecord(r, false); err != nil {
		t.Fatal(err)
	}
	// Simulate interruption after the first immutable target path was written.
	write(t, root, "a.txt", "target\n")
	peer := NewStore(managed, root, opened(t, root))
	if _, err := peer.Discard(t.Context(), run.ID()); !errors.Is(err, ErrCandidateUnsettled) {
		t.Fatal(err)
	}
	peer.SweepExpired(context.Background(), -time.Hour)
	if !slices.Contains(peer.Pending(), run.ID()) {
		t.Fatal("sweep forgot partial publication")
	}
	if _, err := peer.Apply(t.Context(), run.ID()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "b.txt"); got != "target b\n" {
		t.Fatal(got)
	}
}

func TestManagedCandidateEmptyResultStillFollowsExecutionState(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "unknown"}[unknown], func(t *testing.T) {
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
			if unknown {
				err = run.RetainUnknown(t.Context())
			} else {
				err = run.Complete(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			peer := NewStore(managed, root, s.repo)
			_, err = peer.Apply(t.Context(), " "+run.ID()+" ")
			if unknown {
				if !errors.Is(err, ErrCandidateUnsettled) || !slices.Contains(peer.Pending(), run.ID()) {
					t.Fatalf("empty unknown result was forgotten: %v", err)
				}
			} else if err != nil || len(peer.Pending()) != 0 {
				t.Fatalf("empty completed result failed cleanup: %v", err)
			}
		})
	}
}
