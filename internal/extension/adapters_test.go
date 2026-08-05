package extension

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/hook"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"

	// Registers the compile-time built-ins the adapter wraps.
	_ "reasonix/internal/tool/builtin"
)

// contribute is a small harness: run one contributor and return its
// contributions, failing the test on error.
func contribute(t *testing.T, c Contributor) []Contribution {
	t.Helper()
	out, err := c.Contribute(context.Background())
	if err != nil {
		t.Fatalf("%s.Contribute: %v", c.Name(), err)
	}
	return out
}

// TestBuiltinToolsContributor: every registered built-in becomes a KindTool
// at the builtin tier, and the whole set must pass kernel validation —
// built-ins violating the ID contract would be a real wiring bug.
func TestBuiltinToolsContributor(t *testing.T) {
	contribs := contribute(t, BuiltinToolsContributor())
	if len(contribs) == 0 {
		t.Fatal("no built-in tools contributed — is internal/tool/builtin imported?")
	}
	if len(contribs) != len(tool.Builtins()) {
		t.Fatalf("contributed %d tools, want %d", len(contribs), len(tool.Builtins()))
	}
	for _, ct := range contribs {
		if ct.Kind != KindTool {
			t.Fatalf("kind = %s, want tool", ct.Kind)
		}
		if ct.Source.Scope != ScopeBuiltin {
			t.Fatalf("tool %s scope = %s, want builtin", ct.ID, ct.Source.Scope)
		}
		if _, ok := ct.Payload.(tool.Tool); !ok {
			t.Fatalf("tool %s payload = %T, want tool.Tool", ct.ID, ct.Payload)
		}
	}
	snap, _, err := NewBuilder().AddContributor(BuiltinToolsContributor()).Build(context.Background())
	if err != nil {
		t.Fatalf("Build with built-in tools failed: %v", err)
	}
	if len(snap.ToolSchemas()) != len(contribs) {
		t.Fatalf("snapshot schemas = %d, want %d", len(snap.ToolSchemas()), len(contribs))
	}
}

// writeSkill creates a <root>/<name>/SKILL.md fixture.
func writeSkill(t *testing.T, root, name, desc string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\ndescription: " + desc + "\n---\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSkillsContributor: project skills keep the project tier; plugin skills
// become ScopePlugin with the package as PluginID and a package-qualified
// slash ID — the same identity the user invokes.
func TestSkillsContributor(t *testing.T) {
	projectRoot := t.TempDir()
	home := t.TempDir()
	pluginRoot := t.TempDir()
	writeSkill(t, filepath.Join(projectRoot, ".reasonix", skill.SkillsDirname), "projskill", "Project skill")
	writeSkill(t, pluginRoot, "plugskill", "Plugin skill")

	store := skill.New(skill.Options{
		HomeDir:         home,
		ProjectRoot:     projectRoot,
		CustomPaths:     []string{pluginRoot},
		PluginPaths:     map[string][]string{pluginRoot: {"mypkg"}},
		DisableBuiltins: true,
		Stderr:          io.Discard,
	})
	contribs := contribute(t, SkillsContributor(store))
	if len(contribs) != 2 {
		t.Fatalf("contributed %d skills, want 2: %+v", len(contribs), contribs)
	}
	byID := map[string]Contribution{}
	for _, ct := range contribs {
		if ct.Kind != KindSkill {
			t.Fatalf("kind = %s, want skill", ct.Kind)
		}
		if _, ok := ct.Payload.(skill.Skill); !ok {
			t.Fatalf("skill %s payload = %T, want skill.Skill", ct.ID, ct.Payload)
		}
		byID[ct.ID] = ct
	}
	proj, ok := byID["projskill"]
	if !ok {
		t.Fatalf("missing projskill contribution: %v", byID)
	}
	if proj.Source.Scope != ScopeProject || proj.Source.PluginID != "" {
		t.Fatalf("projskill source = %+v, want project tier, no plugin", proj.Source)
	}
	plug, ok := byID["mypkg:plugskill"]
	if !ok {
		t.Fatalf("missing mypkg:plugskill contribution: %v", byID)
	}
	if plug.Source.Scope != ScopePlugin || plug.Source.PluginID != "mypkg" {
		t.Fatalf("plugskill source = %+v, want plugin tier owned by mypkg", plug.Source)
	}
}

// TestCommandsContributor: LoadRoots resolution runs first — the plugin
// command arrives under its qualified name, its unambiguous short alias is
// retained as a hidden compatibility entry, and plain commands map to the
// project tier.
func TestCommandsContributor(t *testing.T) {
	userDir := t.TempDir()
	pluginDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(userDir, "review.md"), []byte("---\ndescription: Review code\n---\nreview $ARGUMENTS"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "commit.md"), []byte("---\ndescription: Commit\n---\ncommit $ARGUMENTS"), 0o644); err != nil {
		t.Fatal(err)
	}
	contribs := contribute(t, CommandsContributor(
		command.Root{Path: userDir},
		command.Root{Path: pluginDir, Plugin: "pkg"},
	))
	byID := map[string]Contribution{}
	for _, ct := range contribs {
		if ct.Kind != KindCommand {
			t.Fatalf("kind = %s, want command", ct.Kind)
		}
		if _, ok := ct.Payload.(command.Command); !ok {
			t.Fatalf("command %s payload = %T, want command.Command", ct.ID, ct.Payload)
		}
		byID[ct.ID] = ct
	}
	if len(byID) != 3 {
		t.Fatalf("command IDs = %v, want review, pkg:commit, and the hidden commit alias", byID)
	}
	if byID["review"].Source.Scope != ScopeProject {
		t.Fatalf("review scope = %s, want project", byID["review"].Source.Scope)
	}
	for _, id := range []string{"pkg:commit", "commit"} {
		if byID[id].Source.Scope != ScopePlugin || byID[id].Source.PluginID != "pkg" {
			t.Fatalf("%s source = %+v, want plugin tier owned by pkg", id, byID[id].Source)
		}
	}
}

