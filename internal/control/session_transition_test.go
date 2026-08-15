package control

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func transitionController(t *testing.T) (*Controller, *agent.Agent, *SessionLeaseKeeper, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	if err := sess.SaveIfAbsent(path); err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	ctrl := New(Options{Runner: exec, Executor: exec, SessionDir: dir, SessionPath: path, Sink: event.Discard})
	keeper := NewSessionLeaseKeeper()
	if err := keeper.Rebind(path); err != nil {
		t.Fatal(err)
	}
	if err := keeper.BindControllerAuthority(ctrl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(keeper.Release)
	return ctrl, exec, keeper, path
}

func assertTransitionOwner(t *testing.T, ctrl *Controller, exec *agent.Agent, keeper *SessionLeaseKeeper, originalPath string) {
	t.Helper()
	targetPath := ctrl.SessionPath()
	if targetPath == originalPath {
		t.Fatal("session path did not rotate")
	}
	if got := keeper.HeldPath(); got != agent.CanonicalSessionPath(targetPath) {
		t.Fatalf("keeper path = %q, want %q", got, agent.CanonicalSessionPath(targetPath))
	}
	if auth := exec.Session().WriteAuthority(); auth == nil || !auth.Covers(targetPath) {
		t.Fatal("rotated Session was published without target authority")
	}
}

func TestNewSessionTransfersWriterBeforePublish(t *testing.T) {
	ctrl, exec, keeper, originalPath := transitionController(t)
	if err := ctrl.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	assertTransitionOwner(t, ctrl, exec, keeper, originalPath)
	if _, err := os.Stat(originalPath); err != nil {
		t.Fatalf("NewSession removed parent transcript: %v", err)
	}
}

func TestClearSessionTransfersWriterBeforePublish(t *testing.T) {
	ctrl, exec, keeper, originalPath := transitionController(t)
	if err := ctrl.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	assertTransitionOwner(t, ctrl, exec, keeper, originalPath)
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Fatalf("ClearSession source stat = %v, want removed", err)
	}
}
