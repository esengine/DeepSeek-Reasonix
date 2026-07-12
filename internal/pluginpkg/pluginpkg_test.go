package pluginpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
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
	if skills, _, hooks, _ := pkg.CapabilityCounts(); skills != 1 || hooks != 1 {
		t.Fatalf("CapabilityCounts skills=%d hooks=%d", skills, hooks)
	}
}

func TestParseDirDecodesGB18030Manifest(t *testing.T) {
	root := t.TempDir()
	manifest := `{"name":"cn-plugin","version":"1.0.0","description":"中文插件"}`
	path := filepath.Join(root, NativeManifest)
	if err := os.WriteFile(path, fileencoding.Encode(manifest, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if pkg.Manifest.Description != "中文插件" {
		t.Fatalf("decoded manifest = %+v", pkg.Manifest)
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

func TestParseClaudePluginManifest(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{
	  "name": "ui-ux-pro-max",
	  "version": "2.6.2",
	  "description": "UI/UX design intelligence",
	  "skills": "./.claude/skills/"
	}`)
	writeTestFile(t, filepath.Join(root, ".claude", "skills", "ui-ux-pro-max", "SKILL.md"), "---\ndescription: UI design helper\n---\nbody")
	writeTestFile(t, filepath.Join(root, "CLAUDE.md"), "Use the bundled UI workflow.")

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if pkg.ManifestKind != "claude" || pkg.Manifest.Name != "ui-ux-pro-max" || pkg.Manifest.Version != "2.6.2" {
		t.Fatalf("pkg = %+v", pkg)
	}
	if got := pkg.SkillRoots(); len(got) != 1 || got[0] != filepath.Join(root, ".claude", "skills") {
		t.Fatalf("SkillRoots = %#v", got)
	}
	inv := pkg.Inventory()
	if len(inv.Skills) != 1 || inv.Skills[0].Name != "ui-ux-pro-max" || inv.Skills[0].Invocation != "/ui-ux-pro-max" {
		t.Fatalf("Inventory().Skills = %+v", inv.Skills)
	}
	if hooks := pkg.Manifest.Hooks["SessionStart"]; len(hooks) != 1 || hooks[0].ContextFile != "CLAUDE.md" {
		t.Fatalf("SessionStart hooks = %+v, want CLAUDE.md context hook", hooks)
	}
	if ManifestPath(pkg.ManifestKind) != ClaudeManifest {
		t.Fatalf("ManifestPath(%q) = %q, want %q", pkg.ManifestKind, ManifestPath(pkg.ManifestKind), ClaudeManifest)
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
	for _, want := range []string{"plugin superpowers [enabled]", "usage: enabled plugins load into new sessions", "/superpowers:plan [subagent] - Plan work", "SessionStart"} {
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

// TestParseClaudePluginConventionSkillDirs pins the standard Claude plugin
// shape: plugin.json carries metadata only, and skills live in the
// conventional skills/ directory that Claude auto-discovers. Without the
// fallback such a package installed as zero capabilities with no warning.
func TestParseClaudePluginConventionSkillDirs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{
	  "name": "design-pack",
	  "version": "1.0.0",
	  "description": "metadata-only manifest"
	}`)
	writeTestFile(t, filepath.Join(root, "skills", "design-review", "SKILL.md"), "---\ndescription: review designs\n---\nbody")

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if pkg.ManifestKind != "claude" {
		t.Fatalf("kind = %q", pkg.ManifestKind)
	}
	if got := pkg.SkillRoots(); len(got) != 1 || got[0] != filepath.Join(root, "skills") {
		t.Fatalf("SkillRoots = %#v, want conventional skills dir", got)
	}
	if inv := pkg.Inventory(); len(inv.Skills) != 1 || inv.Skills[0].Name != "design-review" {
		t.Fatalf("Inventory().Skills = %+v", inv.Skills)
	}
}

func TestParseClaudePluginDotClaudeConventionDir(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "pack"}`)
	writeTestFile(t, filepath.Join(root, ".claude", "skills", "helper", "SKILL.md"), "---\ndescription: helper\n---\nbody")

	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if got := pkg.SkillRoots(); len(got) != 1 || got[0] != filepath.Join(root, ".claude", "skills") {
		t.Fatalf("SkillRoots = %#v, want .claude/skills", got)
	}
}

func TestParseClaudePluginIgnoresEmptyConventionDirAndExplicitSkillsWin(t *testing.T) {
	root := t.TempDir()
	// Empty conventional dir (no SKILL.md inside) must not be adopted.
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "empty-pack"}`)
	if err := os.MkdirAll(filepath.Join(root, "skills", "stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if got := pkg.SkillRoots(); len(got) != 0 {
		t.Fatalf("SkillRoots = %#v, want none for a skill-less conventional dir", got)
	}

	// Explicit skills declaration disables the fallback entirely.
	root2 := t.TempDir()
	writeTestFile(t, filepath.Join(root2, ClaudeManifest), `{"name": "explicit-pack", "skills": "./custom/"}`)
	writeTestFile(t, filepath.Join(root2, "custom", "one", "SKILL.md"), "---\ndescription: one\n---\nbody")
	writeTestFile(t, filepath.Join(root2, "skills", "two", "SKILL.md"), "---\ndescription: two\n---\nbody")
	pkg2, _, err := ParseDir(root2)
	if err != nil {
		t.Fatalf("ParseDir explicit: %v", err)
	}
	if got := pkg2.SkillRoots(); len(got) != 1 || got[0] != filepath.Join(root2, "custom") {
		t.Fatalf("SkillRoots = %#v, want only the declared custom dir", got)
	}
}

func TestParseClaudePluginWarnsOnUnmappedCapabilities(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "big-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeTestFile(t, filepath.Join(root, "commands", "deploy.md"), "run deploy")
	writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), `{}`)
	writeTestFile(t, filepath.Join(root, ".mcp.json"), `{}`)

	_, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"hooks/hooks.json", ".mcp.json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings = %v, want mention of %s", warnings, want)
		}
	}
	// commands map to custom slash commands now — they must not warn.
	if strings.Contains(joined, "commands") {
		t.Fatalf("warnings = %v, must not warn about the mapped commands dir", warnings)
	}
	if strings.Contains(joined, "agents") {
		t.Fatalf("warnings = %v, must not mention absent agents dir", warnings)
	}
}

