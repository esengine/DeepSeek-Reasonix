package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestExternalEditBridgeUsesRequestedTab(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	file := filepath.Join(workspace, "b.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	execA := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	execB := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrlA := control.New(control.Options{Executor: execA, SessionDir: sessionDir, SessionPath: filepath.Join(sessionDir, "a.jsonl"), WorkspaceRoot: workspace})
	ctrlB := control.New(control.Options{Executor: execB, SessionDir: sessionDir, SessionPath: filepath.Join(sessionDir, "b.jsonl"), WorkspaceRoot: workspace})
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"a": {ID: "a", Scope: "project", WorkspaceRoot: workspace, Ctrl: ctrlA, Ready: true},
			"b": {ID: "b", Scope: "project", WorkspaceRoot: workspace, Ctrl: ctrlB, Ready: true},
		},
		activeTabID: "a",
	}

	err := app.runExternalEditForTab("b", "apply_patch", []string{"b.txt"}, func(context.Context) error {
		return os.WriteFile(file, []byte("after\n"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := len(ctrlA.History()); got != 1 {
		t.Fatalf("active tab history changed: got %d messages, want only system", got)
	}
	if got := len(ctrlB.History()); got < 3 {
		t.Fatalf("requested tab history messages = %d, want synthetic tool pair", got)
	}
	cps := ctrlB.Checkpoints()
	if len(cps) != 1 || len(cps[0].Paths) != 1 || cps[0].Paths[0] != "b.txt" {
		t.Fatalf("requested tab checkpoints = %+v, want b.txt", cps)
	}
}

func TestExternalEditBridgeExportedHandleAPI(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	file := filepath.Join(workspace, "a.txt")
	if err := os.WriteFile(file, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SessionDir: sessionDir, SessionPath: filepath.Join(sessionDir, "a.jsonl"), WorkspaceRoot: workspace})
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"a": {ID: "a", Scope: "project", WorkspaceRoot: workspace, Ctrl: ctrl, Ready: true},
		},
		activeTabID: "a",
	}

	id, err := app.BeginExternalEditForTab("a", "apply_patch", []string{"a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.EndExternalEdit(id, ""); err != nil {
		t.Fatal(err)
	}
	if got := len(ctrl.History()); got < 3 {
		t.Fatalf("history messages = %d, want synthetic tool pair", got)
	}
	changes := app.WorkspaceChanges("a")
	if len(changes.Files) != 1 || changes.Files[0].Path != "a.txt" || !containsString(changes.Files[0].Sources, "session") {
		t.Fatalf("workspace changes = %+v, want a.txt session source", changes.Files)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
