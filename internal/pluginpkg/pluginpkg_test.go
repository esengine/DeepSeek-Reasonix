package pluginpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexSuperpowersManifest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, CodexManifest), `{
	  "name": "superpowers",
	  "version": "6.1.0",
	  "description": "Planning workflows",
	  "skills": "./skills/"
	}`)
	writeTestFile(t, filepath.Join(root, "skills", "plan", "SKILL.md"), "---\ndescription: Plan work\n---\nbody")
	writeTestFile(t, filepath.Join(root, "hooks", "session-start-codex"), "#!/usr/bin/env bash\n")

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if pkg.ManifestKind != "codex" || pkg.Manifest.Name != "superpowers" || pkg.Manifest.Version != "6.1.0" {
		t.Fatalf("pkg = %+v", pkg)
	}
	if got := pkg.SkillRoots(); len(got) != 1 || got[0] != filepath.Join(root, "skills") {
		t.Fatalf("SkillRoots = %#v", got)
	}
	if hooks := pkg.Manifest.Hooks["SessionStart"]; len(hooks) != 1 || hooks[0].Command != filepath.Join(root, "hooks", "session-start-codex") {
		t.Fatalf("SessionStart hooks = %+v", hooks)
	}
	inv := pkg.Inventory()
	if len(inv.Skills) != 1 || inv.Skills[0].Name != "plan" || inv.Skills[0].Invocation != "/plan" {
		t.Fatalf("Inventory().Skills = %+v", inv.Skills)
	}
	if skills, hooks, _ := pkg.CapabilityCounts(); skills != 1 || hooks != 1 {
		t.Fatalf("CapabilityCounts skills=%d hooks=%d", skills, hooks)
	}
}

func TestParseCodexClaudeCompatibility(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, CodexManifest), `{
	  "name": "claude-pack",
	  "version": "1.0.0",
	  "skills": "skills"
	}`)
	writeTestFile(t, filepath.Join(root, "CLAUDE.md"), "Always use the bundled workflow.")
	writeTestFile(t, filepath.Join(root, ".claude", "settings.json"), `{
	  "hooks": {
	    "PostToolUse": [
	      {
	        "matcher": "bash|write_file",
	        "hooks": [
	          {
	            "type": "command",
	            "command": "node hooks/post-tool.js",
	            "description": "post tool check",
	            "timeout": 3,
	            "env": { "MODE": "check" }
	          },
	          { "type": "prompt", "command": "ignored" }
	        ]
	      }
	    ],
	    "UserPromptSubmit": [
	      {
	        "hooks": [
	          { "type": "command", "command": "node hooks/prompt.js" }
	        ]
	      }
	    ]
	  }
	}`)

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 1 || warnings[0] == "" {
		t.Fatalf("warnings = %v, want unsupported hook warning", warnings)
	}
	if got := pkg.Manifest.Hooks["SessionStart"]; len(got) != 1 || got[0].ContextFile != "CLAUDE.md" {
		t.Fatalf("SessionStart hooks = %+v, want CLAUDE.md context hook", got)
	}
	if got := pkg.Manifest.Hooks["PostToolUse"]; len(got) != 1 || got[0].Match != "bash|write_file" || got[0].Command != "node hooks/post-tool.js" || got[0].Timeout != 3000 || got[0].Env["MODE"] != "check" {
		t.Fatalf("PostToolUse hooks = %+v", got)
	}
	if got := pkg.Manifest.Hooks["UserPromptSubmit"]; len(got) != 1 || got[0].Command != "node hooks/prompt.js" {
		t.Fatalf("UserPromptSubmit hooks = %+v", got)
	}
}

func TestParseCodexWithoutSessionStartHookDoesNotWarn(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, CodexManifest), `{
	  "name": "skills-only",
	  "skills": "skills"
	}`)

	_, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

func TestRejectsEscapingSkillPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, NativeManifest), `{
	  "name": "bad",
	  "skills": "../skills"
	}`)
	if _, _, err := ParseDir(root); err == nil {
		t.Fatal("ParseDir should reject escaping skill path")
	}
}

func TestStateRoundTripSortsPlugins(t *testing.T) {
	home := t.TempDir()
	if err := Upsert(home, InstalledPlugin{Name: "zeta", Root: "plugins/zeta", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := Upsert(home, InstalledPlugin{Name: "alpha", Root: "plugins/alpha", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Plugins) != 2 || st.Plugins[0].Name != "alpha" || st.Plugins[1].Name != "zeta" {
		t.Fatalf("state plugins = %+v", st.Plugins)
	}
}

func TestInstalledTextDescribesUsageInventory(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "plugins", "superpowers")
	writeTestFile(t, filepath.Join(root, CodexManifest), `{
	  "name": "superpowers",
	  "version": "6.1.0",
	  "description": "Planning workflows",
	  "skills": "skills"
	}`)
	writeTestFile(t, filepath.Join(root, "skills", "plan", "SKILL.md"), "---\ndescription: Plan work\nrunAs: subagent\n---\nbody")
	writeTestFile(t, filepath.Join(root, "hooks", "session-start-codex"), "#!/usr/bin/env bash\n")
	if err := Upsert(home, InstalledPlugin{Name: "superpowers", Root: "plugins/superpowers", Version: "6.1.0", Description: "Planning workflows", ManifestKind: "codex", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	list, err := InstalledListText(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"plugins (1):", "superpowers [enabled]", "1 skills / 1 hooks", "/plugins show <name>"} {
		if !strings.Contains(list, want) {
			t.Fatalf("InstalledListText missing %q:\n%s", want, list)
		}
	}
	details, err := InstalledShowText(home, "superpowers")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"plugin superpowers [enabled]", "usage: enabled plugins load into new sessions", "/plan [subagent] - Plan work", "SessionStart"} {
		if !strings.Contains(details, want) {
			t.Fatalf("InstalledShowText missing %q:\n%s", want, details)
		}
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