// TestParseClaudePluginMapsCommandsDir pins the commands mapping: a Claude
// plugin's conventional commands/ dir becomes a Manifest.Commands root — even
// when the manifest declares skills explicitly — and its flat <name>.md
// templates surface in the inventory as /<name> invocations.
func TestParseClaudePluginMapsCommandsDir(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "pwf-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "planner", "SKILL.md"), "---\ndescription: planner skill\n---\nbody")
	writeTestFile(t, filepath.Join(root, "commands", "plan.md"), "---\ndescription: \"Start planning\"\nargument-hint: \"[task]\"\n---\nPlan: $ARGUMENTS")
	writeTestFile(t, filepath.Join(root, "commands", "status.md"), "Show status")

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for a fully mapped plugin", warnings)
	}
	if got := pkg.CommandRoots(); len(got) != 1 || got[0] != filepath.Join(root, "commands") {
		t.Fatalf("CommandRoots = %#v, want the conventional commands dir", got)
	}
	inv := pkg.Inventory()
	if len(inv.Commands) != 2 {
		t.Fatalf("inventory commands = %#v, want plan and status", inv.Commands)
	}
	byName := map[string]CommandRef{}
	for _, c := range inv.Commands {
		byName[c.Name] = c
	}
	plan, ok := byName["plan"]
	if !ok || plan.Invocation != "/plan" || plan.Description != "Start planning" || plan.ArgHint != "[task]" {
		t.Fatalf("plan command = %+v, want /plan with description and arg hint", plan)
	}
	if _, ok := byName["status"]; !ok {
		t.Fatalf("inventory commands = %#v, want frontmatter-less status command included", inv.Commands)
	}
	skills, commands, hooks, mcp := pkg.CapabilityCounts()
	if skills != 1 || commands != 2 || hooks != 0 || mcp != 0 {
		t.Fatalf("CapabilityCounts = %d skills %d commands %d hooks %d mcp, want 1/2/0/0", skills, commands, hooks, mcp)
	}

	// Explicit skills declaration must not disable command adoption.
	root2 := t.TempDir()
	writeTestFile(t, filepath.Join(root2, ClaudeManifest), `{"name": "explicit-pack", "skills": "./custom/"}`)
	writeTestFile(t, filepath.Join(root2, "custom", "one", "SKILL.md"), "---\ndescription: one\n---\nbody")
	writeTestFile(t, filepath.Join(root2, "commands", "go.md"), "go")
	pkg2, _, err := ParseDir(root2)
	if err != nil {
		t.Fatalf("ParseDir explicit: %v", err)
	}
	if got := pkg2.CommandRoots(); len(got) != 1 || got[0] != filepath.Join(root2, "commands") {
		t.Fatalf("CommandRoots = %#v, want commands adopted alongside explicit skills", got)
	}

	// A docs-only commands dir (no installable <name>.md) is not adopted.
	root3 := t.TempDir()
	writeTestFile(t, filepath.Join(root3, ClaudeManifest), `{"name": "docs-pack"}`)
	writeTestFile(t, filepath.Join(root3, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeTestFile(t, filepath.Join(root3, "commands", "notes.txt"), "not a command")
	pkg3, _, err := ParseDir(root3)
	if err != nil {
		t.Fatalf("ParseDir docs-only: %v", err)
	}
	if got := pkg3.CommandRoots(); len(got) != 0 {
		t.Fatalf("CommandRoots = %#v, want none for a commands dir without .md files", got)
	}
}

// TestNativeManifestCommandsField pins the explicit "commands" declaration in
// reasonix-plugin.json, including path validation.
func TestNativeManifestCommandsField(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, NativeManifest), `{"name": "native-pack", "commands": ["cmds"]}`)
	writeTestFile(t, filepath.Join(root, "cmds", "ship.md"), "---\ndescription: ship it\n---\nShip $1")

	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if got := pkg.CommandRoots(); len(got) != 1 || got[0] != filepath.Join(root, "cmds") {
		t.Fatalf("CommandRoots = %#v, want declared cmds dir", got)
	}
	inv := pkg.Inventory()
	if len(inv.Commands) != 1 || inv.Commands[0].Name != "ship" {
		t.Fatalf("inventory commands = %#v, want ship", inv.Commands)
	}

	rootBad := t.TempDir()
	writeTestFile(t, filepath.Join(rootBad, NativeManifest), `{"name": "bad-pack", "commands": ["../escape"]}`)
	if _, _, err := ParseDir(rootBad); err == nil {
		t.Fatal("ParseDir must reject a commands path escaping the plugin root")
	}
}

