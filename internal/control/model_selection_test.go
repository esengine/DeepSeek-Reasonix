package control

import (
	"errors"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestControllerModelSelectionPersistsAndBranches(t *testing.T) {
	dir := schemaOneTempDir(t)
	path := agent.NewSessionPath(dir, "original")
	s := agent.NewSession("fixture system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "keep this"})
	exec := agent.New(nil, nil, s, agent.Options{}, event.Discard)
	ctrl := New(Options{Executor: exec, Sink: event.Discard, SessionDir: dir, ModelRef: "go/model", ModelIdentity: "accepted"})
	defer ctrl.Close()
	ctrl.SetFreshSessionPath(path)
	if err := ctrl.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if model, identity, ok := agent.LoadSessionModelSelection(path); !ok || model != "go/model" || identity != "accepted" {
		t.Fatal("snapshot did not persist the assembled model identity")
	}
	branch, err := ctrl.Branch("copy")
	if err != nil {
		t.Fatal(err)
	}
	if model, identity, ok := agent.LoadSessionModelSelection(branch); !ok || model != "go/model" || identity != "accepted" {
		t.Fatal("branch did not inherit the assembled model identity")
	}
}

func TestControllerRejectsHistoricalBranchBeforeTransition(t *testing.T) {
	dir := schemaOneTempDir(t)
	path := agent.NewSessionPath(dir, "other")
	s := agent.NewSession("fixture")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "historical branch"})
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.SetBranchModelSelectionPreserveUpdated(path, "go/model", "old"); err != nil {
		t.Fatal(err)
	}
	blocked := errors.New("MIGRATED_MODEL_UNAVAILABLE")
	ctrl := New(Options{SessionDir: dir, ResolveSessionModel: func(model, identity string) (string, error) {
		if model != "go/model" || identity != "old" {
			t.Fatal("branch did not pass the saved selection to the validator")
		}
		return "", blocked
	}})
	defer ctrl.Close()
	if _, err := ctrl.SwitchBranch(agent.BranchID(path)); !errors.Is(err, blocked) {
		t.Fatalf("branch was not rejected: %v", err)
	}
	if ctrl.SessionPath() != "" {
		t.Fatal("failed validation changed the active session")
	}
}
