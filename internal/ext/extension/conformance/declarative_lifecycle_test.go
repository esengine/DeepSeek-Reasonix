package conformance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
)

func TestDeclarativePackageInstallLifecycle(t *testing.T) {
	cases := []struct {
		name         string
		manifestPath string
		manifest     string
		manifestKind string
		mcpCount     int
	}{
		{
			name: "native", manifestPath: pluginpkg.NativeManifest,
			manifest:     `{"apiVersion":"reasonix.io/plugin/v2","name":"fixture","version":"1.0.0","contributes":{"skills":["skills"],"mcpServers":{"helper":{"type":"http","url":"https://example.test/mcp"}}}}`,
			manifestKind: "reasonix", mcpCount: 1,
		},
		{
			name: "codex", manifestPath: pluginpkg.CodexManifest,
			manifest:     `{"name":"fixture","version":"1.0.0","skills":"./skills/"}`,
			manifestKind: "codex",
		},
		{
			name: "claude", manifestPath: pluginpkg.ClaudeManifest,
			manifest:     `{"name":"fixture","version":"1.0.0","skills":"./skills/"}`,
			manifestKind: "claude", mcpCount: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := testenv.TempDir(t)
			copyFixtureBytes(t, filepath.Join(source, tc.manifestPath), []byte(tc.manifest))
			copyFixtureBytes(t, filepath.Join(source, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review a change\n---\nReview the change."))
			if tc.name == "claude" {
				copyFixtureBytes(t, filepath.Join(source, ".mcp.json"), []byte(`{"mcpServers":{"helper":{"type":"http","url":"https://example.test/mcp"}}}`))
			}
			home := testenv.TempDir(t)
			reasonixHome := filepath.Join(home, ".reasonix")
			tool := installsource.NewTool(installsource.Options{ProjectRoot: testenv.TempDir(t), HomeDir: home})
			for _, apply := range []bool{false, true} {
				input, _ := json.Marshal(map[string]any{"source": source, "kind": "plugin", "apply": apply})
				output, err := tool.Execute(context.Background(), input)
				if err != nil {
					t.Fatalf("install_source apply=%t: %v", apply, err)
				}
				var result struct {
					OK      bool   `json:"ok"`
					Status  string `json:"status"`
					Actions []struct {
						SkillCount int `json:"skillCount"`
						ToolCount  int `json:"toolCount"`
					} `json:"actions"`
				}
				if err := json.Unmarshal([]byte(output), &result); err != nil {
					t.Fatal(err)
				}
				wantStatus := "planned"
				if apply {
					wantStatus = "done"
				}
				if !result.OK || result.Status != wantStatus || len(result.Actions) != 1 || result.Actions[0].SkillCount != 1 || result.Actions[0].ToolCount != tc.mcpCount {
					t.Fatalf("install_source apply=%t: %s", apply, output)
				}
				if !apply {
					installed, warnings := pluginpkg.LoadInstalled(reasonixHome)
					if len(installed) != 0 || len(warnings) != 0 {
						t.Fatalf("preview installed packages=%d warnings=%v", len(installed), warnings)
					}
				}
			}
			assertLoaded := func(want int) {
				t.Helper()
				installed, warnings := pluginpkg.LoadInstalled(reasonixHome)
				if len(warnings) != 0 || len(installed) != want {
					t.Fatalf("loaded packages=%d warnings=%v, want %d", len(installed), warnings, want)
				}
				if want == 1 {
					item := installed[0]
					inventory := item.Package.Inventory()
					if item.Package.ManifestKind != tc.manifestKind || len(inventory.Skills) != 1 || inventory.Skills[0].Name != "review" || len(inventory.MCPServers) != tc.mcpCount {
						t.Fatalf("installed package=%+v inventory=%+v", item.Package, inventory)
					}
				}
			}
			assertLoaded(1)
			if err := pluginpkg.SetEnabled(reasonixHome, "fixture", false); err != nil {
				t.Fatal(err)
			}
			assertLoaded(0)
			if err := pluginpkg.SetEnabled(reasonixHome, "fixture", true); err != nil {
				t.Fatal(err)
			}
			assertLoaded(1)
			if _, found, err := pluginpkg.Remove(reasonixHome, "fixture"); err != nil || !found {
				t.Fatalf("remove package: found=%t err=%v", found, err)
			}
			assertLoaded(0)
		})
	}
}

func copyFixtureBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
