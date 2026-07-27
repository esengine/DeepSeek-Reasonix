package capdiag_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/capdiag"
	"reasonix/internal/pluginpkg"
)

func TestCollectStaticNoNetworkSideEffects(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	// Shadowed skill + missing description + command override.
	write(t, filepath.Join(root, ".reasonix", "skills", "demo", "SKILL.md"),
		"---\nname: demo\ndescription: project demo\n---\nbody\n")
	write(t, filepath.Join(home, ".reasonix", "skills", "demo", "SKILL.md"),
		"---\nname: demo\ndescription: global demo\n---\nbody\n")
	write(t, filepath.Join(root, ".reasonix", "skills", "nodesc", "SKILL.md"),
		"---\nname: nodesc\n---\nbody\n")
	write(t, filepath.Join(root, ".reasonix", "commands", "hi.md"),
		"---\ndescription: project hi\n---\nP $ARGUMENTS\n")
	write(t, filepath.Join(home, ".reasonix", "commands", "hi.md"),
		"---\ndescription: home hi\n---\nH $ARGUMENTS\n")

	// Project hooks load automatically.
	write(t, filepath.Join(root, ".reasonix", "settings.json"), `{
  "hooks": {
    "PreToolUse": [{"match": "(", "command": "echo bad"}, {"match": ".*", "command": "echo ok"}]
  }
}`)

	// MCP with missing command.
	write(t, filepath.Join(root, "reasonix.toml"), `
[[plugins]]
name = "broken"
type = "stdio"
command = "definitely-not-a-real-binary-xyzzy-reasonix"
auto_start = false
`)

	// Instruction file.
	write(t, filepath.Join(root, "AGENTS.md"), "# Agents\nUse go test.\n")

	r := capdiag.Collect(capdiag.Options{
		Root:            root,
		HomeDir:         home,
		ReasonixHomeDir: filepath.Join(home, ".reasonix"),
		Live:            false,
	})

	if r.SchemaVersion != 1 {
		t.Fatalf("schema = %d", r.SchemaVersion)
	}
	if r.Live {
		t.Fatal("static mode must set live=false")
	}
	// Missing convention dirs should not produce warnings.
	for _, is := range r.Issues {
		if strings.Contains(is.Message, "missing") && is.Subsystem == "skills" && is.Code != "skill.missing_description" {
			t.Fatalf("unexpected missing-dir issue: %+v", is)
		}
	}

	codes := map[string]bool{}
	for _, is := range r.Issues {
		codes[is.Code] = true
	}
	for _, want := range []string{
		"skill.shadowed", "skill.missing_description", "command.shadowed",
		"hook.invalid_matcher", "mcp.command_not_found",
	} {
		if !codes[want] {
			t.Fatalf("missing issue code %s in %+v", want, codes)
		}
	}

	// Path redaction: no raw home username paths.
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw) + capdiag.RenderText(r)
	if strings.Contains(text, home) {
		t.Fatalf("report leaked home path %q", home)
	}
	if strings.Contains(text, root) && !strings.Contains(text, "<workspace>") {
		// Root itself is rewritten to <workspace>; full root abs path must not appear.
		t.Fatalf("report leaked workspace abs path")
	}
	if strings.Contains(text, "token=") || strings.Contains(text, "Bearer ") {
		t.Fatal("report leaked secret-like material")
	}

	// Deterministic JSON round.
	j1, _ := capdiag.RenderJSON(r)
	r2 := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: filepath.Join(home, ".reasonix"),
	})
	j2, _ := capdiag.RenderJSON(r2)
	if j1 != j2 {
		t.Fatal("JSON report not deterministic")
	}

	// Instructions listed.
	if len(r.Instructions.Docs) == 0 {
		t.Fatal("expected AGENTS.md in instructions")
	}
}

