package pluginpkg

import (
	"errors"
	"os"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"testing"
)

func TestRevisionStateUsesStrictPublication(t *testing.T) {
	home := testenv.TempDir(t)
	synced := false
	restore := fileutil.SetSyncParentDirForTest(func(path string) error { synced = true; return nil })
	defer restore()
	if err := SaveState(home, State{Version: 1, Plugins: []InstalledPlugin{{Name: "neutral", Root: "plugins/neutral", Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if !synced {
		t.Fatal("registry writer did not use strict atomic publication")
	}
	st, err := LoadState(home)
	if err != nil || len(st.Plugins) != 1 {
		t.Fatalf("restart state: %+v, %v", st, err)
	}
	if err := SetEnabled(home, "neutral", false); err != nil {
		t.Fatal(err)
	}
	st, err = LoadState(home)
	if err != nil || st.Plugins[0].Enabled {
		t.Fatalf("disable restart: %+v, %v", st, err)
	}
	if _, ok, err := Remove(home, "neutral"); err != nil || !ok {
		t.Fatalf("remove: %v, %v", ok, err)
	}
	st, err = LoadState(home)
	if err != nil || len(st.Plugins) != 0 {
		t.Fatalf("remove restart: %+v, %v", st, err)
	}
}

func TestRevisionPublicationRechecksAfterABlockedRename(t *testing.T) {
	home := testenv.TempDir(t)
	if err := SaveState(home, State{Version: 1}); err != nil {
		t.Fatal(err)
	}
	errChanged := errors.New("approved bytes changed")
	checks := 0
	err := UpsertValidated(home, InstalledPlugin{Name: "neutral", Root: "plugins/neutral", Enabled: true}, func() error {
		checks++
		if checks == 1 {
			// A directory in the registry's place makes every rename fail.
			if err := os.Remove(StatePath(home)); err != nil {
				return err
			}
			return os.Mkdir(StatePath(home), 0o755)
		}
		return errChanged
	})
	if !errors.Is(err, errChanged) || !errors.Is(err, ErrPublicationFailed) {
		t.Fatalf("err = %v, want the re-check failure carrying the publication identity", err)
	}
	if checks != 2 {
		t.Fatalf("checks = %d, want 2: one before each rename attempt, none after a failure", checks)
	}
}
