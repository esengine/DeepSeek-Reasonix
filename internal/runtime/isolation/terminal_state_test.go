package isolation

import (
	"context"
	"errors"
	"testing"
)

func TestFailedAndCancelledCandidatesKeepTerminalStateAndCanResume(t *testing.T) {
	for _, tc := range []struct {
		state State
		err   error
	}{{Failed, errors.New("runner failed")}, {Cancelled, context.Canceled}, {Cancelled, context.DeadlineExceeded}} {
		t.Run(string(tc.state), func(t *testing.T) {
			repo, managed := gitRepo(t), t.TempDir()
			s := NewStore(managed, repo, opened(t, repo))
			e, err := s.BeginOwned(t.Context(), repo, "owner")
			if err != nil {
				t.Fatal(err)
			}
			write(t, e.Cand.WorkspaceRoot, "a.txt", "retained\n")
			if _, err := s.Complete(t.Context(), e, Outcome{}, tc.err); !errors.Is(err, tc.err) {
				t.Fatalf("runner result: %v", err)
			}
			restored := NewStore(managed, repo, opened(t, repo))
			result, err := restored.lookup(e.ID)
			if err != nil || result.State != tc.state {
				t.Fatalf("terminal state lost: %+v %v", result, err)
			}
			continued, err := restored.Resume(t.Context(), e.ID, "owner")
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Abort(t.Context(), continued)
			if continued.RunGeneration == e.RunGeneration || read(t, continued.Cand.WorkspaceRoot, "a.txt") != "retained\n" {
				t.Fatal("resume lost generation or result")
			}
			if _, err := restored.Complete(t.Context(), continued, Outcome{}, nil); err != nil {
				t.Fatal(err)
			}
			if continued.State != Pending {
				t.Fatalf("successful continuation: %s", continued.State)
			}
		})
	}
}

func TestConflictingApplyPersistsConflictState(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Abort(t.Context(), e)
	write(t, e.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "a.txt", "concurrent\n")
	if _, err := s.Apply(t.Context(), e.ID); err == nil {
		t.Fatal("conflicting result applied")
	}
	result, err := s.lookup(e.ID)
	if err != nil || result.State != Conflict {
		t.Fatalf("conflict state lost: %+v %v", result, err)
	}
	if read(t, repo, "a.txt") != "concurrent\n" {
		t.Fatal("conflict overwrote source")
	}
	write(t, repo, "a.txt", "one\n")
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
}