// TestParseClaudePluginDoesNotRegisterCodexSessionStartHook pins the security
// boundary of the includeCodexSessionStartHook flag: a claude-kind package
// shipping a hooks/session-start-codex file must NOT get it registered as an
// executable SessionStart hook (that convention belongs to codex manifests).
func TestParseClaudePluginDoesNotRegisterCodexSessionStartHook(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "sneaky-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeTestFile(t, filepath.Join(root, "hooks", "session-start-codex"), "#!/bin/sh\necho pwned\n")

	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	for _, h := range pkg.Manifest.Hooks["SessionStart"] {
		if h.Command != "" {
			t.Fatalf("claude package registered executable SessionStart hook: %+v", h)
		}
	}
}

// TestParseCodexManifestNotAffectedByClaudeFallback: the convention-dir
// fallback is claude-only; a codex manifest without a skills field keeps its
// existing "no skills" behavior even when a skills/ directory exists.
func TestParseCodexManifestNotAffectedByClaudeFallback(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, CodexManifest), `{"name": "codex-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")

	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if pkg.ManifestKind != "codex" {
		t.Fatalf("kind = %q", pkg.ManifestKind)
	}
	if got := pkg.SkillRoots(); len(got) != 0 {
		t.Fatalf("SkillRoots = %#v, codex parsing must not adopt convention dirs", got)
	}
}

// TestParseClaudePluginAdoptsNestedCommands pins that namespace layouts like
// commands/git/commit.md — which the runtime loader walks — also gate command
// root adoption, and surface in the inventory under their namespaced name.
func TestParseClaudePluginAdoptsNestedCommands(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "nested-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeTestFile(t, filepath.Join(root, "commands", "git", "commit.md"), "---\ndescription: commit helper\n---\nCommit: $ARGUMENTS")

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if got := pkg.CommandRoots(); len(got) != 1 || got[0] != filepath.Join(root, "commands") {
		t.Fatalf("CommandRoots = %#v, want commands adopted for nested-only layout", got)
	}
	inv := pkg.Inventory()
	if len(inv.Commands) != 1 || inv.Commands[0].Name != "git:commit" || inv.Commands[0].Invocation != "/git:commit" {
		t.Fatalf("inventory commands = %#v, want namespaced git:commit", inv.Commands)
	}
}

// TestInventoryTextCommandsOnly pins that a commands-only inventory does not
// also claim "no detailed inventory available".
func TestInventoryTextCommandsOnly(t *testing.T) {
	var b strings.Builder
	appendInventoryText(&b, "superpowers", Inventory{Commands: []CommandRef{{Name: "plan", Invocation: "/plan", Description: "plan things"}}})
	out := b.String()
	if !strings.Contains(out, "commands:") || !strings.Contains(out, "/superpowers:plan") {
		t.Fatalf("output = %q, want the commands listing", out)
	}
	if strings.Contains(out, "no detailed inventory available") {
		t.Fatalf("output = %q, must not claim an empty inventory after listing commands", out)
	}
}

// TestParseClaudePluginAdoptsDeeplyNestedCommands pins that adoption gating
// shares the runtime loader's discovery semantics with no depth ceiling: a
// plugin whose only command sits six levels deep is still adopted.
func TestParseClaudePluginAdoptsDeeplyNestedCommands(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "deep-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeTestFile(t, filepath.Join(root, "commands", "a", "b", "c", "d", "e", "commit.md"), "---\ndescription: deep commit\n---\nCommit")

	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if got := pkg.CommandRoots(); len(got) != 1 || got[0] != filepath.Join(root, "commands") {
		t.Fatalf("CommandRoots = %#v, want commands adopted for the deeply nested layout", got)
	}
	inv := pkg.Inventory()
	if len(inv.Commands) != 1 || inv.Commands[0].Name != "a:b:c:d:e:commit" {
		t.Fatalf("inventory commands = %#v, want the namespaced deep command", inv.Commands)
	}
}
