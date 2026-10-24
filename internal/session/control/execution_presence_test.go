package control

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func ownedRootController(t *testing.T, manager *jobs.Manager, presence ExecutionPresence) (*Controller, *rootenv.Store, rootenv.Record) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-qm", "base"}} {
		cmd := gitcmd.Command(t.Context(), root, args...)
		cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := rootenv.New(t.TempDir(), root, repo, coord)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	env, err := store.Prepare(t.Context(), sessionstore.CanonicalSessionPath(path), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	if err := env.BindSession(t.Context(), path, auth); err != nil {
		t.Fatal(err)
	}
	r := env.Record()
	ag := agent.New(nil, nil, sessionstore.NewSession("BASE"), agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, SessionPath: path, WorkspaceRoot: r.Candidate.WorkspaceRoot, WorkspaceRepo: r.Candidate.Repo,
		RootEnvironment: env, ExecutionPresence: presence, Jobs: manager, Sink: event.Discard})
	t.Cleanup(c.CloseAfterDestroy)
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	return c, store, r
}

func TestRootOwnershipSurvivesCloseUntilActualExecutionExits(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreground", true: "background"}[background], func(t *testing.T) {
			entered, leave, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var leaveOnce sync.Once
			t.Cleanup(func() { leaveOnce.Do(func() { close(leave) }) })
			presence := func(context.Context, string, bool) (func(), error) { return func() { close(released) }, nil }
			var manager *jobs.Manager
			if background {
				manager = jobs.NewManager(event.Discard)
			}
			c, store, r := ownedRootController(t, manager, presence)
			done := make(chan error, 1)
			body := func(context.Context) error { close(entered); <-leave; return nil }
			if background {
				body = func(context.Context) error {
					manager.Start("fixture", "noncooperative", func(context.Context, io.Writer) (string, error) { close(entered); <-leave; return "done", nil })
					<-entered
					return nil
				}
			}
			go func() { done <- c.runSynchronousTurn(t.Context(), nil, body) }()
			<-entered
			if background {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			c.CloseAfterDestroy()
			if opened, err := store.Open(t.Context(), r.ID, r.SessionID); !errors.Is(err, filelock.ErrHeld) {
				if opened != nil {
					opened.Close()
				}
				t.Fatalf("cancel released ownership before actual completion: %v", err)
			}
			select {
			case <-released:
				t.Fatal("presence closed before physical completion")
			default:
			}
			leaveOnce.Do(func() { close(leave) })
			if !background {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-released:
			case <-t.Context().Done():
				t.Fatal(t.Context().Err())
			}
			opened, err := store.Open(t.Context(), r.ID, r.SessionID)
			if err != nil {
				t.Fatal("completed execution retained ownership:", err)
			}
			opened.Close()
		})
	}
}

func TestPlanningAdmissionRemainsAWriterAcrossInTurnExecutionApproval(t *testing.T) {
	var writer bool
	c := New(Options{WorkspaceRoot: t.TempDir(), Sink: event.Discard,
		ExecutionPresence: func(_ context.Context, _ string, canWrite bool) (func(), error) { writer = canWrite; return nil, nil }})
	defer c.Close()
	c.SetPlanMode(true)
	if err := c.runSynchronousTurn(t.Context(), nil, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !writer {
		t.Fatal("planning turn can approve execution without readmission; writer presence must cover it")
	}
}
