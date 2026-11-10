package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestTheModCeilingsHaveDefaultsAndAZeroIsAnExplicitChoice(t *testing.T) {
	want := ModsLimits{ContextBytes: 8192, ContextBlocks: 8, MessagesMax: 200, SubmitPerTurn: 3, SubmitBytes: 8192}
	if got := Default().ModsLimits(); got != want {
		t.Fatalf("defaults = %+v, want %+v", got, want)
	}
	cfg, _ := loadScoped(t, "[mods]\ncontext_bytes = 0\nsubmit_per_turn = 7\n", "")
	got := cfg.ModsLimits()
	if got.ContextBytes != 0 || got.SubmitPerTurn != 7 || got.ContextBlocks != 8 || got.MessagesMax != 200 {
		t.Fatalf("limits = %+v, want bytes 0 (no ceiling), submits 7, the rest at their defaults", got)
	}
}

// A repository could otherwise lift the ceiling on what a mod adds to a turn.
func TestAProjectFileCannotRaiseWhatAModMayAdd(t *testing.T) {
	cfg, _ := loadScoped(t, "", "[mods]\ncontext_bytes = 0\ncontext_blocks = 0\nmessages_max = 0\nsubmit_per_turn = 0\nsubmit_bytes = 0\n")
	if got, want := cfg.ModsLimits(), Default().ModsLimits(); got != want {
		t.Fatalf("a project file moved the ceilings to %+v", got)
	}
	for _, key := range []string{"mods.context_bytes", "mods.context_blocks", "mods.messages_max", "mods.submit_per_turn"} {
		if !slices.Contains(ignoredKeys(cfg), key) {
			t.Errorf("ignored = %v, want %s reported", ignoredKeys(cfg), key)
		}
	}
}

func TestSavingTheUserFileKeepsACustomModCeiling(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[mods]\nmessages_max = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := LoadForEdit(path)
	cfg.Agent.MaxSideSessions = 2
	if err := cfg.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if got := LoadForEdit(path).ModsLimits(); got.MessagesMax != 0 || got.ContextBytes != 8192 {
		t.Fatalf("after an unrelated save: %+v, want messages_max 0 kept and the others at their defaults", got)
	}
}
