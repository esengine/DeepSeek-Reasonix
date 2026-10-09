package skill

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestScenarioExamplesDiscoverWithoutInvalidFrontmatter(t *testing.T) {
	for _, tc := range []struct {
		plugin string
		name   string
	}{
		{plugin: "release-note-kit", name: "release-note"},
		{plugin: "issue-fix-kit", name: "issue-fix"},
		{plugin: "frontend-page-kit", name: "frontend-page"},
		{plugin: "api-notes-kit", name: "api-notes"},
	} {
		t.Run(tc.plugin, func(t *testing.T) {
			root := testenv.TempDir(t)
			source := filepath.Join("..", "..", "..", "examples", tc.plugin)
			if err := os.CopyFS(root, os.DirFS(source)); err != nil {
				t.Fatal(err)
			}
			skillsRoot := filepath.Join(root, "skills")
			store := New(Options{
				HomeDir:         testenv.TempDir(t),
				CustomPaths:     []string{skillsRoot},
				PluginPaths:     map[string][]string{skillsRoot: {tc.plugin}},
				DisableBuiltins: true,
			})
			skills := store.SlashList()
			if len(skills) != 1 {
				t.Fatalf("discovered skills = %v, want one scenario skill", skills)
			}
			sk := skills[0]
			if sk.Name != tc.name || sk.SlashName() != tc.plugin+":"+tc.name {
				t.Fatalf("skill identity = %q / %q", sk.Name, sk.SlashName())
			}
			if sk.Description == "" || sk.Body == "" {
				t.Fatal("scenario skill lost its description or instructions")
			}
			if len(sk.Invalid) != 0 {
				t.Fatalf("scenario skill frontmatter diagnostics = %v", sk.Invalid)
			}
			if err := store.ValidateInvocation(sk); err != nil {
				t.Fatalf("scenario skill invocation: %v", err)
			}
		})
	}
}
