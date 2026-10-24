package isolation

import (
	"errors"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestResumeRetainsBaselineAndExcludesSettlement(t *testing.T) {
	repo, managed := gitRepo(t), t.TempDir()
	s := NewStore(managed, repo, opened(t, repo))
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	write(t, e.Cand.WorkspaceRoot, "a.txt", "first run\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(managed, repo, opened(t, repo))
	resumed, err := restarted.Resume(t.Context(), e.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restarted.Abort(t.Context(), resumed) })
	if resumed.Snap != e.Snap || resumed.Cand != e.Cand || resumed.State != Running {
		t.Fatalf("resume changed environment or baseline: %+v", resumed)
	}
	for _, op := range []func() error{
		func() error { _, err := s.Resume(t.Context(), e.ID, "owner"); return err },
		func() error { _, err := s.Apply(t.Context(), e.ID); return err },
		func() error { _, err := s.Discard(t.Context(), e.ID); return err },
	} {
		var refusal tool.Refusal
		if err := op(); !errors.As(err, &refusal) || refusal.Code != CodeBusy {
			t.Fatalf("live resumed environment not excluded: %v", err)
		}
	}
	write(t, resumed.Cand.WorkspaceRoot, "a.txt", "second run\n")
	if _, err := restarted.Finish(t.Context(), resumed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(t.Context(), e.ID); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "second run\n" {
		t.Fatal(got)
	}
}

func TestUnchangedOwnedEnvironmentCanResumeOnlyForOwner(t *testing.T) {
	repo := gitRepo(t)
	s := NewStore(t.TempDir(), repo, opened(t, repo))
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), e) })
	if kept, err := s.Finish(t.Context(), e); err != nil || !kept {
		t.Fatalf("unchanged environment removed: %v %v", kept, err)
	}
	if _, err := s.Resume(t.Context(), e.ID, "stranger"); err == nil {
		t.Fatal("another session resumed environment")
	}
	resumed, err := s.Resume(t.Context(), e.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Abort(t.Context(), resumed) })
	if resumed.Cand != e.Cand {
		t.Fatal("resume created a new environment")
	}
}
