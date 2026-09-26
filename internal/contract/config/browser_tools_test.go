package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func decodedTools(t *testing.T, body string) *Config {
	t.Helper()
	c := Default()
	if _, err := decodeTOMLBytes([]byte(body), c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return c
}

// The switch is its own key: absent is on, written false is off, and the
// shared [browser] enabled that the 1.x line owns moves nothing either way.
func TestBrowserToolsSwitchReadsItsOwnKey(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"absent", "", true},
		{"written true", "[tools]\nbrowser_tools = true\n", true},
		{"written false", "[tools]\nbrowser_tools = false\n", false},
		{"shared key false", "[browser]\nenabled = false\n", true},
		{"shared key true, switch off", "[tools]\nbrowser_tools = false\n\n[browser]\nenabled = true\n", false},
	}
	for _, tc := range cases {
		if got := decodedTools(t, tc.body).Tools.BrowserToolsEnabled(); got != tc.want {
			t.Errorf("%s: BrowserToolsEnabled = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func browserSectionOf(t *testing.T, path string) map[string]any {
	t.Helper()
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		t.Fatalf("saved file does not parse: %v", err)
	}
	section, _ := raw["browser"].(map[string]any)
	return section
}

// A save from here carries the shared key through untouched: a value the 1.x
// line wrote stays what it wrote, and a file that never had one does not
// acquire one that would switch the 1.x command line's browser on.
func TestSaveCarriesTheSharedBrowserKeyThrough(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		want  any
		holds bool
	}{
		{"1.x wrote false", "[browser]\nenabled = false\nheadless = true\n", false, true},
		{"1.x wrote true", "[browser]\nenabled = true\n", true, true},
		{"never written", "[browser]\nheadless = true\n", nil, false},
		{"no section", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserConfigHome(t)
			path := UserConfigPath()
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := LoadForEdit(path)
			off := false
			cfg.Tools.BrowserTools = &off
			if err := cfg.SaveTo(path); err != nil {
				t.Fatal(err)
			}
			got, present := browserSectionOf(t, path)["enabled"]
			if present != tc.holds || (present && got != tc.want) {
				raw, _ := os.ReadFile(path)
				t.Fatalf("[browser] enabled after save = %v (present %v), want %v (present %v)\n%s", got, present, tc.want, tc.holds, raw)
			}
			if LoadForEdit(path).Tools.BrowserToolsEnabled() {
				t.Fatal("browser_tools = false did not survive the save")
			}
		})
	}
}

// The project delta writes the switch only when someone set it.
func TestProjectDeltaWritesBrowserToolsOnlyWhenSet(t *testing.T) {
	c := Default()
	if strings.Contains(RenderTOMLProjectDelta(c), "browser_tools") {
		t.Fatal("an unset switch reached the project delta")
	}
	off := false
	c.Tools.BrowserTools = &off
	if !strings.Contains(RenderTOMLProjectDelta(c), "browser_tools = false") {
		t.Fatalf("a set switch is missing from the project delta:\n%s", RenderTOMLProjectDelta(c))
	}
}
