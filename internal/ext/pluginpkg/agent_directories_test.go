package pluginpkg

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestDeclaredAgentDirectoryInventory(t *testing.T) {
	for _, tc := range []struct {
		name, manifest, body string
	}{
		{"native-v2", NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"directory-agents","contributes":{"agents":["agents"]}}`},
		{"claude-convention", ClaudeManifest, `{"name":"directory-agents"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testenv.TempDir(t)
			writeTestFile(t, filepath.Join(root, tc.manifest), tc.body)
			writeTestFile(t, filepath.Join(root, "agents", "review", "SKILL.md"), "---\nname: inspect\ndescription: Inspect selected files\nmodel: custom-model\ntools: [Read, Grep]\n---\nInspect files.")
			writeTestFile(t, filepath.Join(root, "agents", "fallback", "SKILL.md"), "---\ndescription: Use directory name\nallowed-tools: [Read]\ntools: [Write]\n---\nInspect files.")
			writeTestFile(t, filepath.Join(root, "agents", "flat.md"), "---\ndescription: Flat profile\n---\nInspect files.")
			writeTestFile(t, filepath.Join(root, "agents", "notes", "README.md"), "Author notes.")
			writeTestFile(t, filepath.Join(root, "agents", "invalid name", "SKILL.md"), "---\nname: valid\n---\nInvalid directory.")
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			agents := pkg.Inventory().Agents
			if len(agents) != 3 || pkg.AgentCount() != 3 {
				t.Fatalf("agents=%+v, count=%d", agents, pkg.AgentCount())
			}
			if agents[0].Name != "fallback" || agents[1].Name != "flat" || agents[2].Name != "inspect" {
				t.Fatalf("agents=%+v", agents)
			}
			if !reflect.DeepEqual(agents[0].AllowedTools, []string{"Read"}) {
				t.Fatalf("directory allowed-tools=%v", agents[0].AllowedTools)
			}
			agent := agents[2]
			if agent.Path != filepath.Join(root, "agents", "review", "SKILL.md") ||
				agent.Description != "Inspect selected files" || agent.Model != "custom-model" ||
				!reflect.DeepEqual(agent.AllowedTools, []string{"Read", "Grep"}) {
				t.Fatalf("directory agent=%+v", agent)
			}
		})
	}
}

func TestClaudeAgentDirectoryDoesNotExpandConventionDiscovery(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name":"directory-agents"}`)
	writeTestFile(t, filepath.Join(root, "agents", "review", "SKILL.md"), "---\ndescription: Inspect files\n---\nInspect files.")
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.AgentRoots()) != 0 || len(pkg.Inventory().Agents) != 0 {
		t.Fatalf("undeclared directory adopted: %+v", pkg)
	}
}

func TestDeclaredNestedAgentInventory(t *testing.T) {
	for _, manifest := range []string{NativeManifest, ClaudeManifest} {
		t.Run(manifest, func(t *testing.T) {
			root := testenv.TempDir(t)
			body := `{"apiVersion":"reasonix.io/plugin/v2","name":"nested-agents","contributes":{"agents":["agents"]}}`
			if manifest == ClaudeManifest {
				body = `{"name":"nested-agents"}`
			}
			writeTestFile(t, filepath.Join(root, manifest), body)
			writeTestFile(t, filepath.Join(root, "agents", "seed.md"), "---\ndescription: Root convention marker\n---\nReview.")
			writeTestFile(t, filepath.Join(root, "agents", "group", "review", "SKILL.md"), "---\ndescription: Nested directory review\nmodel: custom-model\nallowed-tools: [Read, Grep]\ntools: [Write]\n---\nReview.")
			writeTestFile(t, filepath.Join(root, "agents", "group", "flat.md"), "---\ndescription: Nested flat review\n---\nReview.")
			writeTestFile(t, filepath.Join(root, "agents", "group", "README.md"), "Author notes.")
			writeTestFile(t, filepath.Join(root, "agents", "scripts", "ignored.md"), "---\ndescription: Resource\n---\nIgnored.")
			writeTestFile(t, filepath.Join(root, "agents", "group", "review", "resources.md"), "---\ndescription: Profile resource\n---\nIgnored.")
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			agents := pkg.Inventory().Agents
			if len(agents) != 3 || pkg.AgentCount() != 3 || agents[0].Name != "flat" || agents[1].Name != "review" || agents[2].Name != "seed" {
				t.Fatalf("nested inventory=%+v, count=%d", agents, pkg.AgentCount())
			}
			if got := agents[1]; got.Path != filepath.Join(root, "agents", "group", "review", "SKILL.md") || got.Description != "Nested directory review" || got.Model != "custom-model" || !reflect.DeepEqual(got.AllowedTools, []string{"Read", "Grep"}) {
				t.Fatalf("nested metadata=%+v", got)
			}
		})
	}
}

