package theme

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSurfaceOpacityManifest(t *testing.T) {
	const base = `{"schemaVersion":1,"tokens":{"light":{"bg":"#fff"},"dark":{"bg":"#111"}},"background":{"surfaceOpacity":%s}}`
	for _, tc := range []struct{ name, value, want, warning string }{
		{"independent", `{"dark":{"nav":0,"rail":0.45,"chrome":0.8,"side":1,"compose":0.88},"light":{"nav":0.6}}`, `{"light":{"nav":0.6},"dark":{"nav":0,"rail":0.45,"chrome":0.8,"side":1,"compose":0.88}}`, ""},
		{"clamped", `{"dark":{"nav":-2,"compose":3}}`, `{"dark":{"nav":0,"compose":1}}`, ""},
		{"unknown surface", `{"dark":{"sidebar":0.4,"rail":0.5}}`, `{"dark":{"rail":0.5}}`, "background.surfaceOpacity.dark: \"sidebar\" is not a surface"},
		{"unknown scheme", `{"sepia":{"rail":0.4}}`, `null`, "background.surfaceOpacity: \"sepia\" is not a scheme"},
		{"empty", `{}`, `null`, ""},
		{"null values", `{"light":{"nav":null}}`, `null`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pack, _, err := decodeManifest([]byte(strings.Replace(base, "%s", tc.value, 1)), "probe")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(pack.Background)
			if err != nil {
				t.Fatal(err)
			}
			var bg map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &bg); err != nil {
				t.Fatal(err)
			}
			actual := string(bg["surfaceOpacity"])
			if actual == "" {
				actual = "null"
			}
			if actual != tc.want {
				t.Errorf("surfaceOpacity=%s, want %s", actual, tc.want)
			}
			if tc.warning == "" && len(pack.Warnings) != 0 {
				t.Fatalf("warnings=%v", pack.Warnings)
			}
			if tc.warning != "" && (len(pack.Warnings) != 1 || pack.Warnings[0] != tc.warning) {
				t.Fatalf("warnings=%v, want %q", pack.Warnings, tc.warning)
			}
		})
	}
	for _, value := range []string{`0.62`, `{"dark":{"nav":"0.5"}}`, `{"dark":[]}`} {
		if _, _, err := decodeManifest([]byte(strings.Replace(base, "%s", value, 1)), "probe"); err == nil || !strings.Contains(err.Error(), "surfaceOpacity") {
			t.Errorf("value %s: error=%v", value, err)
		}
	}
}
