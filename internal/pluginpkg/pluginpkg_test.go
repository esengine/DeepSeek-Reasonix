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

func TestParseClaudeHooksKeepsDistinctEnvTimeoutAsyncCwd(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "hook-pack"}`)
	// Same event/matcher/command/args, but each block differs in exactly one
	// of env, timeout, async, cwd — none should be dropped as a duplicate of
	// another.
	writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), `{
  "hooks": {"PreToolUse": [
    {"matcher": "bash", "hooks": [{"type":"command","command":"bin/guard","env":{"MODE":"a"}}]},
    {"matcher": "bash", "hooks": [{"type":"command","command":"bin/guard","env":{"MODE":"b"}}]},
    {"matcher": "bash", "hooks": [{"type":"command","command":"bin/guard","env":{"MODE":"b"},"timeout":5}]},
    {"matcher": "bash", "hooks": [{"type":"command","command":"bin/guard","env":{"MODE":"b"},"timeout":5,"async":true}]},
    {"matcher": "bash", "hooks": [{"type":"command","command":"bin/guard","env":{"MODE":"b"},"timeout":5,"async":true}]}
  ]}
}`)

	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	hooks := pkg.Manifest.Hooks["PreToolUse"]
	// Four distinct configurations; the fifth block is an exact duplicate of
	// the fourth (same env, timeout, and async) and must still be dropped.
	if len(hooks) != 4 {
		t.Fatalf("hooks = %#v, want 4 distinct configurations (dedup must not collapse different env/timeout/async)", hooks)
	}
}

func TestParseClaudeHooksWarnOnUnsupportedSemantics(t *testing.T) {
	cases := []struct {
		name      string
		hooksJSON string
		wantSub   string
	}{
		{
			name:      "conditional-if-runs-unconditionally",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bin/guard","if":"Bash(git *)"}]}]}}`,
			wantSub:   `does not evaluate`,
		},
		{
			name:      "asyncRewake-not-supported",
			hooksJSON: `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"bin/watch","asyncRewake":true}]}]}}`,
			wantSub:   `asyncRewake`,
		},
		{
			name:      "stop-cannot-block",
			hooksJSON: `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"bin/gate"}]}]}}`,
			wantSub:   `cannot block the turn`,
		},
		{
			name:      "subagentstop-cannot-block",
			hooksJSON: `{"hooks":{"SubagentStop":[{"hooks":[{"type":"command","command":"bin/gate"}]}]}}`,
			wantSub:   `cannot block the turn`,
		},
		{
			name:      "matcher-names-unsupported-tool",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"WebSearch","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `will never fire`,
		},
		{
			name:      "matcher-alternation-all-unsupported",
			hooksJSON: `{"hooks":{"PermissionRequest":[{"matcher":"ExitPlanMode|EnterPlanMode","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `will never fire`,
		},
		{
			name:      "webfetch-required-prompt-is-unavailable",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"WebFetch","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `required "prompt"`,
		},
		{
			name:      "mixed-matcher-includes-webfetch",
			hooksJSON: `{"hooks":{"PostToolUse":[{"matcher":"Bash|WebFetch","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `required "prompt"`,
		},
		{
			name:      "wildcard-matcher-includes-webfetch",
			hooksJSON: `{"hooks":{"PermissionRequest":[{"matcher":"*","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `required "prompt"`,
		},
		{
			name:      "empty-matcher-includes-webfetch",
			hooksJSON: `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `required "prompt"`,
		},
		{
			name:      "regex-matcher-includes-webfetch",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"Web(Fetch|Search)","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `required "prompt"`,
		},
		{
			name:      "notebook-cell-number-has-no-claude-equivalent",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"NotebookEdit","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
			wantSub:   `cell_number`,
		},
		{
			name:      "task-output-may-cover-multiple-jobs",
			hooksJSON: `{"hooks":{"PostToolUse":[{"matcher":"TaskOutput","hooks":[{"type":"command","command":"bin/watch"}]}]}}`,
			wantSub:   `multiple or all background jobs`,
		},
		{
			name:      "legacy-bash-output-may-cover-multiple-jobs",
			hooksJSON: `{"hooks":{"PostToolUse":[{"matcher":"BashOutput","hooks":[{"type":"command","command":"bin/watch"}]}]}}`,
			wantSub:   `multiple or all background jobs`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "hook-pack"}`)
			writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), c.hooksJSON)

			pkg, warnings, err := ParseDir(root)
			if err != nil {
				t.Fatalf("ParseDir: %v", err)
			}
			if pkg.Compatibility.Status != "partial" {
				t.Fatalf("compatibility status = %q, want partial (unsupported semantics must not claim full compatibility)", pkg.Compatibility.Status)
			}
			found := false
			for _, w := range warnings {
				if strings.Contains(w, c.wantSub) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("warnings = %v, want one containing %q", warnings, c.wantSub)
			}
			// The hook is still imported best-effort — dropping it entirely
			// could remove a plugin's only safety hook.
			if pkg.Manifest.Hooks == nil {
				t.Fatal("hook should still be imported despite the unsupported semantics")
			}
		})
	}
}

// TestParseClaudeHooksReportsStructuralGapsOncePerFile pins the noise bound:
// a plugin with several wildcard hooks reports each structural input gap
// (WebFetch prompt, NotebookEdit cell_number, TaskOutput multi-job) once per
// hooks file, not once per hook item.
func TestParseClaudeHooksReportsStructuralGapsOncePerFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "hook-pack"}`)
	writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), `{"hooks":{
  "PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"bin/a"},{"type":"command","command":"bin/b"}]}],
  "PostToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"bin/c"}]}]
}}`)

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if pkg.Compatibility.Status != "partial" {
		t.Fatalf("compatibility status = %q, want partial", pkg.Compatibility.Status)
	}
	gapSubs := []string{`required "prompt"`, "cell_number", "multiple or all background jobs"}
	for _, sub := range gapSubs {
		warned := 0
		for _, w := range warnings {
			if strings.Contains(w, sub) {
				warned++
			}
		}
		if warned != 1 {
			t.Errorf("warnings mentioning %q = %d, want exactly 1 per hooks file (got %v)", sub, warned, warnings)
		}
		skipped := 0
		for _, issue := range pkg.Compatibility.Skipped {
			if strings.Contains(issue.Reason, sub) {
				skipped++
			}
		}
		if skipped != 1 {
			t.Errorf("compatibility issues mentioning %q = %d, want exactly 1 per hooks file", sub, skipped)
		}
	}
}

