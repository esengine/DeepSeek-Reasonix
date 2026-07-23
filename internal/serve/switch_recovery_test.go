package serve

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
)

// primarySessionFiles filters a recovery-branch glob down to primary session
// transcripts, dropping the .events.jsonl / .guardian.jsonl sidecars that the
// *-recovery-*.jsonl pattern also matches.
func primarySessionFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".jsonl") &&
			!strings.HasSuffix(base, ".events.jsonl") &&
			!strings.HasSuffix(base, ".guardian.jsonl") {
			out = append(out, path)
		}
	}
	return out
}

// TestSwitchModelContinuesRecoveryPathAfterSnapshotConflict is the serve twin
// of the desktop rebuild fix: when the pre-switch Snapshot hits a conflict and
// retargets the old controller to a recovery branch, the rebuilt controller
// must continue on that recovery path. Capturing prevPath before Snapshot
// bound the just-recovered transcript back to the original file, so every
// later save re-conflicted and derived yet another recovery branch.
func TestSwitchModelContinuesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "switch-conflict.jsonl")

	disk := agent.NewSession("sys prompt")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := disk.Save(originalPath); err != nil {
		t.Fatalf("save disk session: %v", err)
	}

	stale := agent.NewSession("sys prompt")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})

	bc := NewBroadcaster()
	old := control.New(control.Options{
		Executor:    agent.New(nil, nil, stale, agent.Options{}, event.Discard),
		SessionDir:  dir,
		SessionPath: originalPath,
		Label:       "old",
		Sink:        bc,
	})
	s := &Server{ctrl: old, bc: bc}

	var built *control.Controller
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		built = control.New(control.Options{
			Executor:   agent.New(nil, nil, agent.NewSession("sys prompt"), agent.Options{}, event.Discard),
			SessionDir: dir,
			Label:      "new",
			Sink:       bc,
		})
		return built, nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}

	recoveryPath := built.SessionPath()
	if recoveryPath == "" || recoveryPath == originalPath || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("switched session path = %q, want recovery path distinct from %q", recoveryPath, originalPath)
	}
	if s.ctl() != built {
		t.Fatal("switchModel did not publish the rebuilt controller")
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches: %v", err)
	}
	matches = primarySessionFiles(matches)
	if len(matches) != 1 || matches[0] != recoveryPath {
		t.Fatalf("recovery branches after switch = %v, want only %q", matches, recoveryPath)
	}

	// The rebuilt controller adopted the recovery file's baseline, so its next
	// snapshot must not derive a second recovery branch.
	if err := built.Snapshot(); err != nil {
		t.Fatalf("Snapshot after switch: %v", err)
	}
	matches, err = filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches after snapshot: %v", err)
	}
	matches = primarySessionFiles(matches)
	if len(matches) != 1 || matches[0] != recoveryPath {
		t.Fatalf("recovery branches after follow-up snapshot = %v, want only %q", matches, recoveryPath)
	}
}

// TestSwitchModelRefreshesLeadingSystemPrompt pins the fix for the bug where
// switchModel rebuilt the controller with the target model/profile's own
// system prompt, only for AdoptHistory to immediately overwrite it with the
// carried history's leading message — the outgoing controller's system
// prompt. The user-visible symptom was that the model kept following the
// previous system prompt after every /model switch.
func TestSwitchModelRefreshesLeadingSystemPrompt(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldSession.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})

	bc := NewBroadcaster()
	old := control.New(control.Options{
		Executor:   agent.New(nil, nil, oldSession, agent.Options{}, event.Discard),
		SessionDir: dir,
		Label:      "old",
		Sink:       bc,
	})
	s := &Server{ctrl: old, bc: bc}
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		return control.New(control.Options{
			Executor:   agent.New(nil, nil, agent.NewSession("new system prompt"), agent.Options{}, event.Discard),
			SessionDir: dir,
			Label:      "new",
			Sink:       bc,
		}), nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}

	history := s.ctl().History()
	if len(history) != 3 || history[0].Role != provider.RoleSystem {
		t.Fatalf("history = %+v, want a leading system message", history)
	}
	if got, want := history[0].Content, "new system prompt"; got != want {
		t.Fatalf("leading system message = %q, want %q (stale outgoing prompt carried forward)", got, want)
	}
	if history[1].Content != "hello" || history[2].Content != "hi" {
		t.Fatalf("history after switch = %+v, want carried user/assistant turns preserved", history)
	}
}

