package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// divergedSessionController builds a controller whose in-memory transcript has
// diverged from what path holds on disk, so its next Snapshot hits a conflict
// and retargets the controller to a recovery branch.
func divergedSessionController(t *testing.T, dir, path string) *control.Controller {
	t.Helper()
	disk := agent.NewSession("sys prompt")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := disk.Save(path); err != nil {
		t.Fatalf("save disk session: %v", err)
	}

	stale := agent.NewSession("sys prompt")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	return control.New(control.Options{
		Executor:    agent.New(nil, nil, stale, agent.Options{}, event.Discard),
		SessionDir:  dir,
		SessionPath: path,
		Label:       "deepseek-flash",
	})
}

// TestModelSwitchCarriesRecoveryPathAfterSnapshotConflict is the TUI /model
// twin of the desktop rebuild fix: when the pre-switch Snapshot retargets the
// controller to a recovery branch, the resume path handed to buildController
// must be that recovery path. A pre-snapshot capture bound the just-recovered
// transcript back to the original file, re-conflicting on every later save.
func TestModelSwitchCarriesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "model-switch-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, originalPath)
	m.modelRef = "old/old-model"
	var gotResumePath string
	m.buildController = func(_ string, _ []provider.Message, resumePath string) (*control.Controller, error) {
		gotResumePath = resumePath
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	m.runModelSubcommand("/model deepseek-flash/deepseek-v4-flash")
	if m.pendingModelSwitch == nil {
		t.Fatal("runModelSubcommand did not queue a model switch")
	}
	m.pendingModelSwitch()

	if gotResumePath == "" || gotResumePath == originalPath || !strings.Contains(filepath.Base(gotResumePath), "-recovery-") {
		t.Fatalf("resume path = %q, want recovery path distinct from %q", gotResumePath, originalPath)
	}
	if got := m.ctrl.SessionPath(); got != gotResumePath {
		t.Fatalf("old controller session path = %q, want recovery path %q", got, gotResumePath)
	}
}

// TestEffortSwitchCarriesRecoveryPathAfterSnapshotConflict covers the same
// contract for the TUI /effort rebuild path.
func TestEffortSwitchCarriesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	isolateUserConfig(t)
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "effort-switch-conflict.jsonl")

	m := newTestChatTUI()
	m.ctrl = divergedSessionController(t, dir, originalPath)
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	var gotResumePath string
	m.buildController = func(_ string, _ []provider.Message, resumePath string) (*control.Controller, error) {
		gotResumePath = resumePath
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	cmd := m.runEffortCommand("/effort max")
	if cmd == nil {
		t.Fatal("runEffortCommand did not queue a rebuild")
	}
	cmd()

	if gotResumePath == "" || gotResumePath == originalPath || !strings.Contains(filepath.Base(gotResumePath), "-recovery-") {
		t.Fatalf("resume path = %q, want recovery path distinct from %q", gotResumePath, originalPath)
	}
	if got := m.ctrl.SessionPath(); got != gotResumePath {
		t.Fatalf("old controller session path = %q, want recovery path %q", got, gotResumePath)
	}
}
