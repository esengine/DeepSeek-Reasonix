package serve

import (
	"context"
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