// TestHooksContributor: hooks are additive, keyed "event#n" in load order,
// scoped by the settings file they came from.
func TestHooksContributor(t *testing.T) {
	projectRoot := t.TempDir()
	home := t.TempDir()
	settingsDir := filepath.Join(projectRoot, hook.SettingsDirname)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"hooks": {"PreToolUse": [{"command": "echo pre"}], "SessionStart": [{"command": "echo a"}, {"command": "echo b"}]}}`
	if err := os.WriteFile(filepath.Join(settingsDir, hook.SettingsFilename), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	contribs := contribute(t, HooksContributor(hook.LoadOptions{ProjectRoot: projectRoot, HomeDir: home}))
	if len(contribs) != 3 {
		t.Fatalf("contributed %d hooks, want 3: %+v", len(contribs), contribs)
	}
	ids := []string{}
	for _, ct := range contribs {
		if ct.Kind != KindHook {
			t.Fatalf("kind = %s, want hook", ct.Kind)
		}
		if ct.Source.Scope != ScopeProject {
			t.Fatalf("hook %s scope = %s, want project", ct.ID, ct.Source.Scope)
		}
		if _, ok := ct.Payload.(hook.ResolvedHook); !ok {
			t.Fatalf("hook %s payload = %T, want hook.ResolvedHook", ct.ID, ct.Payload)
		}
		ids = append(ids, ct.ID)
	}
	want := []string{"PreToolUse#0", "SessionStart#0", "SessionStart#1"}
	for i, id := range ids {
		if id != want[i] {
			t.Fatalf("hook IDs = %v, want %v", ids, want)
		}
	}
	// Hooks of one event from two tiers must both survive a build.
	snap, _, err := NewBuilder().AddContributor(
		HooksContributor(hook.LoadOptions{ProjectRoot: projectRoot, HomeDir: home}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("Build with hooks failed: %v", err)
	}
	if got := snap.Catalog().ByKind(KindHook); len(got) != 3 {
		t.Fatalf("effective hooks = %d, want all 3 (additive)", len(got))
	}
}

// TestMCPServersContributor pins the provenance → tier mapping: plugin
// package → plugin tier, project/workspace config → project tier, user-level
// → global tier.
func TestMCPServersContributor(t *testing.T) {
	contribs := contribute(t, MCPServersContributor(
		plugin.Spec{Name: "fs", Package: "pkgA", Command: "fs-server"},
		plugin.Spec{Name: "web", ConfigSource: "project_config", URL: "http://x"},
		plugin.Spec{Name: "legacy", Command: "legacy-server"},
	))
	if len(contribs) != 3 {
		t.Fatalf("contributed %d servers, want 3", len(contribs))
	}
	byID := map[string]Contribution{}
	for _, ct := range contribs {
		if ct.Kind != KindMCPServer {
			t.Fatalf("kind = %s, want mcp_server", ct.Kind)
		}
		if _, ok := ct.Payload.(plugin.Spec); !ok {
			t.Fatalf("server %s payload = %T, want plugin.Spec", ct.ID, ct.Payload)
		}
		byID[ct.ID] = ct
	}
	if byID["fs"].Source.Scope != ScopePlugin || byID["fs"].Source.PluginID != "pkgA" {
		t.Fatalf("fs source = %+v, want plugin tier owned by pkgA", byID["fs"].Source)
	}
	if byID["web"].Source.Scope != ScopeProject {
		t.Fatalf("web source = %+v, want project tier", byID["web"].Source)
	}
	if byID["legacy"].Source.Scope != ScopeGlobal {
		t.Fatalf("legacy source = %+v, want global tier", byID["legacy"].Source)
	}
}

// TestProvidersContributor: descriptors become KindProvider keyed by ref at
// the builtin tier.
func TestProvidersContributor(t *testing.T) {
	contribs := contribute(t, ProvidersContributor(
		provider.Descriptor{Ref: "deepseek/deepseek-chat", DisplayName: "DeepSeek"},
		provider.Descriptor{Ref: "openai/gpt-5"},
	))
	if len(contribs) != 2 {
		t.Fatalf("contributed %d providers, want 2", len(contribs))
	}
	for _, ct := range contribs {
		if ct.Kind != KindProvider {
			t.Fatalf("kind = %s, want provider", ct.Kind)
		}
		if ct.Source.Scope != ScopeBuiltin {
			t.Fatalf("provider %s scope = %s, want builtin", ct.ID, ct.Source.Scope)
		}
		desc, ok := ct.Payload.(provider.Descriptor)
		if !ok || desc.Ref != ct.ID {
			t.Fatalf("provider %s payload = %+v, want matching Descriptor", ct.ID, ct.Payload)
		}
	}
}

// TestAdaptersAssembleTogether: the realistic end-to-end path — every
// adapter feeding one builder, producing a frozen snapshot whose schema order
// and hash are stable across rebuilds.
func TestAdaptersAssembleTogether(t *testing.T) {
	projectRoot := t.TempDir()
	home := t.TempDir()
	writeSkill(t, filepath.Join(projectRoot, ".reasonix", skill.SkillsDirname), "projskill", "Project skill")
	cmdDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cmdDir, "review.md"), []byte("review body"), 0o644); err != nil {
		t.Fatal(err)
	}

	build := func() *RuntimeSnapshot {
		b := NewBuilder().WithSystemPrompt("sys").WithGeneration(1)
		b.AddContributor(
			BuiltinToolsContributor(),
			SkillsContributor(skill.New(skill.Options{
				HomeDir: home, ProjectRoot: projectRoot, DisableBuiltins: true, Stderr: io.Discard,
			})),
			CommandsContributor(command.Root{Path: cmdDir}),
			HooksContributor(hook.LoadOptions{ProjectRoot: projectRoot, HomeDir: home}),
			MCPServersContributor(plugin.Spec{Name: "fs", Package: "pkgA"}),
			ProvidersContributor(provider.Descriptor{Ref: "deepseek/deepseek-chat"}),
		)
		snap, set, err := b.Build(context.Background())
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if set.Generation() != 1 {
			t.Fatalf("set generation = %d, want 1", set.Generation())
		}
		return snap
	}
	first, second := build(), build()
	if first.CacheHash() != second.CacheHash() {
		t.Fatal("identical discovery state produced different CacheHash")
	}
	if !first.Catalog().Frozen() {
		t.Fatal("snapshot catalog is not frozen")
	}
	if len(first.ToolSchemas()) == 0 {
		t.Fatal("no tool schemas in snapshot")
	}
	if got := first.Catalog().ByKind(KindSkill); len(got) != 1 || got[0].ID != "projskill" {
		t.Fatalf("skills in snapshot = %v, want projskill", got)
	}
	if got := first.Catalog().ByKind(KindMCPServer); len(got) != 1 || got[0].ID != "fs" {
		t.Fatalf("MCP servers in snapshot = %v, want fs", got)
	}
}
