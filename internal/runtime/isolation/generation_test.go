package isolation

import (
	"errors"
	"os"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestPreviousRunCannotFinishOrAbortResumedEnvironment(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	old, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), old) })
	write(t, old.Cand.WorkspaceRoot, "a.txt", "first\n")
	if _, err := s.Finish(t.Context(), old); err != nil {
		t.Fatal(err)
	}
	current, err := s.Resume(t.Context(), old.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), current) })
	if _, err := s.Finish(t.Context(), old); err == nil {
		t.Fatal("previous run finalized the current run")
	}
	s.Abort(t.Context(), old)
	if _, err := os.Stat(current.Cand.WorkspaceRoot); err != nil {
		t.Fatalf("previous abort removed current worktree: %v", err)
	}
	var refusal tool.Refusal
	if _, err := s.Discard(t.Context(), current.ID); !errors.As(err, &refusal) || refusal.Code != CodeBusy {
		t.Fatalf("previous run released current liveness: %v", err)
	}
	write(t, current.Cand.WorkspaceRoot, "a.txt", "second\n")
	if _, err := s.Finish(t.Context(), current); err != nil {
		t.Fatal(err)
	}
}