func TestParseClaudeHooksDoesNotWarnOnMatchersThatCanFire(t *testing.T) {
	cases := []struct {
		name      string
		hooksJSON string
	}{
		{
			name:      "supported-tool-name",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
		},
		{
			// A partly-unsupported alternation can still fire for Bash calls,
			// so it must not be flagged as dead.
			name:      "mixed-alternation-still-fires",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"Bash|WebSearch","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
		},
		{
			// A regex beyond a plain "|" alternation isn't evaluated, to
			// avoid guessing wrong and producing a false positive.
			name:      "complex-regex-not-evaluated",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"WebSearch.*","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
		},
		{
			// A previously-unmapped Reasonix tool the fix now supports.
			name:      "run-skill-now-mapped",
			hooksJSON: `{"hooks":{"PreToolUse":[{"matcher":"Skill","hooks":[{"type":"command","command":"bin/guard"}]}]}}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "hook-pack"}`)
			writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), c.hooksJSON)

			pkg, warnings, err := ParseDir(root)
			if err != nil {
				t.Fatalf("ParseDir: %v", err)
			}
			for _, w := range warnings {
				if strings.Contains(w, "will never fire") {
					t.Fatalf("warnings = %v, want no dead-matcher warning", warnings)
				}
			}
			if pkg.Compatibility.Status != "full" {
				t.Fatalf("compatibility status = %q, want full", pkg.Compatibility.Status)
			}
		})
	}
}

func TestParseClaudePluginMapsConventionCapabilities(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, ClaudeManifest), `{"name": "big-pack"}`)
	writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeTestFile(t, filepath.Join(root, "commands", "deploy.md"), "run deploy")
	writeTestFile(t, filepath.Join(root, "agents", "reviewer.md"), "---\nname: reviewer\ndescription: review changes\nmodel: sonnet\ntools: [Read, Grep]\n---\nReview carefully.")
	writeTestFile(t, filepath.Join(root, "hooks", "hooks.json"), `{
  "hooks": {"SessionStart": [{"hooks": [{"type":"command","command":"bin/start","args":["--hook"],"async":true}]}]}
}`)
	writeTestFile(t, filepath.Join(root, ".mcp.json"), `{
  "mcpServers": {"Google Drive": {"type":"local","command":"uvx","args":["drive-mcp"],"title":"Drive"}}
}`)

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want fully mapped package", warnings)
	}
	if pkg.Compatibility.Status != "full" || pkg.AgentCount() != 1 {
		t.Fatalf("compatibility = %+v agents=%d", pkg.Compatibility, pkg.AgentCount())
	}
	agent := pkg.Inventory().Agents[0]
	if agent.Name != "reviewer" || agent.Model != "sonnet" || strings.Join(agent.AllowedTools, ",") != "Read,Grep" {
		t.Fatalf("agent = %+v", agent)
	}
	hook := pkg.Manifest.Hooks["SessionStart"][0]
	if !hook.Async || hook.PayloadFormat != "claude" || strings.Join(hook.Args, ",") != "--hook" {
		t.Fatalf("hook = %+v", hook)
	}
	if len(pkg.Manifest.MCPServers) != 1 {
		t.Fatalf("MCP servers = %+v", pkg.Manifest.MCPServers)
	}
	for name, server := range pkg.Manifest.MCPServers {
		if !IsValidName(name) || server.Type != "stdio" || server.DisplayName != "Drive" || server.AutoStart == nil || *server.AutoStart {
			t.Fatalf("MCP %q = %+v", name, server)
		}
	}
}

func TestClaudeMCPServerIDUsesConnectionIdentityAndPreservesValidNames(t *testing.T) {
	identity := claudeMCPIdentity{Type: "http", URL: "https://open.feishu.cn/mcp"}
	if got := claudeMCPServerID("yuandian", identity); got != "yuandian" {
		t.Fatalf("valid MCP ID changed to %q", got)
	}
	first := claudeMCPServerID("飞书", identity)
	second := claudeMCPServerID("飞书", identity)
	if first != second || !IsValidName(first) {
		t.Fatalf("stable MCP IDs = %q / %q", first, second)
	}
	different := claudeMCPServerID("飞书", claudeMCPIdentity{Type: "http", URL: "https://example.com/other"})
	if different == first {
		t.Fatalf("different endpoints shared MCP ID %q", first)
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
