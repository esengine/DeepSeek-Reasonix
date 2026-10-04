package pluginpkg

import (
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
