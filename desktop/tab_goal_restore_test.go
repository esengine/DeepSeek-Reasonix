package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// TestRestoredTabGoalFollowsSessionGoalState pins the restart half of the
// goal-reset contract: a typed /new or /clear rotates the session through the
// controller (never touching App.NewSession/ClearSession), so the goal saved
// in desktop-tabs.json can be stale. Startup restore must validate it against
// the session's goal-state sidecar — which the rotation wrote as stopped on
// the fresh path — instead of re-seeding the cleared goal via SetGoal.
func TestRestoredTabGoalFollowsSessionGoalState(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "session.jsonl")

	exec := agent.New(stubProvider{}, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: oldPath, Label: "test", Sink: event.Discard})
	ctrl.SetGoal("finish the migration")

	// Typed /new: controller-side rotation only.
	if err := ctrl.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	newPath := ctrl.SessionPath()
	ctrl.Close()

	// desktop-tabs.json still carries the pre-rotation goal with the rotated
	// path (the tab profile was never told about the /new). Restore must not
	// resurrect it: the rotated session's sidecar says stopped.
	if got := runningTabSessionGoal(newPath, "finish the migration"); got != "" {
		t.Fatalf("restored goal for rotated session = %q, want empty", got)
	}

	// The OLD session's sidecar still says running: restoring a tab that
	// points at the old session keeps its goal (resume semantics).
	if got := runningTabSessionGoal(oldPath, "finish the migration"); got != "finish the migration" {
		t.Fatalf("restored goal for old session = %q, want preserved", got)
	}

	// A session without a goal-state sidecar keeps the persisted goal
	// (legacy sessions predating the sidecar).
	bare := filepath.Join(dir, "bare.jsonl")
	if err := os.WriteFile(bare, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write bare session: %v", err)
	}
	if got := runningTabSessionGoal(bare, "legacy goal"); got != "legacy goal" {
		t.Fatalf("restored goal without sidecar = %q, want fallback preserved", got)
	}
}
