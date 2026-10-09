package pluginpkg

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestInstalledPromptTextNamesQualifiedInvocations(t *testing.T) {
	home := testenv.TempDir(t)
	root := InstallRoot(home, "notes-kit")
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"notes-kit","contributes":{"prompts":["prompts"]}}`)
	writeTestFile(t, filepath.Join(root, "prompts", "review", "brief.md"), "---\ndescription: Summarize a note\nargument-hint: <note>\n---\nSummarize $ARGUMENTS")
	writeTestFile(t, filepath.Join(root, "prompts", "plain.md"), "Read $ARGUMENTS")
	for _, enabled := range []bool{true, false} {
		if err := Upsert(home, InstalledPlugin{Name: "notes-kit", Root: RelativeRoot(home, root), ManifestKind: "reasonix", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out, err := InstalledShowText(home, "notes-kit")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"2 prompts", "prompts:\n", "/notes-kit:review:brief <note> - Summarize a note", "/notes-kit:plain - (no description)"} {
			if !strings.Contains(out, want) {
				t.Errorf("enabled=%v: missing %q:\n%s", enabled, want, out)
			}
		}
		if !enabled && !strings.Contains(out, "enable this plugin before") {
			t.Errorf("disabled package lost its usage warning: %s", out)
		}
	}
}