func TestNestedAgentInventoryStopsInternalSymlinkCycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host privileges")
	}
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"nested-agents","contributes":{"agents":["agents"]}}`)
	writeTestFile(t, filepath.Join(root, "agents", "group", "review", "SKILL.md"), "---\ndescription: Nested review\n---\nReview.")
	if err := os.Symlink("..", filepath.Join(root, "agents", "group", "loop")); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if agents := pkg.Inventory().Agents; len(agents) != 1 || agents[0].Name != "review" {
		t.Fatalf("cycle inventory=%+v", agents)
	}
}

func TestDeclaredDirectoryAgentRuntimeNames(t *testing.T) {
	for _, tc := range []struct{ directory, declared, want string }{
		{"中文", "", "中文"},
		{"cafe\u0301", "", "café"},
		{"中文", "审查", "审查"},
		{"review", "审查", "review"},
		{"中文", "inspect", "inspect"},
	} {
		t.Run(tc.directory+"/"+tc.declared, func(t *testing.T) {
			root := testenv.TempDir(t)
			writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"directory-agents","contributes":{"agents":["agents"]}}`)
			writeTestFile(t, filepath.Join(root, "agents", "group", tc.directory, "SKILL.md"), "---\nname: "+tc.declared+"\ndescription: Directory profile\n---\nReview.")
			pkg, _, err := ParseDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if agents := pkg.Inventory().Agents; len(agents) != 1 || agents[0].Name != tc.want || agents[0].Invocation != "/"+tc.want {
				t.Fatalf("agents=%+v, want %q", agents, tc.want)
			}
		})
	}
}

func TestAgentInventoryChecksResolvedResidency(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires host privileges")
	}
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"directory-agents","contributes":{"agents":["agents"]}}`)
	writeTestFile(t, filepath.Join(root, "profiles", "resident.md"), "---\ndescription: Resident fixture\n---\nReview.")
	writeTestFile(t, filepath.Join(outside, "profile.md"), "---\ndescription: Nonresident fixture\n---\nReview.")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, target string }{
		{"resident", filepath.Join("..", "..", "profiles", "resident.md")},
		{"absolute-resident", filepath.Join(root, "profiles", "resident.md")},
		{"nonresident", filepath.Join(outside, "profile.md")},
		{"nonregular", filepath.Join("..", "..", "empty")},
		{"broken", filepath.Join("..", "..", "missing.md")},
	} {
		path := filepath.Join(root, "agents", tc.name, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if tc.name == "nonresident" {
			var err error
			tc.target, err = filepath.Rel(filepath.Dir(path), tc.target)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(tc.target, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "agents", "nonresident-directory")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "profile.md"), filepath.Join(root, "agents", "nonresident-flat.md")); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if agents := pkg.Inventory().Agents; len(agents) != 1 || agents[0].Name != "resident" {
		t.Fatalf("resident-only inventory=%+v", agents)
	}
}

func TestAgentDirectoryScanMatchesDefaultRuntimeDepth(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"bounded-agents","contributes":{"agents":["agents"]}}`)
	for _, rel := range []string{"agents/top.md", "agents/group/empty.md", "agents/group/child/limit.md", "agents/group/child/deeper/ignored.md"} {
		body := "---\ndescription: Depth fixture\n---\nBODY"
		if filepath.Base(rel) == "empty.md" {
			body = "No description."
		}
		writeTestFile(t, filepath.Join(root, rel), body)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if agents := pkg.Inventory().Agents; len(agents) != 2 || agents[0].Name != "limit" || agents[1].Name != "top" {
		t.Fatalf("default-depth inventory=%+v", agents)
	}
}

func TestDirectoryAgentRefsUseBorrowedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("moving an open root requires POSIX directory semantics")
	}
	parent := testenv.TempDir(t)
	rootPath := filepath.Join(parent, "package")
	writeTestFile(t, filepath.Join(rootPath, "agents", "original", "SKILL.md"), "---\ndescription: Original profile\n---\nBODY")
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(rootPath, filepath.Join(parent, "moved")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(rootPath, "agents", "replacement", "SKILL.md"), "---\ndescription: Replacement profile\n---\nBODY")
	for range 2 {
		agents := loadAgentRefs(root, "agents")
		if len(agents) != 1 || agents[0].Name != "original" || agents[0].Description != "Original profile" {
			t.Fatalf("borrowed-root directory inventory=%+v", agents)
		}
	}
}

func TestDirectoryAgentSourcesRemainBounded(t *testing.T) {
	root := testenv.TempDir(t)
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"apiVersion":"reasonix.io/plugin/v2","name":"bounded-agents","contributes":{"agents":["agents"]}}`)
	writeTestFile(t, filepath.Join(root, "agents", "small", "SKILL.md"), "---\ndescription: Ordinary profile\n---\nBODY")
	writeTestFile(t, filepath.Join(root, "agents", "large", "SKILL.md"), strings.Repeat("x", maxAgentSourceBytes+1))
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if agents := pkg.Inventory().Agents; len(agents) != 1 || agents[0].Name != "small" {
		t.Fatalf("bounded directory sources=%+v", agents)
	}
}
