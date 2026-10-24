package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func TestFinishDoesNotHoldEntryWhileWaitingForToolResources(t *testing.T) {
	repo := gitRepo(t)
	waiting := make(chan struct{}, 1)
	c, err := mutation.New(repo, t.TempDir(), func(n mutation.Notice) {
		if n.CallID == "isolation.finish" && n.State == "waiting" {
			waiting <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), repo, opened(t, repo)).WithMutationCoordinator(c)
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	impact, err := NewApplyTool(s).DescribeMutation(t.Context(), args(e.ID))
	if err != nil {
		t.Fatal(err)
	}
	e, err = s.Resume(t.Context(), e.ID, "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Abort(t.Context(), e)
	l, err := c.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: append(impact.Paths, impact.SharedResources...), Recovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(mutation.NotExecuted)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := s.Finish(ctx, e); finished <- err }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("Finish did not wait on tool resources")
	}
	_, err = s.Apply(l.Context(ctx), e.ID)
	var refused tool.Refusal
	if !errors.As(err, &refused) || refused.Code != CodeBusy {
		t.Fatalf("tool could not inspect live run while Finish waited: %v", err)
	}
	l.End(mutation.NotExecuted)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestDirectApplyDoesNotHoldEntryWhileWaitingForToolResources(t *testing.T) {
	repo := gitRepo(t)
	waiting := make(chan struct{}, 1)
	c, err := mutation.New(repo, t.TempDir(), func(n mutation.Notice) {
		if n.CallID == "isolation.apply" && n.State == "waiting" {
			waiting <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(t.TempDir(), repo, opened(t, repo)).WithMutationCoordinator(c)
	e, err := s.BeginOwned(t.Context(), repo, "owner")
	if err != nil {
		t.Fatal(err)
	}
	write(t, e.Cand.WorkspaceRoot, "a.txt", "candidate\n")
	if _, err := s.Finish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	impact, err := NewApplyTool(s).DescribeMutation(t.Context(), args(e.ID))
	if err != nil {
		t.Fatal(err)
	}
	l, err := c.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: append(impact.Paths, impact.SharedResources...), Recovery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(mutation.NotExecuted)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Apply(ctx, e.ID); done <- err }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("direct apply did not queue")
	}
	if _, err := s.Apply(l.Context(ctx), e.ID); err != nil {
		t.Fatal(err)
	}
	l.End(mutation.Committed)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("idempotent direct operation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("direct operation retained entry lock")
	}
	if got := read(t, repo, "a.txt"); got != "candidate\n" {
		t.Fatal(got)
	}
}
