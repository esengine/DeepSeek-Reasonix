package pluginpkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// jsonText marshals v for manifest fixture construction in tests.
func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// writeV2Plugin writes a Manifest v2 plugin tree: the manifest plus the
// on-disk assets its paths refer to.
func writeV2Plugin(t *testing.T, root, manifest string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, NativeManifest), manifest)
}

// v2ExampleManifest is the canonical Manifest v2 example: contributes with
// every path-list kind plus a full runtime declaration.
const v2ExampleManifest = `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "example",
  "version": "1.0.0",
  "description": "Example v2 plugin",
  "contributes": {
    "skills": ["skills"],
    "agents": ["agents"],
    "commands": ["commands"],
    "prompts": ["prompts"],
    "themes": ["themes/*.reasonix-theme"]
  },
  "runtime": {
    "command": "${REASONIX_PLUGIN_ROOT}/bin/example",
    "args": ["--serve"],
    "env": {"MODE": "plugin"},
    "required": true,
    "priority": 10,
    "intercepts": ["input.receive", "tool.before"],
    "replaces": ["system_prompt"],
    "capabilities": ["interceptors", "strategies", "providers", "ui"]
  }
}`

// writeV2ExampleAssets materializes every path the example manifest declares.
func writeV2ExampleAssets(t *testing.T, root string) {
	t.Helper()
	writeTestFile(t, filepath.Join(root, "skills", "demo", "SKILL.md"), "---\ndescription: demo skill\n---\nDemo")
	writeTestFile(t, filepath.Join(root, "agents", "reviewer.md"), "---\ndescription: reviews\n---\nReview")
	writeTestFile(t, filepath.Join(root, "commands", "ship.md"), "---\ndescription: ship\n---\nShip $ARGUMENTS")
	writeTestFile(t, filepath.Join(root, "prompts", "plan.md"), "---\ndescription: plan\n---\nPlan $ARGUMENTS")
	writeTestFile(t, filepath.Join(root, "themes", "neon.reasonix-theme"), "theme pack bytes")
	writeTestFile(t, filepath.Join(root, "bin", "example"), "#!/bin/sh\n")
}

func TestManifestV2FullParse(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, v2ExampleManifest)
	writeV2ExampleAssets(t, root)

	pkg, warnings, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir v2 manifest: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if pkg.ManifestKind != "reasonix" {
		t.Fatalf("ManifestKind = %q, want reasonix", pkg.ManifestKind)
	}
	m := pkg.Manifest
	if m.Name != "example" || m.Version != "1.0.0" || m.Description != "Example v2 plugin" {
		t.Fatalf("identity fields: %+v", m)
	}
	if !reflect.DeepEqual(m.Skills, []string{"skills"}) {
		t.Fatalf("Skills = %#v", m.Skills)
	}
	if !reflect.DeepEqual(m.Agents, []string{"agents"}) {
		t.Fatalf("Agents = %#v", m.Agents)
	}
	if !reflect.DeepEqual(m.Commands, []string{"commands"}) {
		t.Fatalf("Commands = %#v", m.Commands)
	}
	if !reflect.DeepEqual(m.Prompts, []string{"prompts"}) {
		t.Fatalf("Prompts = %#v", m.Prompts)
	}
	if !reflect.DeepEqual(m.Themes, []string{"themes/*.reasonix-theme"}) {
		t.Fatalf("Themes = %#v", m.Themes)
	}
	rt := m.Runtime
	if rt == nil {
		t.Fatal("Runtime is nil, want the declared runtime spec")
	}
	if rt.Command != "${REASONIX_PLUGIN_ROOT}/bin/example" {
		t.Fatalf("Runtime.Command = %q, want the unexpanded ${REASONIX_PLUGIN_ROOT} form", rt.Command)
	}
	if !reflect.DeepEqual(rt.Args, []string{"--serve"}) || rt.Env["MODE"] != "plugin" {
		t.Fatalf("Runtime args/env: %+v", rt)
	}
	if !rt.Required || rt.Priority != 10 {
		t.Fatalf("Runtime required/priority: %+v", rt)
	}
	if !reflect.DeepEqual(rt.Intercepts, []string{"input.receive", "tool.before"}) {
		t.Fatalf("Runtime.Intercepts = %#v", rt.Intercepts)
	}
	if !reflect.DeepEqual(rt.Replaces, []string{"system_prompt"}) {
		t.Fatalf("Runtime.Replaces = %#v", rt.Replaces)
	}
	if !reflect.DeepEqual(rt.Capabilities, []string{"interceptors", "strategies", "providers", "ui"}) {
		t.Fatalf("Runtime.Capabilities = %#v", rt.Capabilities)
	}

	summary := pkg.CapabilitySummary()
	if summary.Skills != 1 || summary.Commands != 1 || summary.Prompts != 1 || summary.Themes != 1 || !summary.Runtime {
		t.Fatalf("CapabilitySummary = %+v", summary)
	}
	inv := pkg.Inventory()
	if len(inv.Prompts) != 1 || inv.Prompts[0].Name != "plan" || inv.Prompts[0].Description != "plan" {
		t.Fatalf("Inventory.Prompts = %+v", inv.Prompts)
	}
	if len(inv.Themes) != 1 || inv.Themes[0].Name != "neon" {
		t.Fatalf("Inventory.Themes = %+v", inv.Themes)
	}
}

