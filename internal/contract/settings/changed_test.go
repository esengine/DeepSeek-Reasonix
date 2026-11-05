package settings

import (
	"slices"
	"testing"

	"reasonix/internal/contract/config"
)

func TestChangedListsOnlyTheKeysThatDifferFromTheDefaults(t *testing.T) {
	def := config.Default()
	if got := Changed(def, config.Default()); len(got) != 0 {
		t.Fatalf("a default config differs from itself in %v", got)
	}
	cfg := config.Default()
	cfg.UI.ShowChangedFiles = !cfg.UI.ShowChangedFiles
	cfg.UI.FoldTurnWork = !cfg.UI.FoldTurnWork
	got := Changed(cfg, def)
	want := []string{"ui.fold_turn_work", "ui.show_changed_files"}
	if !slices.Equal(got, want) {
		t.Fatalf("Changed = %v, want %v", got, want)
	}
}

func TestChangedNeverNamesASecretOrAStructuredKey(t *testing.T) {
	cfg := config.Default()
	cfg.ConfigVersion++
	for _, id := range Changed(cfg, config.Default()) {
		if e, _ := Lookup(id); e.Secret || ShapeOf(id) == ShapeStructured {
			t.Fatalf("Changed named %s, which is not a plain value", id)
		}
	}
}
