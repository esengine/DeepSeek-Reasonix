package pluginpkg_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
)

func TestPackageAgentNamesMatchRuntime(t *testing.T) {
	for _, tc := range []struct{ label, stem, frontName, want string }{
		{"Chinese", "审查", "", "审查"},
		{"Japanese", "点検", "点検", "点検"},
		{"decomposed stem", "cafe\u0301", "", "café"},
		{"Unicode override", "审查", "点検", "点検"},
		{"decomposed override", "审查", "re\u0301vision", "révision"},
		{"ASCII stem compatibility", "review", "审查", "review"},
		{"ASCII override", "review", "inspect", "inspect"},
		{"64 runes", strings.Repeat("审", 64), "", strings.Repeat("审", 64)},
		{"65 runes", strings.Repeat("审", 65), "", ""},
		{"leading mark", "\u0301review", "", ""},
		{"invisible stem", "rev\u200diew", "", ""},
		{"variation selector", "审\ufe0f", "", ""},
		{"invalid override", "审查", "bad/name", "审查"},
		{"invalid stem with override", "bad name", "inspect", ""},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, home := testenv.TempDir(t), testenv.TempDir(t)
			write := func(rel, body string) {
				t.Helper()
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"names-kit","contributes":{"agents":["agents"]}}`)
			write(filepath.Join("agents", tc.stem+".md"), "---\nname: "+tc.frontName+"\ndescription: Agent name fixture\n---\nBODY")
			pkg, warnings, err := pluginpkg.ParseDir(root)
			if err != nil || len(warnings) != 0 {
				t.Fatalf("ParseDir: warnings=%v err=%v", warnings, err)
			}
			owners := map[string][]string{pkg.AgentRoots()[0]: {"names-kit"}}
			runtime := skill.New(skill.Options{HomeDir: home, CustomPaths: pkg.AgentRoots(), PluginPaths: owners, PluginAgentPaths: owners, DisableBuiltins: true}).List()
			inventory := pkg.Inventory().Agents
			if tc.want == "" {
				if len(runtime) != 0 || len(inventory) != 0 || pkg.AgentCount() != 0 {
					t.Fatalf("invalid stem accepted: runtime=%+v inventory=%+v", runtime, inventory)
				}
				return
			}
			if len(runtime) != 1 || runtime[0].Name != tc.want || runtime[0].RunAs != skill.RunSubagent || runtime[0].SlashName() != "names-kit:agent:"+tc.want {
				t.Fatalf("runtime=%+v, want names-kit:agent:%s", runtime, tc.want)
			}
			if len(inventory) != 1 || inventory[0].Name != tc.want || inventory[0].Invocation != "/"+tc.want || pkg.AgentCount() != 1 {
				t.Fatalf("inventory=%+v, want /%s", inventory, tc.want)
			}
			if inventory[0].Path != runtime[0].Path {
				t.Fatalf("inventory path=%q runtime path=%q", inventory[0].Path, runtime[0].Path)
			}
		})
	}
}

func TestPackageRejectsCanonicalAgentNameCollisions(t *testing.T) {
	for _, tc := range []struct {
		label, roots, first, second string
		frontNames                  bool
	}{
		{"frontmatter names", `["agents"]`, "agents/第一.md", "agents/第二.md", true},
		{"filenames across roots", `["second","first"]`, "first/café.md", "second/cafe\u0301.md", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, home := testenv.TempDir(t), testenv.TempDir(t)
			write := func(rel, body string) {
				t.Helper()
				path := filepath.Join(root, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write(pluginpkg.NativeManifest, fmt.Sprintf(`{"apiVersion":"reasonix.io/plugin/v2","name":"names-kit","contributes":{"agents":%s}}`, tc.roots))
			firstBody, secondBody := "---\ndescription: First fixture\n---\nFIRST", "---\ndescription: Second fixture\n---\nSECOND"
			if tc.frontNames {
				firstBody = "---\nname: café\ndescription: First fixture\n---\nFIRST"
				secondBody = "---\nname: cafe\u0301\ndescription: Second fixture\n---\nSECOND"
			}
			write(tc.first, firstBody)
			write(tc.second, secondBody)
			pkg, _, err := pluginpkg.ParseDir(root)
			if err == nil {
				owners := map[string][]string{}
				for _, path := range pkg.AgentRoots() {
					owners[path] = []string{"names-kit"}
				}
				runtime := skill.New(skill.Options{HomeDir: home, CustomPaths: pkg.AgentRoots(), PluginPaths: owners, PluginAgentPaths: owners, DisableBuiltins: true}).SlashList()
				t.Fatalf("canonical collision accepted: inventory=%+v runtime=%+v", pkg.Inventory().Agents, runtime)
			}
			if !strings.Contains(err.Error(), `agent name "café"`) || !strings.Contains(err.Error(), "both") {
				t.Fatalf("collision diagnostic = %v", err)
			}
		})
	}
}

func TestPackageAllowsRepeatedAgentPathAndSeparateSkillName(t *testing.T) {
	root := testenv.TempDir(t)
	for _, rel := range []string{"agents/review.md", "skills/review/SKILL.md"} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("---\ndescription: Separate namespace fixture\n---\nBODY"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"apiVersion":"reasonix.io/plugin/v2","name":"names-kit","contributes":{"agents":["agents","agents"],"skills":["skills"]}}`
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("repeated path and separate namespace: warnings=%v err=%v", warnings, err)
	}
	inv := pkg.Inventory()
	if len(inv.Skills) != 1 || inv.Skills[0].Name != "review" || len(inv.Agents) == 0 {
		t.Fatalf("separate namespaces inventory = %+v", inv)
	}
	for _, ref := range inv.Agents {
		if ref.Name != "review" || ref.Path != filepath.Join(root, "agents", "review.md") {
			t.Fatalf("repeated declaration changed its source: %+v", ref)
		}
	}
}
