package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
)

func TestBoundEnvironmentRefusesWrongRuntimeBeforeResumeAndAdmission(t *testing.T) {
	root, isolated := t.TempDir(), t.TempDir()
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	auth, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	binding := sessionstore.ExecutionEnvironmentBinding{Version: 1, EnvironmentID: "renv_test", Generation: "generation", SourceWorkspace: root, WorkspaceRoot: isolated, WorkingCopyRoot: isolated}
	if err := sessionstore.BindExecutionEnvironment(path, binding, auth); err != nil {
		t.Fatal(err)
	}
	session := sessionstore.NewSession("initial")
	ag := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	c := New(Options{Executor: ag, WorkspaceRoot: root, Sink: event.Discard})
	defer c.ReleaseResources()
	if err := c.Resume(sessionstore.NewSession("different"), path); !errors.Is(err, sessionstore.ErrExecutionEnvironmentMismatch) {
		t.Fatalf("wrong runtime resumed: %v", err)
	}
	if c.SessionPath() != "" || ag.Session() != session {
		t.Fatal("refused resume changed live session")
	}
	missing := New(Options{Executor: agent.New(nil, nil, sessionstore.NewSession("initial"), agent.Options{}, event.Discard), WorkspaceRoot: isolated, Sink: event.Discard})
	defer missing.ReleaseResources()
	if err := missing.Resume(sessionstore.NewSession("bound"), path); !errors.Is(err, sessionstore.ErrExecutionEnvironmentMismatch) {
		t.Fatalf("matching cwd bypassed environment ownership: %v", err)
	}
	c.SetSessionPath(path)
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if err := c.ensureWriteAuthorityReady(); !errors.Is(err, sessionstore.ErrExecutionEnvironmentMismatch) {
		t.Fatalf("wrong environment became writable: %v", err)
	}
	called := false
	if err := c.runSynchronousTurn(t.Context(), nil, func(context.Context) error { called = true; return nil }); !errors.Is(err, sessionstore.ErrExecutionEnvironmentMismatch) {
		t.Fatalf("sync admission: %v", err)
	}
	if called {
		t.Fatal("wrong environment executed turn")
	}
	if got := c.runGuarded(func(context.Context) error { t.Error("wrong async environment ran"); return nil }); !got.Refused() {
		t.Fatalf("async admitted: %v", got)
	}
}