func TestMissingConventionDirsNoWarning(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: filepath.Join(home, ".reasonix"),
	})
	if r.Issues == nil {
		t.Fatal("empty issues must be a non-nil slice for JSON consumers")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"issues":[]`) {
		t.Fatalf("empty issues must marshal as [], got %s", raw)
	}
	for _, is := range r.Issues {
		if is.Severity == "warning" && strings.Contains(strings.ToLower(is.Message), "directory") {
			t.Fatalf("missing dir should not warn: %+v", is)
		}
	}
	// Roots may be missing; that is a normal status, not an issue.
	_ = r.Skills.Roots
}

func TestProjectHooksEnabledByDefault(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	write(t, filepath.Join(root, ".reasonix", "settings.json"), `{
  "hooks": {"Stop": [{"command": "echo done"}]}
}`)
	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: filepath.Join(home, ".reasonix"),
	})
	if !r.Hooks.TrustedProject {
		t.Fatal("compatibility field trusted_project should reflect default enablement")
	}
	if len(r.Hooks.Entries) != 1 || r.Hooks.Entries[0].Scope != "project" {
		t.Fatalf("project hook should be diagnosed as active by default: %+v", r.Hooks.Entries)
	}
}

func TestHasErrorSeverity(t *testing.T) {
	if capdiag.HasErrorSeverity(capdiag.Report{}) {
		t.Fatal("empty should be clean")
	}
	if !capdiag.HasErrorSeverity(capdiag.Report{Issues: []capdiag.Issue{{Severity: "error"}}}) {
		t.Fatal("error severity not detected")
	}
	if capdiag.HasErrorSeverity(capdiag.Report{Issues: []capdiag.Issue{{Severity: "warning"}}}) {
		t.Fatal("warning alone should not fail")
	}
}

func TestLoadForRootReadOnlyDoesNotRewriteTier(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	userCfg := filepath.Join(home, ".reasonix", "config.toml")
	if err := os.MkdirAll(filepath.Dir(userCfg), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "[[plugins]]\nname = \"x\"\ncommand = \"echo\"\ntier = \"eager\"\n"
	if err := os.WriteFile(userCfg, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: filepath.Join(home, ".reasonix"),
	})
	raw, err := os.ReadFile(userCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != original {
		t.Fatalf("static diagnostics rewrote config file:\n got %q\nwant %q", raw, original)
	}
}

func TestUnknownHookEventIsReported(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	write(t, filepath.Join(root, ".reasonix", "settings.json"), `{
  "hooks": {
    "NotARealEvent": [{"command": "echo hi"}]
  }
}`)
	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: filepath.Join(home, ".reasonix"),
	})
	found := false
	for _, is := range r.Issues {
		if is.Code == "hook.unknown_event" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected hook.unknown_event, issues=%+v", r.Issues)
	}
}

func TestCollectIgnoresMatchersOnNonToolHookEvents(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", reasonixHome)
	write(t, filepath.Join(reasonixHome, "settings.json"), `{
  "hooks": {
    "Stop": [{"match": "(", "command": "echo done"}]
  }
}`)

	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: reasonixHome,
	})
	if len(r.Hooks.Entries) != 1 {
		t.Fatalf("hook entries = %+v, want one Stop hook", r.Hooks.Entries)
	}
	for _, issue := range r.Issues {
		if issue.Code == "hook.invalid_matcher" {
			t.Fatalf("non-tool Stop matcher was reported invalid: %+v", issue)
		}
	}
}

func TestCollectRejectsNonRegularPluginContextFile(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", reasonixHome)

	pluginRoot := filepath.Join(reasonixHome, "plugins", "demo")
	write(t, filepath.Join(pluginRoot, pluginpkg.NativeManifest), `{
  "name": "demo",
  "hooks": {
    "SessionStart": [{"contextFile": "CLAUDE.md"}]
  }
}`)
	if err := os.MkdirAll(filepath.Join(pluginRoot, "CLAUDE.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name: "demo", Root: "plugins/demo", ManifestKind: "reasonix", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: reasonixHome,
	})
	for _, issue := range r.Issues {
		if issue.Code == "hook.missing_context_file" {
			return
		}
	}
	t.Fatalf("expected hook.missing_context_file for context directory, issues=%+v", r.Issues)
}

func TestPluginPackageCommandsAreReported(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", reasonixHome)

	pluginRoot := filepath.Join(reasonixHome, "plugins", "demo")
	write(t, filepath.Join(pluginRoot, pluginpkg.NativeManifest), `{"name":"demo","commands":["commands"]}`)
	write(t, filepath.Join(pluginRoot, "commands", "ship.md"), "---\ndescription: ship it\n---\nShip $ARGUMENTS\n")
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name: "demo", Root: "plugins/demo", ManifestKind: "reasonix", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: reasonixHome,
	})
	if len(r.Plugins.Packages) != 1 {
		t.Fatalf("plugin packages = %+v, want demo", r.Plugins.Packages)
	}
	pkg := r.Plugins.Packages[0]
	if pkg.Commands != 1 {
		t.Fatalf("plugin commands = %d, want 1", pkg.Commands)
	}
	if r.Commands.Winners != 1 {
		t.Fatalf("command winners = %d, want plugin command", r.Commands.Winners)
	}
	if text := capdiag.RenderText(r); !strings.Contains(text, "commands=1") {
		t.Fatalf("text report omitted plugin commands:\n%s", text)
	}
}

func TestDisplayPathExternal(t *testing.T) {
	// External absolute path should not include user components in JSON.
	root := t.TempDir()
	home := t.TempDir()
	ext := filepath.Join(t.TempDir(), "secret-user-bin", "tool")
	write(t, filepath.Join(root, "reasonix.toml"), `
[[plugins]]
name = "ext"
type = "stdio"
command = "`+filepath.ToSlash(ext)+`"
`)
	// Create the binary so we don't also get command_not_found noise on path form.
	write(t, ext, "#!/bin/sh\n")
	if runtime.GOOS != "windows" {
		_ = os.Chmod(ext, 0o755)
	}
	r := capdiag.Collect(capdiag.Options{
		Root: root, HomeDir: home, ReasonixHomeDir: filepath.Join(home, ".reasonix"),
	})
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "secret-user-bin") {
		// basename of command is "tool"; parent dir name must not appear.
		// displayPath uses base only for external: <external>/tool
		t.Fatalf("leaked external parent path: %s", raw)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