func TestManifestV2RejectsUnknownFieldsWithPath(t *testing.T) {
	base := `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "strict-demo",
  "contributes": {"skills": ["skills"]},
  "hooks": {"PreToolUse": [{"match": "Bash", "command": "./pre.sh"}]},
  "mcpServers": {"srv": {"type": "stdio", "command": "./srv"}}
}`
	cases := []struct {
		name    string
		mutate  func(m map[string]any)
		wantErr string
	}{
		{"root field", func(m map[string]any) { m["nam"] = "typo" }, `unknown field "nam"`},
		{"contributes field", func(m map[string]any) {
			m["contributes"].(map[string]any)["promts"] = []string{"prompts"}
		}, `contributes: json: unknown field "promts"`},
		{"runtime field", func(m map[string]any) {
			m["runtime"] = map[string]any{"command": "./run", "intrcepts": []string{"input.receive"}}
		}, `runtime: json: unknown field "intrcepts"`},
		{"hook entry field", func(m map[string]any) {
			m["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["cmd"] = "./x"
		}, `hooks.PreToolUse[0]: json: unknown field "cmd"`},
		{"contributes hook entry field", func(m map[string]any) {
			m["contributes"].(map[string]any)["hooks"] = map[string]any{
				"SessionStart": []any{map[string]any{"command": "echo hi", "shellComand": true}},
			}
		}, `contributes.hooks.SessionStart[0]: json: unknown field "shellComand"`},
		{"mcp server entry field", func(m map[string]any) {
			m["mcpServers"].(map[string]any)["srv"].(map[string]any)["autoStart"] = false
		}, `mcpServers.srv: json: unknown field "autoStart"`},
		{"path list object field", func(m map[string]any) {
			m["contributes"].(map[string]any)["skills"] = []any{map[string]any{"paht": "skills"}}
		}, `contributes.skills[0]: json: unknown field "paht"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(base), &doc); err != nil {
				t.Fatal(err)
			}
			tc.mutate(doc)
			root := t.TempDir()
			writeV2Plugin(t, root, jsonText(doc))
			writeTestFile(t, filepath.Join(root, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nS")
			_, _, err := ParseDir(root)
			if err == nil {
				t.Fatalf("ParseDir succeeded, want unknown-field rejection")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestManifestAPIVersionV2Gating(t *testing.T) {
	cases := []struct {
		apiVersion any
		wantErr    string
	}{
		{"reasonix.io/plugin/v2", ""},
		{"reasonix.io/plugin/v1", `unsupported apiVersion`},
		{"reasonix.io/plugin/v0", `unsupported apiVersion`},
		{"reasonix.io/plugin/v10.2", `unsupported apiVersion`},
		{"v1", `unsupported apiVersion`},
		{"reasonix.io/plugin/vX", `unsupported apiVersion`},
		{"foo", `unsupported apiVersion`},
		{42, `apiVersion must be a string`},
		// Exact v2 only — v2.x minor aliases are rejected by design.
		{"reasonix.io/plugin/v2.1", `unsupported apiVersion`},
		{"reasonix.io/plugin/v2.0", `unsupported apiVersion`},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(strings.TrimSpace(jsonText(tc.apiVersion)), `"`, ""), func(t *testing.T) {
			root := t.TempDir()
			manifest := `{"apiVersion": ` + jsonText(tc.apiVersion) + `, "name": "ver-demo"}`
			writeV2Plugin(t, root, manifest)
			_, _, err := ParseDir(root)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseDir apiVersion %v: %v, want success", tc.apiVersion, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestManifestV2LegacyFieldsMergeWithContributes(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "merge-demo",
  "skills": ["legacy-skills", "shared"],
  "commands": "legacy-commands",
  "hooks": {
    "SessionStart": [{"command": "./start.sh", "description": "same everywhere"}]
  },
  "mcpServers": {
    "same": {"type": "stdio", "command": "./srv"},
    "legacy-only": {"url": "https://example.invalid/mcp"}
  },
  "contributes": {
    "skills": ["contrib-skills", "shared"],
    "commands": ["contrib-commands"],
    "prompts": ["prompts"],
    "hooks": {
      "SessionStart": [{"command": "./start.sh", "description": "same everywhere"}],
      "PreToolUse": [{"match": "Bash", "command": "./pre.sh"}]
    },
    "mcpServers": {
      "same": {"type": "stdio", "command": "./srv"},
      "contrib-only": {"type": "stdio", "command": "./other"}
    }
  }
}`)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir merged manifest: %v", err)
	}
	m := pkg.Manifest
	wantSkills := []string{"contrib-skills", "legacy-skills", "shared"}
	if !reflect.DeepEqual(m.Skills, wantSkills) {
		t.Fatalf("Skills = %#v, want unioned/deduped %#v", m.Skills, wantSkills)
	}
	wantCommands := []string{"contrib-commands", "legacy-commands"}
	if !reflect.DeepEqual(m.Commands, wantCommands) {
		t.Fatalf("Commands = %#v, want %#v", m.Commands, wantCommands)
	}
	if !reflect.DeepEqual(m.Prompts, []string{"prompts"}) {
		t.Fatalf("Prompts = %#v", m.Prompts)
	}
	// Identical hook entries dedupe across the two sources; distinct events union.
	if len(m.Hooks["SessionStart"]) != 1 {
		t.Fatalf("SessionStart hooks = %+v, want the identical entries deduped to one", m.Hooks["SessionStart"])
	}
	if len(m.Hooks["PreToolUse"]) != 1 {
		t.Fatalf("PreToolUse hooks = %+v, want the contributes entry kept", m.Hooks["PreToolUse"])
	}
	for _, name := range []string{"same", "legacy-only", "contrib-only"} {
		if _, ok := m.MCPServers[name]; !ok {
			t.Fatalf("MCPServers missing %q: %+v", name, m.MCPServers)
		}
	}
}

func TestManifestV2MergeConflictsFail(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{"hook redefined", `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "conflict-demo",
  "hooks": {"SessionStart": [{"command": "./start.sh", "timeout": 5000}]},
  "contributes": {"hooks": {"SessionStart": [{"command": "./start.sh", "timeout": 9000}]}}
}`, `hook "./start.sh" (event SessionStart) is defined differently`},
		{"mcp server redefined", `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "conflict-demo",
  "mcpServers": {"srv": {"type": "stdio", "command": "./a"}},
  "contributes": {"mcpServers": {"srv": {"type": "stdio", "command": "./b"}}}
}`, `MCP server "srv" is defined differently`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeV2Plugin(t, root, tc.manifest)
			_, _, err := ParseDir(root)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestManifestV2PromptsAndCommandsStaySeparateSets(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "dual-demo",
  "contributes": {
    "commands": ["shared"],
    "prompts": "shared"
  }
}`)
	writeTestFile(t, filepath.Join(root, "shared", "task.md"), "---\ndescription: task\n---\nDo $ARGUMENTS")
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}
	// A path listed under both contributes.prompts and contributes.commands
	// joins both semantic sets: slash-command discovery AND kernel prompts.
	if !reflect.DeepEqual(pkg.Manifest.Commands, []string{"shared"}) || !reflect.DeepEqual(pkg.Manifest.Prompts, []string{"shared"}) {
		t.Fatalf("Commands/Prompts = %#v/%#v", pkg.Manifest.Commands, pkg.Manifest.Prompts)
	}
	summary := pkg.CapabilitySummary()
	if summary.Commands != 1 || summary.Prompts != 1 {
		t.Fatalf("CapabilitySummary = %+v, want 1 command and 1 prompt from the shared path", summary)
	}
}

