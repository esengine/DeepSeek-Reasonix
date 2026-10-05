package pluginpkg

import (
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestThemeInventoryNamesDirectoryPacksAndStandaloneFiles(t *testing.T) {
	root := testenv.TempDir(t)
	writeV2Plugin(t, root, `{"apiVersion":"reasonix.io/plugin/v2","name":"palette-duo","contributes":{"themes":["themes/*/theme.json","themes/dawn/theme.json","themes/palette.json","themes/retro.reasonix-theme"]}}`)
	for _, path := range []string{"themes/dawn/theme.json", "themes/midnight/theme.json", "themes/palette.json", "themes/retro.reasonix-theme"} {
		writeTestFile(t, filepath.Join(root, path), "{}")
	}
	pkg, warnings, err := ParseDir(root)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("ParseDir = %v, %v", warnings, err)
	}
	want := []ThemeRef{
		{Name: "dawn", Path: filepath.Join(root, "themes", "dawn", "theme.json")},
		{Name: "midnight", Path: filepath.Join(root, "themes", "midnight", "theme.json")},
		{Name: "palette", Path: filepath.Join(root, "themes", "palette.json")},
		{Name: "retro", Path: filepath.Join(root, "themes", "retro.reasonix-theme")},
	}
	if got := pkg.ThemeFiles(); !reflect.DeepEqual(got, want) {
		t.Errorf("ThemeFiles = %+v, want %+v", got, want)
	}
	if got := pkg.Inventory().Themes; !reflect.DeepEqual(got, want) {
		t.Errorf("Inventory.Themes = %+v, want %+v", got, want)
	}
	if pkg.ThemeCount() != 4 || pkg.CapabilitySummary().Themes != 4 {
		t.Fatal("directory names changed the contribution count or path deduplication")
	}
}
