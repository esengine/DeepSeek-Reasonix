package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/memory"
)

func TestEffectMigrationBackupDoesNotMovePrefix(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", effectProbeConfig)
	approveWorkspace(t, dir)
	store := memory.StoreFor(config.MemoryUserDir(), dir)
	if _, err := store.Save(memory.Memory{Name: "pinned-fact", Body: "Neutral pinned guidance.", Activation: memory.ActivationPinned}); err != nil {
		t.Fatal(err)
	}
	build := func() string {
		ctrl, err := Build(context.Background(), Options{Sink: event.Discard, WorkspaceRoot: dir})
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		return systemMessage(ctrl.History())
	}
	before := build()
	if !strings.Contains(before, "Neutral pinned guidance.") {
		t.Fatal("fixture did not reach memory prefix")
	}
	for _, root := range []string{store.Dir, store.GlobalDir} {
		writeFile(t, filepath.Join(root, ".migration-backup"), "ghost.md", "---\nname: ghost\nactivation: pinned\ntype: user\nscope: global\n---\nbackup-only-marker")
	}
	if after := build(); after != before {
		t.Fatalf("backup moved prefix: %q", firstDivergence(before, after))
	}
}

func TestEffectMigrationBackupFailureWarnsAtBoot(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", effectProbeConfig)
	approveWorkspace(t, dir)
	store := memory.StoreFor(config.MemoryUserDir(), dir)
	legacy := "---\nname: legacy-fact\n---\nneutral legacy body\n"
	writeFile(t, store.Dir, "legacy-fact.md", legacy)
	writeFile(t, store.Dir, ".migration-backup", "blocked")
	var warnings []event.Event
	ctrl, err := Build(context.Background(), Options{WorkspaceRoot: dir, Sink: event.FuncSink(func(e event.Event) {
		if e.Level == event.LevelWarn {
			warnings = append(warnings, e)
		}
	})})
	if err != nil {
		t.Fatalf("backup failure crashed boot: %v", err)
	}
	defer ctrl.Close()
	found := false
	for _, warning := range warnings {
		if warning.Code == event.NoticeCodeMemoryMigrationBackup {
			found = true
		}
	}
	if !found {
		t.Fatalf("typed backup warning missing: %+v", warnings)
	}
	got, err := os.ReadFile(filepath.Join(store.Dir, "legacy-fact.md"))
	if err != nil || string(got) != legacy {
		t.Fatalf("boot rewrote unbacked fact: %q, %v", got, err)
	}
}
