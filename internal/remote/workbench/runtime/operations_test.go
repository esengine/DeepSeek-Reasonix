package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

type rewindCommitController struct {
	*persistentFakeController
	turn  int
	scope control.RewindScope
}

func (c *rewindCommitController) Rewind(turn int, scope control.RewindScope) error {
	c.turn, c.scope = turn, scope
	return nil
}

func TestRewindRegistryFailureReturnsCommittedResult(t *testing.T) {
	sessionDir := t.TempDir()
	log := &strings.Builder{}
	srv := New(Options{
		Workspace: t.TempDir(), SessionDir: sessionDir,
		RegistryPath: t.TempDir(), // AtomicWriteFile cannot replace this directory.
		Logger:       log,
	})
	ctrl := &rewindCommitController{persistentFakeController: &persistentFakeController{
		fakeController: &fakeController{model: "local/test"}, sessionDir: sessionDir,
		sessionPath: filepath.Join(sessionDir, "session.jsonl"),
	}}
	target := srv.installTestSession(ctrl)
	srv.registryRead = true
	result, err := srv.rewindSession(protocol.SessionRewindParams{
		SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		CheckpointID: "checkpoint_2", Scope: protocol.RewindBoth,
	})
	if err != nil {
		t.Fatalf("rewind returned an error after controller commit: %v", err)
	}
	if ctrl.turn != 2 || ctrl.scope != control.RewindBoth || !result.WorkspaceChanged || !result.ConversationRewritten || !result.SnapshotRequired {
		t.Fatalf("committed rewind = result %+v, turn %d, scope %d", result, ctrl.turn, ctrl.scope)
	}
	if !strings.Contains(log.String(), "persist committed rewind") {
		t.Fatalf("registry failure was not logged: %q", log.String())
	}
}

type forkArtifactController struct {
	*persistentFakeController
	forkPath string
}

func (c *forkArtifactController) ForkSession(_ int, _ string) (string, error) {
	sess := agent.NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "private transcript"})
	if err := sess.Save(c.forkPath); err != nil {
		return "", err
	}
	if err := agent.SaveBranchMeta(c.forkPath, agent.BranchMeta{Name: "fork"}); err != nil {
		return "", err
	}
	return c.forkPath, nil
}

func TestForkRegistryFailureRemovesCreatedArtifacts(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	forkPath := filepath.Join(sessionDir, "fork.jsonl")
	srv := New(Options{
		Workspace: workspace, SessionDir: sessionDir,
		RegistryPath: t.TempDir(), // AtomicWriteFile cannot replace this directory.
		BuildController: func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
			return &persistentFakeController{fakeController: &fakeController{model: model}, sessionDir: sessionDir}, nil
		},
	})
	ctrl := &forkArtifactController{
		persistentFakeController: &persistentFakeController{
			fakeController: &fakeController{model: "local/test"}, sessionDir: sessionDir,
			sessionPath: filepath.Join(sessionDir, "source.jsonl"),
		},
		forkPath: forkPath,
	}
	target := srv.installTestSession(ctrl)
	srv.registryRead = true
	_, err := srv.forkSession(context.Background(), protocol.SessionForkParams{
		SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		CheckpointID: "checkpoint_0",
	})
	if err == nil {
		t.Fatal("fork unexpectedly succeeded despite registry failure")
	}
	for _, path := range []string{forkPath, agent.BranchMetaPath(forkPath)} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("failed fork left artifact %q on disk: %v", filepath.Base(path), statErr)
		}
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.sessions) != 1 || srv.sessions[target.SessionID] == nil {
		t.Fatalf("failed fork changed live sessions: %+v", srv.sessions)
	}
}