func TestManifestV2RuntimeValidation(t *testing.T) {
	cases := []struct {
		name    string
		runtime string
		wantErr string
	}{
		{"empty command", `{}`, `runtime.command is required`},
		{"blank command", `{"command": "  "}`, `runtime.command is required`},
		{"empty arg", `{"command": "./run", "args": ["ok", ""]}`, `runtime.args[1] must not be empty`},
		{"empty env key", `{"command": "./run", "env": {"": "x"}}`, `runtime.env contains an empty key`},
		{"priority too high", `{"command": "./run", "priority": 1001}`, `runtime.priority 1001 out of range [-1000, 1000]`},
		{"priority too low", `{"command": "./run", "priority": -1001}`, `runtime.priority -1001 out of range [-1000, 1000]`},
		{"unknown intercept", `{"command": "./run", "intercepts": ["input.recieve"]}`, `runtime.intercepts: unknown interceptor point "input.recieve"`},
		{"unknown replace slot", `{"command": "./run", "replaces": ["systemprompt"]}`, `runtime.replaces: unknown slot "systemprompt"`},
		{"bad tool slot", `{"command": "./run", "replaces": ["tool:"]}`, `runtime.replaces: invalid tool slot "tool:"`},
		{"bad provider slot", `{"command": "./run", "replaces": ["provider:openai"]}`, `runtime.replaces: invalid provider slot "provider:openai"`},
		{"unknown capability", `{"command": "./run", "capabilities": ["filesystem"]}`, `runtime.capabilities: unknown capability "filesystem" (want one of: interceptors, strategies, providers, ui)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeV2Plugin(t, root, `{"apiVersion": "reasonix.io/plugin/v2", "name": "rt-demo", "runtime": `+tc.runtime+`}`)
			_, _, err := ParseDir(root)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestManifestV2RuntimeAcceptsBoundaryValues(t *testing.T) {
	root := t.TempDir()
	writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "rt-ok",
  "runtime": {
    "command": "plugin-runtime",
    "priority": -1000,
    "intercepts": ["session.start", "session.end", "session.load", "session.save", "session.rotate", "input.receive", "agent.before_start", "system_prompt.build", "context.prepare", "provider.request", "provider.response", "tool.before", "tool.after", "permission.decision", "compaction.prepare", "compaction.complete", "frontend.event"],
    "replaces": ["system_prompt", "context", "provider_request", "provider_response", "compaction", "session_policy", "permission", "frontend_events", "tool:bash", "provider:openai/gpt-5"]
  }
}`)
	pkg, _, err := ParseDir(root)
	if err != nil {
		t.Fatalf("ParseDir boundary runtime: %v", err)
	}
	if pkg.Manifest.Runtime == nil || len(pkg.Manifest.Runtime.Intercepts) != 17 || len(pkg.Manifest.Runtime.Replaces) != 10 {
		t.Fatalf("Runtime = %+v", pkg.Manifest.Runtime)
	}
}

func TestManifestV2PathSafety(t *testing.T) {
	t.Run("dot-dot escape rejected", func(t *testing.T) {
		root := t.TempDir()
		writeV2Plugin(t, root, `{"apiVersion": "reasonix.io/plugin/v2", "name": "esc", "contributes": {"skills": ["../outside"]}}`)
		if _, _, err := ParseDir(root); err == nil || !strings.Contains(err.Error(), "must be relative and stay inside the plugin root") {
			t.Fatalf("error = %v, want the traversal refusal", err)
		}
	})
	for name, absolute := range map[string]string{
		"unix":          "/etc/passwd",
		"windows drive": `C:\Windows\System32\drivers\etc\hosts`,
		"windows UNC":   `\\server\share\secret.txt`,
	} {
		t.Run(name+" absolute path rejected", func(t *testing.T) {
			root := t.TempDir()
			writeV2Plugin(t, root, jsonText(map[string]any{
				"apiVersion": ManifestAPIVersionV2,
				"name":       "abs",
				"contributes": map[string]any{
					"prompts": []string{absolute},
				},
			}))
			if _, _, err := ParseDir(root); err == nil || !strings.Contains(err.Error(), "must be relative and stay inside the plugin root") {
				t.Fatalf("error = %v, want the absolute-path refusal", err)
			}
		})
	}
	t.Run("symlink escape rejected", func(t *testing.T) {
		outside := t.TempDir()
		writeTestFile(t, filepath.Join(outside, "evil.md"), "evil")
		root := t.TempDir()
		writeV2Plugin(t, root, `{"apiVersion": "reasonix.io/plugin/v2", "name": "linkesc", "contributes": {"skills": ["skills-link"]}}`)
		if err := os.Symlink(outside, filepath.Join(root, "skills-link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, _, err := ParseDir(root); err == nil || !strings.Contains(err.Error(), "escapes the plugin root through a symlink") {
			t.Fatalf("error = %v, want the symlink escape refusal", err)
		}
	})
	t.Run("in-root symlink accepted", func(t *testing.T) {
		root := t.TempDir()
		writeV2Plugin(t, root, `{"apiVersion": "reasonix.io/plugin/v2", "name": "linkok", "contributes": {"skills": ["skills-link"]}}`)
		writeTestFile(t, filepath.Join(root, "real-skills", "s", "SKILL.md"), "---\ndescription: s\n---\nS")
		if err := os.Symlink(filepath.Join(root, "real-skills"), filepath.Join(root, "skills-link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, _, err := ParseDir(root); err != nil {
			t.Fatalf("ParseDir in-root symlink: %v", err)
		}
	})
	t.Run("theme symlink escape rejected", func(t *testing.T) {
		outside := t.TempDir()
		writeTestFile(t, filepath.Join(outside, "evil.reasonix-theme"), "evil")
		root := t.TempDir()
		writeV2Plugin(t, root, `{"apiVersion": "reasonix.io/plugin/v2", "name": "themesc", "contributes": {"themes": ["themes/*.reasonix-theme"]}}`)
		if err := os.MkdirAll(filepath.Join(root, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "evil.reasonix-theme"), filepath.Join(root, "themes", "evil.reasonix-theme")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, _, err := ParseDir(root); err == nil || !strings.Contains(err.Error(), "escapes the plugin root through a symlink") {
			t.Fatalf("error = %v, want the theme symlink escape refusal", err)
		}
	})
	t.Run("non-regular theme rejected", func(t *testing.T) {
		root := t.TempDir()
		writeV2Plugin(t, root, `{"apiVersion": "reasonix.io/plugin/v2", "name": "themedir", "contributes": {"themes": ["themes"]}}`)
		if err := os.MkdirAll(filepath.Join(root, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ParseDir(root); err == nil || !strings.Contains(err.Error(), `theme "themes" is not a regular file`) {
			t.Fatalf("error = %v, want the non-regular theme refusal", err)
		}
	})
	t.Run("missing paths are warnings not failures", func(t *testing.T) {
		root := t.TempDir()
		writeV2Plugin(t, root, `{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "missing-demo",
  "contributes": {
    "skills": ["no-such-skills"],
    "prompts": ["no-such-prompts"],
    "themes": ["themes/*.reasonix-theme"]
  }
}`)
		pkg, warnings, err := ParseDir(root)
		if err != nil {
			t.Fatalf("ParseDir with missing paths: %v", err)
		}
		joined := strings.Join(warnings, "\n")
		for _, want := range []string{`skills path "no-such-skills" does not exist`, `prompts path "no-such-prompts" does not exist`, `theme glob "themes/*.reasonix-theme" matched no files`} {
			if !strings.Contains(joined, want) {
				t.Fatalf("warnings missing %q:\n%s", want, joined)
			}
		}
		if pkg.ThemeCount() != 0 {
			t.Fatalf("ThemeCount = %d, want 0 for an unmatched glob", pkg.ThemeCount())
		}
	})
}

func TestManifestV2DescribeRendersPromptsThemesRuntime(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "plugins", "example")
	writeV2Plugin(t, root, v2ExampleManifest)
	writeV2ExampleAssets(t, root)
	if err := Upsert(home, InstalledPlugin{Name: "example", Root: "plugins/example", Version: "1.0.0", ManifestKind: "reasonix", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	show, err := InstalledShowText(home, "example")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"capabilities: 1 skills, 1 commands, 1 prompts, 0 hooks, 0 MCP servers, 1 themes",
		"runtime: FULL TRUST",
		"command: ${REASONIX_PLUGIN_ROOT}/bin/example --serve",
		"intercepts: input.receive, tool.before",
		"replaces: system_prompt",
		"capabilities: interceptors, strategies, providers, ui",
		"bypass permissions",
		"prompts:\n  plan - plan",
		"themes:\n  neon - ",
	} {
		if !strings.Contains(show, want) {
			t.Fatalf("InstalledShowText missing %q:\n%s", want, show)
		}
	}

	list, err := InstalledListText(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 prompts", "1 themes", "FULL TRUST runtime"} {
		if !strings.Contains(list, want) {
			t.Fatalf("InstalledListText missing %q:\n%s", want, list)
		}
	}
}

func TestExpandRuntimeCommand(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "plugins", "example")
	if got := ExpandRuntimeCommand("${REASONIX_PLUGIN_ROOT}/bin/example", root); got != filepath.Join(root, "bin", "example") {
		t.Fatalf("ExpandRuntimeCommand = %q", got)
	}
	if got := ExpandRuntimeCommand("plugin-runtime", root); got != "plugin-runtime" {
		t.Fatalf("ExpandRuntimeCommand bare name = %q", got)
	}
}
