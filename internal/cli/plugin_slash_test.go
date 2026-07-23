package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
)

func TestPluginsSlashShowsInstalledPluginDetails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	root := filepath.Join(home, "plugins", "superpowers")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.CodexManifest), `{
	  "name": "superpowers",
	  "version": "6.1.0",
	  "description": "Planning workflows",
	  "skills": "skills"
	}`)
	writePluginTestFile(t, filepath.Join(root, "skills", "plan", "SKILL.md"), "---\ndescription: Plan work\n---\nbody")
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "superpowers", Root: "plugins/superpowers", Version: "6.1.0", ManifestKind: "codex", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	m := newTestChatTUI()
	if cmd := m.runSlashCommand("/plugins show superpowers"); cmd != nil {
		t.Fatal("/plugins show should render locally")
	}
	out := strings.Join(m.transcript, "\n")
	for _, want := range []string{"plugin superpowers [enabled]", "/superpowers:plan", "usage: enabled plugins load into new sessions"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/plugins show output missing %q:\n%s", want, out)
		}
	}
}

func writePluginTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
