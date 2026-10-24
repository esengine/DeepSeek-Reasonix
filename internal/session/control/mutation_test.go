package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/runtime/commitmsg"
	"reasonix/internal/state/mutation"
)

func coordinatedController(t *testing.T, source *Controller, notices chan<- mutation.Notice) *Controller {
	t.Helper()
	c, err := mutation.New(source.WorkspaceRoot(), t.TempDir(), func(n mutation.Notice) {
		if n.State == "waiting" {
			notices <- n
		}
	}, mutation.WithWorkspaceResources(source.WorkspaceRepo().GitDir, source.WorkspaceRepo().CommonDir))
	if err != nil {
		t.Fatal(err)
	}
	ctrl := New(Options{WorkspaceRoot: source.WorkspaceRoot(), WorkspaceRepo: source.WorkspaceRepo(),
		SessionDir: t.TempDir(), Sink: event.Discard, MutationCoordinator: c})
	t.Cleanup(ctrl.Close)
	return ctrl
}

func TestControllerGitMutationsWaitAndCancelBeforeWriting(t *testing.T) {
	for _, operation := range []string{"stage", "discard", "commit"} {
		t.Run(operation, func(t *testing.T) {
			base, root := branchFixture(t)
			notices := make(chan mutation.Notice, 8)
			c := coordinatedController(t, base, notices)
			path := filepath.Join(root, "a.txt")
			if err := os.WriteFile(path, []byte("dirty\n"), 0600); err != nil {
				t.Fatal(err)
			}
			held, err := c.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths,
				Paths: []string{path, c.WorkspaceRepo().CommonDir}})
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(mutation.NotExecuted)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var err error
				switch operation {
				case "stage":
					err = c.StageChanges(ctx, StageRequest{Paths: []string{"a.txt"}})
				case "discard":
					err = c.DiscardChange(ctx, DiscardRequest{Path: "a.txt"})
				case "commit":
					_, err = c.CommitStaged(ctx, CommitRequest{Message: "fixture", Fingerprint: "stale"})
				}
				done <- err
			}()
			select {
			case n := <-notices:
				if n.CallID != "git."+operation || n.ExecutionID == "" {
					t.Fatalf("missing host operation identity: %+v", n)
				}
			case err := <-done:
				t.Fatalf("mutation bypassed coordination: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("missing wait notice")
			}
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled mutation: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "dirty\n" {
				t.Fatalf("cancelled operation changed file: %q %v", data, err)
			}
			staged, err := c.WorkspaceRepo().Command(t.Context(), "diff", "--cached", "--name-only").Output()
			if err != nil || len(staged) != 0 {
				t.Fatalf("cancelled operation changed index: %q %v", staged, err)
			}
		})
	}
}

type mutationDraftFunc func(context.Context, commitmsg.Input) (string, error)

func (f mutationDraftFunc) Generate(ctx context.Context, in commitmsg.Input) (string, error) {
	return f(ctx, in)
}

func TestCommitProposalReleasesMetadataBeforeModelCall(t *testing.T) {
	base, root := branchFixture(t)
	c := coordinatedController(t, base, make(chan mutation.Notice, 8))
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.StageChanges(t.Context(), StageRequest{Paths: []string{"a.txt"}}); err != nil {
		t.Fatal(err)
	}
	c.committer = mutationDraftFunc(func(ctx context.Context, _ commitmsg.Input) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		lease, err := c.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.GitMetadata, Paths: []string{c.WorkspaceRepo().CommonDir}})
		if err != nil {
			return "", err
		}
		lease.End(mutation.NotExecuted)
		return "proposed subject", nil
	})
	proposal, err := c.ProposeCommit(t.Context(), "")
	if err != nil || proposal.Message != "proposed subject" || proposal.Fingerprint == "" {
		t.Fatalf("model held metadata scope: %+v %v", proposal, err)
	}
	if _, err := gitcommit.Staged(t.Context(), c.WorkspaceRepo()); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedDiscardRefusesChangedFileIndexOrHead(t *testing.T) {
	for _, changed := range []string{"file", "index", "head"} {
		t.Run(changed, func(t *testing.T) {
			base, root := branchFixture(t)
			notices := make(chan mutation.Notice, 8)
			c := coordinatedController(t, base, notices)
			path := filepath.Join(root, "a.txt")
			if err := os.WriteFile(path, []byte("first edit\n"), 0600); err != nil {
				t.Fatal(err)
			}
			held, err := c.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{path, c.WorkspaceRepo().CommonDir}})
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(mutation.NotExecuted)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.DiscardChange(ctx, DiscardRequest{Path: "a.txt"}) }()
			select {
			case <-notices:
			case <-ctx.Done():
				t.Fatal("discard did not queue")
			}
			switch changed {
			case "file":
				err = os.WriteFile(path, []byte("new edit\n"), 0600)
			case "index":
				err = gitcommit.Stage(t.Context(), c.WorkspaceRepo(), []string{"a.txt"}, false)
			case "head":
				err = c.WorkspaceRepo().Command(t.Context(), "update-ref", "HEAD", "refs/heads/next").Run()
			}
			if err != nil {
				t.Fatal(err)
			}
			held.End(mutation.Unknown)
			if err := <-done; !errors.Is(err, mutation.ErrVersionConflict) {
				t.Fatalf("queued discard accepted a changed %s: %v", changed, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || (string(data) != "first edit\n" && string(data) != "new edit\n") {
				t.Fatalf("conflict restored over changed state: %q %v", data, err)
			}
		})
	}
}
