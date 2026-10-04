package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
)

func TestPluginShowNamesQualifiedPromptInvocations(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	root := pluginpkg.InstallRoot(home, "notes-kit")
	writePluginTestFile(t, filepath.Join(root, pluginpkg.NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"notes-kit","contributes":{"prompts":["prompts"]}}`)
	writePluginTestFile(t, filepath.Join(root, "prompts", "review", "brief.md"), "---\ndescription: Summarize a note\nargument-hint: <note>\n---\nSummarize $ARGUMENTS")
	writePluginTestFile(t, filepath.Join(root, "prompts", "plain.md"), "Read $ARGUMENTS")
	for _, enabled := range []bool{true, false} {
		if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{Name: "notes-kit", Root: pluginpkg.RelativeRoot(home, root), ManifestKind: "reasonix", Enabled: enabled}); err != nil {
			t.Fatal(err)
		}
		out := captureStdout(t, func() {
			if rc := pluginCommand([]string{"show", "notes-kit"}); rc != 0 {
				t.Fatalf("show rc=%d", rc)
			}
		})
		for _, want := range []string{"prompts: 2", "prompts:\n", "/notes-kit:review:brief <note>\tSummarize a note", "/notes-kit:plain\t(no description)"} {
			if !strings.Contains(out, want) {
				t.Errorf("enabled=%v: missing %q:\n%s", enabled, want, out)
			}
		}
	}
}
