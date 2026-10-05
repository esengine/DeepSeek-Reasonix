package pluginpkg_test

import (
	"fmt"
	"io"
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

func TestPackageCanonicalAgentCollisionMatchesRuntime(t *testing.T) {
	for _, tc := range []struct {
		label, roots, first, second string
		frontNames                  bool
	}{
		{"frontmatter names", `["agents"]`, "agents/第一.md", "agents/第二.md", true},
		{"composed first across roots", `["second","first"]`, "first/café.md", "second/cafe\u0301.md", false},
		{"decomposed first across roots", `["second","first"]`, "first/cafe\u0301.md", "second/café.md", false},
		{"nested before flat", `["agents"]`, "agents/a-group/café.md", "agents/cafe\u0301.md", false},
		{"directory before flat", `["agents"]`, "agents/第一/SKILL.md", "agents/第二.md", true},
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
			pkg, warnings, err := pluginpkg.ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			owners := map[string][]string{}
			for _, path := range pkg.AgentRoots() {
				owners[path] = []string{"names-kit"}
			}
			store := skill.New(skill.Options{HomeDir: home, ReasonixHomeDir: home, CustomPaths: pkg.AgentRoots(), PluginPaths: owners, PluginAgentPaths: owners, DisableBuiltins: true, Stderr: io.Discard})
			winner, ok := store.ReadSlash("names-kit:agent:café")
			if !ok || winner.Body != "FIRST" {
				t.Fatalf("actual runtime winner=%+v found=%t", winner, ok)
			}
			t.Logf("actual runtime selected %s: %s", pluginpkg.RelativeRoot(root, winner.Path), winner.Body)
			inventory := pkg.Inventory().Agents
			if len(inventory) != 1 || pkg.AgentCount() != 1 || inventory[0].Path != winner.Path || inventory[0].Name != winner.Name || inventory[0].Description != winner.Description {
				t.Fatalf("inventory=%+v, actual runtime winner=%+v", inventory, winner)
			}
			loserDir := filepath.Join(root, filepath.Dir(tc.second))
			entries, err := os.ReadDir(loserDir)
			if err != nil {
				t.Fatal(err)
			}
			loserInfo, err := os.Stat(filepath.Join(root, tc.second))
			if err != nil {
				t.Fatal(err)
			}
			var loser string
			for _, entry := range entries {
				info, err := entry.Info()
				if err == nil && os.SameFile(info, loserInfo) {
					loser = filepath.Join(loserDir, entry.Name())
				}
			}
			if loser == "" {
				t.Fatal("fixture loser not found")
			}

			want := fmt.Sprintf("agent name %q uses %s; %s is shadowed", winner.Name, pluginpkg.RelativeRoot(root, winner.Path), pluginpkg.RelativeRoot(root, loser))
			if len(warnings) != 1 || warnings[0] != want {
				t.Fatalf("warnings=%v, want %q", warnings, want)
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
	if len(inv.Skills) != 1 || inv.Skills[0].Name != "review" || len(inv.Agents) != 1 {
		t.Fatalf("separate namespaces inventory = %+v", inv)
	}
	for _, ref := range inv.Agents {
		if ref.Name != "review" || ref.Path != filepath.Join(root, "agents", "review.md") {
			t.Fatalf("repeated declaration changed its source: %+v", ref)
		}
	}
}
