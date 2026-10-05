package theme

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func installThemePlugin(t *testing.T, home string, enabled bool) string {
	t.Helper()
	root := filepath.Join(home, "plugins", "neon")
	writePack(t, filepath.Join(root, "themes"), "night", minimal)
	if err := os.WriteFile(filepath.Join(root, "themes", "night", "background.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"apiVersion":"reasonix.io/plugin/v2","name":"neon","version":"1.0.0","contributes":{"themes":["themes/*/theme.json"]}}`
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "neon", Root: "plugins/neon", Version: "1.0.0", ManifestKind: "reasonix", Enabled: enabled}); err != nil {
		t.Fatal(err)
	}
	return root
}

// A plugin's pack is read in place under a name that says whose it is, and it
// serves its own images; it is never copied into the user's library.
func TestAnEnabledPluginContributesAPackUnderItsOwnName(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	installThemePlugin(t, home, true)

	const id = "plugin:neon:night"
	var listed bool
	for _, p := range List() {
		listed = listed || p.ID == id
	}
	if !listed {
		t.Fatalf("List() lacks %s: %+v", id, List())
	}
	pack, err := Load(id)
	if err != nil || pack.Name != "Dusk" || pack.Background == nil || !pack.Background.Image {
		t.Fatalf("Load = %+v, %v; want the pack with its background", pack, err)
	}
	if raw, _, err := Asset(id, assetBackground); err != nil || string(raw) != "png" {
		t.Fatalf("Asset = %q, %v", raw, err)
	}
	if entries, _ := os.ReadDir(Dir()); len(entries) != 0 {
		t.Fatalf("the user library gained %d entries", len(entries))
	}
}

func TestADisabledPluginContributesNoPack(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	installThemePlugin(t, home, false)
	if _, err := Load("plugin:neon:night"); err == nil {
		t.Fatal("a disabled plugin's pack loaded")
	}
	if _, _, err := Asset("plugin:neon:night", assetBackground); err == nil {
		t.Fatal("a disabled plugin's image was served")
	}
}
