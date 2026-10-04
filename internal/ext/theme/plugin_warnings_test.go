package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginWarningsUsesThemeValidationWithoutRegistration(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	const valid = `{"schemaVersion":1,"tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#111111"}}}`
	for _, tc := range []struct {
		name string
		file string
		raw  string
		want []string
	}{
		{name: "valid", file: manifestName, raw: valid},
		{name: "invalid JSON", file: manifestName, raw: "{", want: []string{"unexpected end of JSON input"}},
		{name: "unsupported schema", file: manifestName, raw: strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":99`, 1), want: []string{"unsupported schemaVersion 99"}},
		{name: "missing scheme", file: manifestName, raw: `{"schemaVersion":1,"tokens":{"light":{"bg":"#FFFFFF"}}}`, want: []string{"no dark tokens"}},
		{name: "no usable tokens", file: manifestName, raw: `{"schemaVersion":1,"tokens":{"light":{"sidebar":"#FFFFFF"},"dark":{"bg":"#111111"}}}`, want: []string{"no usable light tokens"}},
		{name: "partial", file: manifestName, raw: `{"schemaVersion":1,"tokens":{"light":{"bg":"#FFFFFF","fg":"not-a-colour","sidebar":"#111111"},"dark":{"bg":"#111111"}}}`, want: []string{`"fg"`, `"sidebar" is not a theme token`}},
		{name: "other file", file: "ignored.txt", raw: "Not a theme manifest."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testenv.TempDir(t)
			dir := filepath.Join(root, "themes", "palette")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.raw), 0o644); err != nil {
				t.Fatal(err)
			}
			rel := "themes/palette/" + tc.file
			manifest := `{"apiVersion":"reasonix.io/plugin/v2","name":"unregistered","contributes":{"themes":["` + rel + `"]}}`
			if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			pkg, _, err := pluginpkg.ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			warnings := PluginWarnings(pkg)
			if len(warnings) != len(tc.want) {
				t.Fatalf("warnings=%v, want %d", warnings, len(tc.want))
			}
			for i, want := range tc.want {
				if !strings.HasPrefix(warnings[i], rel+": ") || !strings.Contains(warnings[i], want) {
					t.Errorf("warning=%q, want relative path and %q", warnings[i], want)
				}
			}
		})
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("diagnostics changed the registry home: %v, %v", entries, err)
	}
}