// TestSwitchModelRestoresSessionAuthorizations pins the fix for switchModel
// dropping same-session "Allow for this session" tool grants and Plan-mode
// read-only command trust on every /model switch, forcing the user to
// re-approve something already granted this session.
func TestSwitchModelRestoresSessionAuthorizations(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()

	bc := NewBroadcaster()
	old := control.New(control.Options{
		Executor:   agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard),
		SessionDir: dir,
		Label:      "old",
		Sink:       bc,
	})
	old.RestoreSessionAuthorizations(control.SessionAuthorizations{
		Grants:                   []string{"bash|go test ./..."},
		PlanModeReadOnlyCommands: []string{"go test ./..."},
	})

	s := &Server{ctrl: old, bc: bc}
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		return control.New(control.Options{
			Executor:   agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard),
			SessionDir: dir,
			Label:      "new",
			Sink:       bc,
		}), nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}

	newCtrl, ok := s.ctl().(*control.Controller)
	if !ok {
		t.Fatalf("s.ctl() = %T, want *control.Controller", s.ctl())
	}
	got := newCtrl.SessionAuthorizations()
	if len(got.Grants) != 1 || got.Grants[0] != "bash|go test ./..." {
		t.Fatalf("restored grants = %+v, want [\"bash|go test ./...\"]", got.Grants)
	}
	if len(got.PlanModeReadOnlyCommands) != 1 || got.PlanModeReadOnlyCommands[0] != "go test ./..." {
		t.Fatalf("restored plan-mode read-only commands = %+v, want [\"go test ./...\"]", got.PlanModeReadOnlyCommands)
	}
}

// TestSwitchModelPersistsRefreshedSystemPromptToDisk pins the disk half of the
// system-prompt splice: switchModel refreshes the leading system message in
// the new controller's memory, and nothing snapshots an idle session again, so
// the switch itself must persist the adopted history or a restart + /resume
// revives the outgoing controller's contract from disk.
func TestSwitchModelPersistsRefreshedSystemPromptToDisk(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "switch-persist.jsonl")

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldSession.Add(provider.Message{Role: provider.RoleAssistant, Content: "hi"})
	if err := oldSession.Save(path); err != nil {
		t.Fatalf("save base session: %v", err)
	}

	bc := NewBroadcaster()
	old := control.New(control.Options{
		Executor:    agent.New(nil, nil, oldSession, agent.Options{}, event.Discard),
		SessionDir:  dir,
		SessionPath: path,
		Label:       "old",
		Sink:        bc,
	})
	s := &Server{ctrl: old, bc: bc}
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		return control.New(control.Options{
			Executor:   agent.New(nil, nil, agent.NewSession("new system prompt"), agent.Options{}, event.Discard),
			SessionDir: dir,
			Label:      "new",
			Sink:       bc,
		}), nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}

	loaded, err := agent.LoadSession(s.ctl().SessionPath())
	if err != nil {
		t.Fatalf("load transcript after switch: %v", err)
	}
	msgs := loaded.Snapshot()
	if len(msgs) != 3 || msgs[0].Role != provider.RoleSystem {
		t.Fatalf("on-disk history after switch = %+v, want 3 messages with a leading system message", msgs)
	}
	if got, want := msgs[0].Content, "new system prompt"; got != want {
		t.Fatalf("on-disk leading system message = %q, want %q (a restart would revive the outgoing contract)", got, want)
	}
}

func TestSwitchModelSnapshotFailureKeepsOldController(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	invalidSessionDir := filepath.Join(t.TempDir(), "session-dir-is-a-file")
	if err := os.WriteFile(invalidSessionDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write invalid session dir: %v", err)
	}

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	bc := NewBroadcaster()
	old := control.New(control.Options{
		Executor: agent.New(nil, nil, oldSession, agent.Options{}, event.Discard),
		Label:    "old",
		Sink:     bc,
	})
	t.Cleanup(old.Close)
	s := &Server{ctrl: old, bc: bc}
	s.buildController = func(_ context.Context, _ string) (*control.Controller, error) {
		return control.New(control.Options{
			Executor:   agent.New(nil, nil, agent.NewSession("new system prompt"), agent.Options{}, event.Discard),
			SessionDir: invalidSessionDir,
			Label:      "new",
			Sink:       bc,
		}), nil
	}

	err := s.switchModel(context.Background(), "next-model")
	if err == nil || !strings.Contains(err.Error(), "snapshot adopted history") {
		t.Fatalf("switchModel error = %v, want snapshot adopted history failure", err)
	}
	if got := s.ctl(); got != old {
		t.Fatalf("active controller changed after persistence failure: got %T %p, want outgoing %p", got, got, old)
	}
}
