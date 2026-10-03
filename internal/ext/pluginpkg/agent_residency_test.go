package pluginpkg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestAgentInventoryReadsOnlyResidentRegularSources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host privileges")
	}
	for _, tc := range []struct{ label, manifest, body string }{
		{"native", NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"resident-agents","contributes":{"agents":["agents"]}}`},
		{"Claude convention", ClaudeManifest, `{"name":"resident-agents"}`},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, outside := testenv.TempDir(t), testenv.TempDir(t)
			writeTestFile(t, filepath.Join(root, tc.manifest), tc.body)
			writeTestFile(t, filepath.Join(root, "agents", "ordinary.md"), "---\ndescription: Ordinary profile\n---\nORDINARY")
			writeTestFile(t, filepath.Join(root, "profiles", "resident.md"), "---\ndescription: Linked resident profile\n---\nRESIDENT")
			writeTestFile(t, filepath.Join(outside, "profile.md"), "---\ndescription: Nonresident profile\n---\nNONRESIDENT")
			if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
				t.Fatal(err)
			}
			for _, link := range []struct{ name, target string }{
				{"resident.md", filepath.Join(root, "profiles", "resident.md")},
				{"nonresident.md", filepath.Join(outside, "profile.md")},
				{"nonregular.md", filepath.Join(root, "empty")},
				{"broken.md", filepath.Join(root, "missing.md")},
			} {
				if err := os.Symlink(link.target, filepath.Join(root, "agents", link.name)); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(root)
			for _, inputRoot := range []string{root, "."} {
				pkg, _, err := ParseDir(inputRoot)
				if err != nil {
					t.Fatal(err)
				}
				agents := pkg.Inventory().Agents
				if len(agents) != 2 || pkg.AgentCount() != 2 || agents[0].Name != "ordinary" || agents[1].Name != "resident" {
					t.Fatalf("resident-only inventory=%+v, count=%d", agents, pkg.AgentCount())
				}
				if agents[1].Path != filepath.Join(inputRoot, "agents", "resident.md") || agents[1].Description != "Linked resident profile" {
					t.Fatalf("resident link projection=%+v", agents[1])
				}
			}
		})
	}
}
