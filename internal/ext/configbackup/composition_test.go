package configbackup

import (
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/agentpreset"
)

func TestCompositionBackupRetainsDeclarationsButRequiresExplicitRestore(t *testing.T) {
	machine(t, userConfig)
	store := agentpreset.New(config.ReasonixHomeDir(), "")
	d := agentcomposition.Builtins()[1]
	d.ID = "user:review-copy"
	d.Persona.Suffix = "custom tail"
	v, err := store.Save(d, "")
	must(t, err)
	must(t, store.SetDefault(d.ID, false))
	snapshot, err := Collect(CollectOptions{Categories: []Category{CategorySettings}})
	must(t, err)
	if snapshot.Format != 3 {
		t.Fatalf("format=%d", snapshot.Format)
	}
	var preset Item
	for _, it := range snapshot.Items {
		if it.Kind == KindAgentPreset {
			preset = it
		}
	}
	if preset.ID == "" {
		t.Fatal("preset omitted")
	}
	must(t, store.Delete(d.ID, v.Revision))
	if consentFor(preset, snapshot, &localState{}) != ConsentExecutes {
		t.Fatal("restore omitted consent")
	}
	must(t, restoreCompositionItem(preset))
	restored, err := store.Get(d.ID)
	must(t, err)
	if restored.Revision != v.Revision {
		t.Fatal("backup changed declaration")
	}
	def, err := store.Default()
	must(t, err)
	if def != d.ID {
		t.Fatal("personal default lost")
	}
	snapshot.Format = 2
	if snapshot.validate() == nil {
		t.Fatal("old backup boundary ignored new item")
	}
}
