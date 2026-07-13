package installsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "reasonix-installsource-test-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	_ = os.Setenv("AppData", filepath.Join(dir, "AppData"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// --- shared helpers ---------------------------------------------------------

// execInstall marshals args, calls Execute, and unmarshals the response.
// Failures in Execute bubble up as t.Fatal; the response is returned so the
// caller can assert on the JSON shape.
func execInstall(t *testing.T, tl tool.Tool, args map[string]any) response {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tl.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute error: %v\nout=%s", err, out)
	}
	var resp response
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("response JSON %q: %v", out, err)
	}
	return resp
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func registeredRoots(actions []action) []string {
	var roots []string
	for _, a := range actions {
		if a.Action == "register_skill_root" {
			roots = append(roots, a.Source)
		}
	}
	return roots
}

// stubConnector returns a ConnectMCP closure that records every entry and
// returns the configured tool count + Disconnect. disconnectCalls counts
// rollback invocations so tests can assert on ghost-install behavior.
type stubConnector struct {
	connected       []config.PluginEntry
	toolCount       int
	failOnName      string
	disconnectCalls *atomic.Int32
}

func (s *stubConnector) connector() MCPConnector {
	return func(e config.PluginEntry) (MCPConnectResult, error) {
		if e.Name == s.failOnName {
			return MCPConnectResult{}, errors.New("connect refused: " + e.Name)
		}
		s.connected = append(s.connected, e)
		return MCPConnectResult{
			ToolCount: s.toolCount,
			Disconnect: func() {
				if s.disconnectCalls != nil {
					s.disconnectCalls.Add(1)
				}
			},
		}, nil
	}
}

// --- apply: skill paths -----------------------------------------------------

func TestApplyLocalSkillRootRegistersPath(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "shared-skills")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "alpha.md"), "---\nname: alpha\ndescription: Alpha helper\n---\nDo alpha work.")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": root,
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
	})

	if !resp.OK || resp.Status != "done" {
		t.Fatalf("response = %+v", resp)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Action != "register_skill_root" {
		t.Fatalf("actions = %+v", resp.Actions)
	}
	if resp.PlanID == "" {
		t.Error("PlanID should be populated on apply")
	}
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if len(cfg.Skills.Paths) != 1 || cfg.Skills.Paths[0] != root {
		t.Fatalf("skills.paths = %v, want %q", cfg.Skills.Paths, root)
	}
	st := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: cfg.SkillCustomPaths(), DisableBuiltins: true})
	if _, ok := st.Read("alpha"); !ok {
		t.Fatal("alpha should be discoverable after registering the skill root")
	}
}

func TestApplyLocalCodexPluginPackage(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "superpowers")
	writeFile(t, filepath.Join(src, ".codex-plugin", "plugin.json"), `{
  "name": "superpowers",
  "version": "6.1.0",
  "description": "Planning workflows",
  "skills": "./skills/"
}`)
	writeFile(t, filepath.Join(src, "skills", "using-superpowers", "SKILL.md"), "---\nname: using-superpowers\ndescription: Use skills\n---\nUse skills.")
	writeFile(t, filepath.Join(src, "hooks", "session-start-codex"), "#!/usr/bin/env bash\necho ok\n")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	planned := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "plugin",
	})
	if planned.Kind != "plugin" || planned.Kinds.Plugin != 1 {
		t.Fatalf("planned = %+v, want one plugin action", planned)
	}
	if planned.Actions[0].Name != "superpowers" || planned.Actions[0].SkillCount != 1 || planned.Actions[0].HookCount != 1 {
		t.Fatalf("plugin action = %+v", planned.Actions[0])
	}

	done := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "plugin",
		"apply":  true,
	})
	if !done.OK || done.Status != "done" {
		t.Fatalf("apply response = %+v", done)
	}
	statePath := filepath.Join(home, ".reasonix", "plugin-packages.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	if !strings.Contains(string(raw), `"name": "superpowers"`) || !strings.Contains(string(raw), `"manifestKind": "codex"`) {
		t.Fatalf("state file = %s", raw)
	}
	if _, err := os.Stat(filepath.Join(home, ".reasonix", "plugins", "superpowers", ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("installed plugin missing: %v", err)
	}
}

func TestApplyLocalClaudePluginPackage(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "ui-ux-pro-max")
	writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), `{
  "name": "ui-ux-pro-max",
  "version": "2.6.2",
  "description": "UI/UX design intelligence",
  "skills": "./.claude/skills/"
}`)
	writeFile(t, filepath.Join(src, ".claude", "skills", "ui-ux-pro-max", "SKILL.md"), "---\nname: ui-ux-pro-max\ndescription: UI design helper\n---\nUse design rules.")
	writeFile(t, filepath.Join(src, "CLAUDE.md"), "Use the bundled UI workflow.")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	planned := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "plugin",
	})
	if planned.Kind != "plugin" || planned.Kinds.Plugin != 1 {
		t.Fatalf("planned = %+v, want one plugin action", planned)
	}
	if planned.Actions[0].Name != "ui-ux-pro-max" || planned.Actions[0].ManifestKind != "claude" || planned.Actions[0].SkillCount != 1 || planned.Actions[0].HookCount != 1 {
		t.Fatalf("plugin action = %+v", planned.Actions[0])
	}

	done := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "plugin",
		"apply":  true,
	})
	if !done.OK || done.Status != "done" {
		t.Fatalf("apply response = %+v", done)
	}
	statePath := filepath.Join(home, ".reasonix", "plugin-packages.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	if !strings.Contains(string(raw), `"name": "ui-ux-pro-max"`) || !strings.Contains(string(raw), `"manifestKind": "claude"`) {
		t.Fatalf("state file = %s", raw)
	}
	if _, err := os.Stat(filepath.Join(home, ".reasonix", "plugins", "ui-ux-pro-max", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("installed plugin missing: %v", err)
	}
}

func TestApplyLocalSkillFileCopiesToProject(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "beta.md")
	writeFile(t, src, "---\nname: beta\ndescription: Beta helper\n---\nDo beta work.")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
	})

	if !resp.OK || resp.Actions[0].Action != "copy_skill" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].RiskLevel != RiskLow {
		t.Errorf("copy of a single file should be RiskLow, got %q", resp.Actions[0].RiskLevel)
	}
	target := filepath.Join(project, ".reasonix", "skills", "beta", "SKILL.md")
	if raw, err := os.ReadFile(target); err != nil || !strings.Contains(string(raw), "Beta helper") {
		t.Fatalf("copied skill = %q err=%v", raw, err)
	}
	if resp.Actions[0].CanonicalPath != target || !resp.Actions[0].Discoverable || !resp.Actions[0].Indexed {
		t.Fatalf("skill verification fields = %+v, want canonical/discoverable/indexed", resp.Actions[0])
	}
}

func TestApplyLocalSkillFileDoesNotShadowFlatCompatInstall(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	existing := filepath.Join(project, ".reasonix", "skills", "beta.md")
	writeFile(t, existing, "---\nname: beta\ndescription: Existing beta\n---\nold")
	src := filepath.Join(t.TempDir(), "beta.md")
	writeFile(t, src, "---\nname: beta\ndescription: New beta\n---\nnew")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
	})

	if resp.OK {
		t.Fatalf("canonical install should not shadow existing flat skill, got %+v", resp)
	}
	if !strings.Contains(resp.Actions[0].Error, "already exists") {
		t.Fatalf("error = %q, want duplicate guard", resp.Actions[0].Error)
	}
}

func TestApplyLocalSKILLFileCopiesSiblingResources(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	srcDir := filepath.Join(t.TempDir(), "frontend-design")
	writeFile(t, filepath.Join(srcDir, "SKILL.md"), "---\nname: frontend-design\ndescription: Frontend helper\n---\nSee references/style.md")
	writeFile(t, filepath.Join(srcDir, "references", "style.md"), "# Style\n\nUse crisp layouts.")
	writeFile(t, filepath.Join(srcDir, "scripts", "lint.sh"), "#!/bin/sh\nexit 0\n")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": filepath.Join(srcDir, "SKILL.md"),
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
	})

	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].RiskLevel != RiskMedium {
		t.Fatalf("directory package copy should be RiskMedium, got %q", resp.Actions[0].RiskLevel)
	}
	target := filepath.Join(project, ".reasonix", "skills", "frontend-design", "SKILL.md")
	if resp.Actions[0].CanonicalPath != target {
		t.Fatalf("canonicalPath = %q, want %q", resp.Actions[0].CanonicalPath, target)
	}
	if _, err := os.Stat(filepath.Join(project, ".reasonix", "skills", "frontend-design", "references", "style.md")); err != nil {
		t.Fatalf("reference file should be copied with SKILL.md source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".reasonix", "skills", "frontend-design", "scripts", "lint.sh")); err != nil {
		t.Fatalf("script file should be copied with SKILL.md source: %v", err)
	}
	st := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, DisableBuiltins: true})
	sk, ok := st.Read("frontend-design")
	if !ok || !strings.Contains(sk.Body, "Use crisp layouts.") {
		t.Fatalf("installed skill should load copied reference, ok=%v body=%q", ok, sk.Body)
	}
}

func TestApplyLocalSkillLinkMode(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(project, "local-skills", "gamma.md")
	writeFile(t, src, "---\nname: gamma\ndescription: Gamma helper\n---\nDo gamma work.")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
		"mode":   "link",
	})

	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	if resp.Actions[0].Action != "link_skill" {
		t.Fatalf("action = %q, want link_skill", resp.Actions[0].Action)
	}
	if resp.Actions[0].RiskLevel != RiskMedium && resp.Actions[0].RiskLevel != RiskHigh {
		t.Errorf("link mode should be at least RiskMedium, got %q", resp.Actions[0].RiskLevel)
	}
	target := filepath.Join(project, ".reasonix", "skills", "gamma", "SKILL.md")
	if fi, err := os.Lstat(target); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("target should be a symlink: lstat err=%v mode=%v", err, fi.Mode())
	}
}

func TestPlanNestedSkillRootRegistersContainingRoots(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "skill-pack")
	writeFile(t, filepath.Join(root, "top.md"), "---\nname: top\ndescription: Top helper\n---\nbody")
	writeFile(t, filepath.Join(root, "superpower", "tool-a", "SKILL.md"), "---\nname: tool-a\ndescription: Tool A\n---\nbody")
	writeFile(t, filepath.Join(root, "superpower", "tool-b", "SKILL.md"), "---\nname: tool-b\ndescription: Tool B\n---\nbody")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": root,
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
	})

	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if len(resp.Actions) != 2 {
		t.Fatalf("actions = %+v, want root and nested containing-root registrations", resp.Actions)
	}
	registered := map[string][]string{}
	for _, a := range resp.Actions {
		registered[a.Source] = a.Skills
		if !a.Discoverable || !a.Indexed {
			t.Fatalf("registered action should be verified: %+v", a)
		}
	}
	if got := strings.Join(registered[root], ","); got != "top" {
		t.Fatalf("root skills = %q, want top", got)
	}
	if got := strings.Join(registered[filepath.Join(root, "superpower")], ","); got != "tool-a,tool-b" {
		t.Fatalf("nested skills = %q, want tool-a,tool-b", got)
	}
	st := skill.New(skill.Options{HomeDir: home, ProjectRoot: project, CustomPaths: registeredRoots(resp.Actions), DisableBuiltins: true})
	for _, name := range []string{"top", "tool-a", "tool-b"} {
		if _, ok := st.Read(name); !ok {
			t.Fatalf("%s should be discoverable after registering containing roots", name)
		}
	}
}

func TestPlanNestedSkillRootRespectsDepthLimit(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "deep-pack")
	writeFile(t, filepath.Join(root, "one", "two", "three", "deep", "SKILL.md"), "---\nname: deep\ndescription: Deep helper\n---\nbody")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	raw, _ := json.Marshal(map[string]any{
		"source": root,
		"kind":   "skill",
	})
	out, err := tl.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected no manifest within depth limit, got %s", out)
	}
	if !errors.Is(err, ErrManifestMissing) {
		t.Fatalf("error = %v, want ErrManifestMissing", err)
	}
}

func TestApplyLinkSkillRejectsEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("absolute path semantics differ on Windows")
	}
	project := t.TempDir()
	home := t.TempDir()

	// Synthesize a candidate that points at /etc/passwd by calling the
	// private helper directly. The link check runs before any disk write.
	if isLinkTargetSafe("/etc/passwd", home, project) {
		t.Fatal("/etc/passwd should be considered unsafe")
	}
	if !isLinkTargetSafe("./local-skill.md", home, project) {
		t.Fatal("relative link target should be safe")
	}
	if !isLinkTargetSafe(filepath.Join(project, "skills/x.md"), home, project) {
		t.Fatal("link under project root should be safe")
	}

	src := filepath.Join(t.TempDir(), "escape.md")
	writeFile(t, src, "---\nname: escape\ndescription: Escape helper\n---\nbody")
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
		"mode":   "link",
	})
	if resp.OK {
		t.Fatalf("unsafe link should fail, got %+v", resp)
	}
	if !strings.Contains(resp.Actions[0].Error, ErrUnsafeLinkTarget.Error()) {
		t.Errorf("error = %q, want ErrUnsafeLinkTarget", resp.Actions[0].Error)
	}
}

func TestParseSkillContentStrictAllowsMissingDescription(t *testing.T) {
	// strict=false lets us install a raw SKILL.md that lacks a description.
	// The model should still be told the description is empty so the
	// Skills index entry is honest.
	cand, err := parseSkillContent("---\nname: raw\n---\nBody without desc", "raw", "in-memory", false)
	if err != nil {
		t.Fatalf("strict=false should accept missing description: %v", err)
	}
	if cand.Description != "" {
		t.Errorf("description should be empty, got %q", cand.Description)
	}
	if cand.Name != "raw" {
		t.Errorf("name = %q, want raw", cand.Name)
	}

	if _, err := parseSkillContent("---\nname: strict\n---\nbody", "strict", "in-memory", true); err == nil {
		t.Fatal("strict=true should reject missing description")
	}
}

func TestParseSkillContentRejectsMalformedFrontmatter(t *testing.T) {
	_, err := parseSkillContent("---\nname: [broken\n---\nbody", "broken", "in-memory", true)
	if err == nil {
		t.Fatal("malformed frontmatter should fail")
	}
	if !strings.Contains(err.Error(), "invalid YAML") || !strings.Contains(strings.ToLower(err.Error()), "line") {
		t.Fatalf("error = %v, want invalid YAML with location", err)
	}
}

func TestApplyStrictFalseWarnsWhenDescriptionMissing(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "raw.md")
	writeFile(t, src, "---\nname: raw\n---\nBody without desc")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
		"strict": false,
	})

	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if !resp.Actions[0].Discoverable || !resp.Actions[0].Indexed {
		t.Fatalf("raw skill should still be discoverable/indexed with placeholder: %+v", resp.Actions[0])
	}
	found := false
	for _, warning := range append(resp.Warnings, resp.Actions[0].Warnings...) {
		if strings.Contains(warning, "no description") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v action warnings = %v, want missing description warning", resp.Warnings, resp.Actions[0].Warnings)
	}
}

// --- plan / apply: MCP paths -----------------------------------------------

func TestPlanLocalMCPJSON(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	mcpPath := filepath.Join(t.TempDir(), ".mcp.json")
	writeFile(t, mcpPath, `{
  "mcpServers": {
    "fs": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."] },
    "remote": { "type": "http", "url": "https://mcp.example.com/mcp", "headers": { "Authorization": "Bearer ${TOKEN}" } }
  }
}`)

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": mcpPath,
		"kind":   "mcp",
	})

	if !resp.OK || resp.Status != "planned" {
		t.Fatalf("response = %+v", resp)
	}
	if len(resp.Actions) != 2 {
		t.Fatalf("actions = %+v", resp.Actions)
	}
	if resp.Actions[0].Name != "fs" || resp.Actions[0].Transport != "stdio" {
		t.Fatalf("first action = %+v", resp.Actions[0])
	}
	if resp.Actions[1].Name != "remote" || resp.Actions[1].Transport != "http" {
		t.Fatalf("second action = %+v", resp.Actions[1])
	}
	for _, action := range resp.Actions {
		if action.Scope != "global" || action.ConfigPath != config.UserConfigPath() {
			t.Fatalf("local .mcp.json outside project action scope/path = %q %q, want global %q", action.Scope, action.ConfigPath, config.UserConfigPath())
		}
	}
	if resp.Kinds.MCP != 2 {
		t.Errorf("Kinds.MCP = %d, want 2", resp.Kinds.MCP)
	}
}

func TestPlanProjectMCPJSONDefaultsProject(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	mcpPath := filepath.Join(project, ".mcp.json")
	writeFile(t, mcpPath, `{
  "mcpServers": {
    "projectfs": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."] }
  }
}`)

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": mcpPath,
		"kind":   "mcp",
	})

	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	wantPath := filepath.Join(project, "reasonix.toml")
	if resp.Scope != "project" || resp.Actions[0].Scope != "project" || resp.Actions[0].ConfigPath != wantPath {
		t.Fatalf("project .mcp.json scope/path = response %q action %q path %q, want project %q", resp.Scope, resp.Actions[0].Scope, resp.Actions[0].ConfigPath, wantPath)
	}
}

func TestPlanMCPJSONUnknownTierProducesWarning(t *testing.T) {
	entries, warnings, err := parseMCPJSON([]byte(`{
  "mcpServers": {
    "x": { "command": "node", "tier": "absurd" }
  }
}`))
	if err != nil {
		t.Fatalf("parseMCPJSON: %v", err)
	}
	if len(entries) != 1 || entries[0].Tier != "background" {
		t.Fatalf("entries = %+v", entries)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "absurd") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning about unknown tier, got %v", warnings)
	}
}

func TestPlanMCPJSONDefaultTierIsBackground(t *testing.T) {
	entries, warnings, err := parseMCPJSON([]byte(`{
  "mcpServers": {
    "x": { "command": "node" }
  }
}`))
	if err != nil {
		t.Fatalf("parseMCPJSON: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(entries) != 1 || entries[0].Tier != "background" {
		t.Fatalf("entries = %+v, want default tier background", entries)
	}
}

func TestNormalizeTierDefaultBackgroundUnknownBackground(t *testing.T) {
	if got, ok := normalizeTier(""); got != "background" || !ok {
		t.Fatalf("normalizeTier(empty) = %q, %v; want background, true", got, ok)
	}
	if got, ok := normalizeTier("absurd"); got != "background" || ok {
		t.Fatalf("normalizeTier(absurd) = %q, %v; want background, false", got, ok)
	}
}

func TestPlanMCPJSONSplitsPastedCommandLine(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	mcpPath := filepath.Join(t.TempDir(), ".mcp.json")
	writeFile(t, mcpPath, `{
  "mcpServers": {
    "playwright": { "command": "npx -y @playwright/mcp" }
  }
}`)

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": mcpPath,
		"kind":   "mcp",
	})

	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	a := resp.Actions[0]
	if a.Command != "npx" || len(a.Args) != 2 || a.Args[0] != "-y" || a.Args[1] != "@playwright/mcp" {
		t.Fatalf("action command/args = %q %v, want npx [-y @playwright/mcp]", a.Command, a.Args)
	}
	found := false
	for _, warning := range resp.Warnings {
		if strings.Contains(warning, "split a pasted MCP command line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want split warning", resp.Warnings)
	}
}

func TestPlanMCPJSONRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty", `{"mcpServers": {}}`},
		{"stdio no command", `{"mcpServers": {"x": {"type": "stdio"}}}`},
		{"http no url", `{"mcpServers": {"x": {"type": "http"}}}`},
		{"unknown transport", `{"mcpServers": {"x": {"type": "smoke", "command": "c"}}}`},
		{"invalid name", `{"mcpServers": {"bad/name": {"command": "c"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseMCPJSON([]byte(tc.body)); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestApplyRemoteMCPURLConnectsAndPersists(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	stub := &stubConnector{toolCount: 3}
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		ConnectMCP:  stub.connector(),
	})

	resp := execInstall(t, tl, map[string]any{
		"source":  "https://mcp.example.com/mcp",
		"kind":    "mcp",
		"apply":   true,
		"scope":   "project",
		"name":    "example",
		"headers": map[string]string{"Authorization": "Bearer ${TOKEN}"},
	})

	if !resp.OK || resp.Status != "done" || resp.Actions[0].ToolCount != 3 {
		t.Fatalf("response = %+v", resp)
	}
	if len(stub.connected) != 1 || stub.connected[0].Name != "example" {
		t.Fatalf("connected = %+v", stub.connected)
	}
	if resp.Actions[0].RiskLevel != RiskHigh {
		t.Errorf("auth headers should produce RiskHigh, got %q", resp.Actions[0].RiskLevel)
	}
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("plugins = %+v", cfg.Plugins)
	}
}

func TestApplyRemoteMCPURLDefaultsGlobal(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	stub := &stubConnector{toolCount: 1}
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		ConnectMCP:  stub.connector(),
	})

	resp := execInstall(t, tl, map[string]any{
		"source": "https://global.example.com/mcp",
		"kind":   "mcp",
		"apply":  true,
		"name":   "global-default",
	})

	if !resp.OK || resp.Scope != "global" || resp.Actions[0].ConfigPath != config.UserConfigPath() {
		t.Fatalf("response = %+v, want global user config %q", resp, config.UserConfigPath())
	}
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if p, ok := findPlugin(userCfg.Plugins, "global-default"); !ok || p.URL != "https://global.example.com/mcp" {
		t.Fatalf("global config plugins = %+v, want global-default", userCfg.Plugins)
	}
	projectCfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if _, ok := findPlugin(projectCfg.Plugins, "global-default"); ok {
		t.Fatalf("project config should not receive default-global MCP: %+v", projectCfg.Plugins)
	}
}

func TestApplyMCPRejectsDuplicateByDefault(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	// Seed an existing entry the same way the first install would have.
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if err := cfg.UpsertPlugin(config.PluginEntry{Name: "dup", Command: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(filepath.Join(project, "reasonix.toml")); err != nil {
		t.Fatal(err)
	}

	stub := &stubConnector{toolCount: 1, failOnName: "dup"}
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, ConnectMCP: stub.connector()})

	resp := execInstall(t, tl, map[string]any{
		"source": "https://mcp.example.com/mcp",
		"kind":   "mcp",
		"apply":  true,
		"name":   "dup",
		"scope":  "project",
	})

	if resp.OK {
		t.Fatalf("expected duplicate rejection, got %+v", resp)
	}
	if !strings.Contains(resp.Actions[0].Error, "already exists") {
		t.Errorf("expected ErrAlreadyExists text, got %q", resp.Actions[0].Error)
	}
}

func TestApplyMCPReplaceOverwritesExisting(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if err := cfg.UpsertPlugin(config.PluginEntry{Name: "editable", Command: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(filepath.Join(project, "reasonix.toml")); err != nil {
		t.Fatal(err)
	}

	stub := &stubConnector{toolCount: 1}
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, ConnectMCP: stub.connector()})

	resp := execInstall(t, tl, map[string]any{
		"source":  "https://mcp.example.com/mcp",
		"kind":    "mcp",
		"apply":   true,
		"replace": true,
		"name":    "editable",
		"scope":   "project",
	})

	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	reloaded := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if reloaded.Plugins[0].Command != "" || reloaded.Plugins[0].URL != "https://mcp.example.com/mcp" {
		t.Errorf("replace did not update entry: %+v", reloaded.Plugins[0])
	}
}

func TestApplyMCPReplaceDisconnectsLiveServerBeforeConnect(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if err := cfg.UpsertPlugin(config.PluginEntry{Name: "live", Command: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(filepath.Join(project, "reasonix.toml")); err != nil {
		t.Fatal(err)
	}

	var liveConnected atomic.Bool
	liveConnected.Store(true)
	var disconnects atomic.Int32
	var connects atomic.Int32
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		ConnectMCP: func(e config.PluginEntry) (MCPConnectResult, error) {
			if e.Name == "live" && liveConnected.Load() {
				return MCPConnectResult{}, errors.New(`server "live" is already connected`)
			}
			liveConnected.Store(true)
			connects.Add(1)
			return MCPConnectResult{ToolCount: 1, Disconnect: func() { liveConnected.Store(false) }}, nil
		},
		OnDisconnect: func(name string) bool {
			if name != "live" || !liveConnected.Load() {
				return false
			}
			liveConnected.Store(false)
			disconnects.Add(1)
			return true
		},
	})

	resp := execInstall(t, tl, map[string]any{
		"source":  "https://mcp.example.com/mcp",
		"kind":    "mcp",
		"apply":   true,
		"replace": true,
		"name":    "live",
		"scope":   "project",
	})

	if !resp.OK {
		t.Fatalf("replace should reconnect after disconnecting old live server: %+v", resp)
	}
	if disconnects.Load() != 1 || connects.Load() != 1 || !liveConnected.Load() {
		t.Fatalf("disconnects=%d connects=%d live=%v", disconnects.Load(), connects.Load(), liveConnected.Load())
	}
}

func TestApplyMCPRollsBackOnSaveFailure(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	// Pre-create a directory at the config path so cfg.SaveTo will fail
	// (it cannot overwrite a non-empty directory with the file it wants).
	if err := os.MkdirAll(filepath.Join(project, "reasonix.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(project, "reasonix.toml", "blocker"), "x")

	var disconnects atomic.Int32
	stub := &stubConnector{toolCount: 2, disconnectCalls: &disconnects}
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		ConnectMCP:  stub.connector(),
		OnDisconnect: func(string) bool {
			disconnects.Add(1)
			return true
		},
	})

	resp := execInstall(t, tl, map[string]any{
		"source": "https://mcp.example.com/mcp",
		"kind":   "mcp",
		"apply":  true,
		"name":   "ghost",
		"scope":  "project",
	})

	if resp.OK {
		t.Fatalf("expected failure, got %+v", resp)
	}
	if got := disconnects.Load(); got != 1 {
		t.Errorf("rollback expected to call the new connection Disconnect once, got %d", got)
	}
}

func TestApplyConnectFailureDoesNotPersist(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	stub := &stubConnector{failOnName: "broken"}
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, ConnectMCP: stub.connector()})

	resp := execInstall(t, tl, map[string]any{
		"source": "https://mcp.example.com/mcp",
		"kind":   "mcp",
		"apply":  true,
		"name":   "broken",
		"scope":  "project",
	})

	if resp.OK {
		t.Fatalf("expected connect failure, got %+v", resp)
	}
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if len(cfg.Plugins) != 0 {
		t.Errorf("no plugin should be persisted on connect failure, got %+v", cfg.Plugins)
	}
}

func TestPackageActionUsesNpx(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": "@example/mcp-pkg",
		"kind":   "mcp",
	})

	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].Command != "npx" {
		t.Errorf("command = %q, want npx", resp.Actions[0].Command)
	}
	if len(resp.Actions[0].Args) != 2 || resp.Actions[0].Args[0] != "-y" || resp.Actions[0].Args[1] != "@example/mcp-pkg" {
		t.Errorf("args = %v, want [-y @example/mcp-pkg]", resp.Actions[0].Args)
	}
	if resp.Actions[0].Scope != "global" || resp.Actions[0].ConfigPath != config.UserConfigPath() {
		t.Errorf("scope/path = %q %q, want global %q", resp.Actions[0].Scope, resp.Actions[0].ConfigPath, config.UserConfigPath())
	}
}

func TestPlanURLBlobRewritesToRaw(t *testing.T) {
	got := rawGitHubBlobURL("https://github.com/foo/bar/blob/main/path/SKILL.md")
	want := "https://raw.githubusercontent.com/foo/bar/main/path/SKILL.md"
	if got != want {
		t.Errorf("rawGitHubBlobURL = %q, want %q", got, want)
	}
}

func TestPlanURLRemoteEndpointAuto(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": "https://mcp.example.com/mcp",
	})
	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].Transport != "http" {
		t.Errorf("transport = %q, want http", resp.Actions[0].Transport)
	}
	if resp.Actions[0].Scope != "global" || resp.Actions[0].ConfigPath != config.UserConfigPath() {
		t.Errorf("scope/path = %q %q, want global %q", resp.Actions[0].Scope, resp.Actions[0].ConfigPath, config.UserConfigPath())
	}
}

func TestPlanURLRemoteMCPHostAuto(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": "https://mcp.stripe.com",
	})
	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].Name != "stripe" || resp.Actions[0].Transport != "http" {
		t.Errorf("action = %+v, want stripe http", resp.Actions[0])
	}
}

func TestPlanURLSSEDefault(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": "https://example.com/sse/stream",
	})
	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].Transport != "sse" {
		t.Errorf("transport = %q, want sse (URL contains 'sse')", resp.Actions[0].Transport)
	}
}

func TestPlanUnsupportedKindReturnsTypedError(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	raw, _ := json.Marshal(map[string]any{
		"source": "https://example.com/sse/stream",
		"kind":   "skill",
	})
	out, err := tl.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected typed error, got %s", out)
	}
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Errorf("expected ErrUnsupportedKind, got %v", err)
	}
}

func TestPlanGitHubRepoProbesMainAndMaster(t *testing.T) {
	// We can't easily stand up a fake github.com, so we exercise the URL
	// rewriting and probe selection separately. The hostname check is
	// `strings.EqualFold(u.Hostname(), "github.com")` — anything else is
	// treated as a remote URL, not a repo probe.
	if got := rawGitHubBlobURL("https://github.com/foo/bar/blob/main/path/SKILL.md"); got != "https://raw.githubusercontent.com/foo/bar/main/path/SKILL.md" {
		t.Errorf("blob rewrite = %q", got)
	}
	if got := rawGitHubBlobURL("https://github.com/foo/bar/raw/main/path/SKILL.md"); got != "https://raw.githubusercontent.com/foo/bar/main/path/SKILL.md" {
		t.Errorf("raw rewrite = %q", got)
	}
	if got := rawGitHubBlobURL("https://example.com/foo/bar"); got != "https://example.com/foo/bar" {
		t.Errorf("non-github passthrough = %q", got)
	}
}

func TestPlanGitHubRepoDiscoversMultipleSkills(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/foo/bar/contents":
			if r.URL.Query().Get("ref") != "main" {
				t.Fatalf("ref = %q, want main", r.URL.Query().Get("ref"))
			}
			_, _ = fmt.Fprintf(w, `[
				{"name":"skills","path":"skills","type":"dir"}
			]`)
		case "/repos/foo/bar/contents/skills":
			_, _ = fmt.Fprintf(w, `[
				{"name":"gsap-core","path":"skills/gsap-core","type":"dir"},
				{"name":"gsap-timeline","path":"skills/gsap-timeline","type":"dir"}
			]`)
		case "/repos/foo/bar/contents/skills/gsap-core":
			_, _ = fmt.Fprintf(w, `[
				{"name":"SKILL.md","path":"skills/gsap-core/SKILL.md","type":"file","download_url":%q}
			]`, srv.URL+"/raw/gsap-core/SKILL.md")
		case "/repos/foo/bar/contents/skills/gsap-timeline":
			_, _ = fmt.Fprintf(w, `[
				{"name":"SKILL.md","path":"skills/gsap-timeline/SKILL.md","type":"file","download_url":%q}
			]`, srv.URL+"/raw/gsap-timeline/SKILL.md")
		case "/raw/gsap-core/SKILL.md":
			_, _ = w.Write([]byte("---\nname: gsap-core\ndescription: GSAP core helper\n---\ncore body"))
		case "/raw/gsap-timeline/SKILL.md":
			_, _ = w.Write([]byte("---\nname: gsap-timeline\ndescription: GSAP timeline helper\n---\ntimeline body"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldAPIBase := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = oldAPIBase }()

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, HTTPClient: srv.Client()})
	resp := execInstall(t, tl, map[string]any{
		"source": "https://github.com/foo/bar",
		"kind":   "skill",
	})

	if !resp.OK || resp.Status != "planned" {
		t.Fatalf("response = %+v", resp)
	}
	if len(resp.Actions) != 2 {
		t.Fatalf("actions = %+v, want two skills", resp.Actions)
	}
	if resp.Actions[0].Name != "gsap-core" || resp.Actions[1].Name != "gsap-timeline" {
		t.Fatalf("actions = %+v", resp.Actions)
	}
	for _, action := range resp.Actions {
		wantSuffix := filepath.Join(action.Name, skill.SkillFile)
		if action.Layout != "canonical_dir" || !strings.HasSuffix(action.CanonicalPath, wantSuffix) {
			t.Fatalf("action = %+v, want canonical layout ending in %s", action, wantSuffix)
		}
	}
}

func TestFetchTextAppliesTimeoutAndUA(t *testing.T) {
	// Use a context with a tiny deadline to assert timeout behavior. We
	// can't easily test the UA from inside a HandlerFunc, so we just check
	// that a cancelled context propagates as ErrSourceUnreadable.
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home}).(*installSourceTool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tl.fetchText(ctx, "http://example.invalid")
	if !errors.Is(err, ErrSourceUnreadable) {
		t.Errorf("expected ErrSourceUnreadable, got %v", err)
	}
}

func TestGlobalSkillInstallRootUsesReasonixHome(t *testing.T) {
	home := t.TempDir()
	reasonixHome := filepath.Join(t.TempDir(), "rx-home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", reasonixHome)
	oldUserHomeDir := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = oldUserHomeDir })

	tl := NewTool(Options{ProjectRoot: t.TempDir()}).(*installSourceTool)
	root, err := tl.skillInstallRoot("global")
	if err != nil {
		t.Fatalf("skillInstallRoot: %v", err)
	}
	want := filepath.Join(reasonixHome, skill.SkillsDirname)
	if root != want {
		t.Fatalf("global skill root = %q, want %q", root, want)
	}
}

func TestFetchTextAuthMapsToErrAuthRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, HTTPClient: srv.Client()}).(*installSourceTool)
	_, err := tl.fetchText(context.Background(), srv.URL)
	if !errors.Is(err, ErrAuthRequired) {
		t.Errorf("expected ErrAuthRequired, got %v", err)
	}
}

func TestFetchTextRefusesInternalAddress(t *testing.T) {
	// SSRF guard: an install source pointed at cloud-metadata / internal IPs must
	// be refused at dial time, not fetched. These are IP literals so no real
	// network or DNS is involved — the guard blocks before connecting.
	tl := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: t.TempDir()}).(*installSourceTool)
	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://10.0.0.1/",                         // RFC1918 internal
	} {
		if _, err := tl.fetchText(context.Background(), target); !errors.Is(err, ErrSourceUnreadable) {
			t.Errorf("fetchText(%q) err = %v, want ErrSourceUnreadable (SSRF-refused)", target, err)
		}
	}
}

func TestPlanMarkdownSkillURL(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("---\nname: remote-skill\ndescription: Remote helper\n---\nUse the remote helper."))
	}))
	defer srv.Close()

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, HTTPClient: srv.Client()})
	resp := execInstall(t, tl, map[string]any{
		"source": srv.URL + "/SKILL.md",
	})

	if !resp.OK || resp.Status != "planned" {
		t.Fatalf("response = %+v", resp)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Kind != "skill" || resp.Actions[0].Name != "remote-skill" {
		t.Fatalf("actions = %+v", resp.Actions)
	}
}

// --- uninstall --------------------------------------------------------------

func TestUninstallRemovesSkillByName(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	target := filepath.Join(project, ".reasonix", "skills", "doomed.md")
	writeFile(t, target, "---\nname: doomed\ndescription: Doomed\n---\nbody")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"op":    "uninstall",
		"name":  "doomed",
		"scope": "project",
	})

	if !resp.OK || resp.Status != "done" {
		t.Fatalf("response = %+v", resp)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("skill file should be gone, lstat err = %v", err)
	}
}

func TestUninstallRemovesRegisteredSkillRootByContainedSkillName(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "shared-skills")
	writeFile(t, filepath.Join(root, "alpha.md"), "---\nname: alpha\ndescription: Alpha helper\n---\nbody")
	writeFile(t, filepath.Join(root, "beta.md"), "---\nname: beta\ndescription: Beta helper\n---\nbody")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	install := execInstall(t, tl, map[string]any{
		"source": root,
		"kind":   "skill",
		"apply":  true,
		"scope":  "project",
	})
	if !install.OK {
		t.Fatalf("install response = %+v", install)
	}

	resp := execInstall(t, tl, map[string]any{
		"op":    "uninstall",
		"name":  "alpha",
		"scope": "project",
	})
	if !resp.OK || resp.Actions[0].Action != "remove_skill_root" {
		t.Fatalf("uninstall response = %+v", resp)
	}
	if resp.Actions[0].SkillCount != 2 {
		t.Errorf("SkillCount = %d, want 2", resp.Actions[0].SkillCount)
	}
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if len(cfg.Skills.Paths) != 0 {
		t.Fatalf("skills.paths should be empty after root uninstall, got %v", cfg.Skills.Paths)
	}
}

func TestUninstallRemovesMCPAndDisconnects(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if err := cfg.UpsertPlugin(config.PluginEntry{Name: "ed", Type: "http", URL: "https://mcp.example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(filepath.Join(project, "reasonix.toml")); err != nil {
		t.Fatal(err)
	}

	var disconnects atomic.Int32
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		OnDisconnect: func(string) bool {
			disconnects.Add(1)
			return true
		},
	})
	resp := execInstall(t, tl, map[string]any{
		"op":    "uninstall",
		"name":  "ed",
		"scope": "project",
	})

	if !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if disconnects.Load() != 1 {
		t.Errorf("OnDisconnect should fire once, got %d", disconnects.Load())
	}
	reloaded := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if len(reloaded.Plugins) != 0 {
		t.Errorf("plugin should be removed, got %+v", reloaded.Plugins)
	}
}

func TestUninstallWithoutScopePrefersProjectSkill(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	projectTarget := filepath.Join(project, ".reasonix", "skills", "dupe.md")
	globalTarget := filepath.Join(home, ".reasonix", "skills", "dupe.md")
	writeFile(t, projectTarget, "---\nname: dupe\ndescription: Project\n---\nbody")
	writeFile(t, globalTarget, "---\nname: dupe\ndescription: Global\n---\nbody")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"op":   "uninstall",
		"name": "dupe",
	})

	if !resp.OK || resp.Scope != "project" {
		t.Fatalf("response = %+v, want project uninstall", resp)
	}
	if _, err := os.Lstat(projectTarget); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("project skill should be gone, lstat err = %v", err)
	}
	if _, err := os.Lstat(globalTarget); err != nil {
		t.Errorf("global skill should remain when project matched first, lstat err = %v", err)
	}
}

func TestUninstallWithoutScopeFallsBackToGlobalMCP(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	name := "global-fallback"
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.UpsertPlugin(config.PluginEntry{Name: name, Type: "http", URL: "https://global.example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := config.LoadForEdit(config.UserConfigPath())
		if cleanup.RemovePlugin(name) {
			_ = cleanup.SaveTo(config.UserConfigPath())
		}
	})

	var disconnects atomic.Int32
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		OnDisconnect: func(string) bool {
			disconnects.Add(1)
			return true
		},
	})
	resp := execInstall(t, tl, map[string]any{
		"op":   "uninstall",
		"name": name,
	})

	if !resp.OK || resp.Scope != "global" {
		t.Fatalf("response = %+v, want global fallback uninstall", resp)
	}
	if disconnects.Load() != 1 {
		t.Errorf("OnDisconnect should fire once, got %d", disconnects.Load())
	}
	reloaded := config.LoadForEdit(config.UserConfigPath())
	if _, ok := findPlugin(reloaded.Plugins, name); ok {
		t.Fatalf("global MCP should be removed, got %+v", reloaded.Plugins)
	}
}

func TestUninstallUnknownNameIsBlocked(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"op":   "uninstall",
		"name": "ghost",
	})
	if resp.OK || resp.Status != "blocked" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestUninstallRequiresName(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	raw, _ := json.Marshal(map[string]any{"op": "uninstall"})
	if _, err := tl.Execute(context.Background(), raw); err == nil {
		t.Fatal("expected error when name is missing for uninstall")
	}
}

// --- approval hook ----------------------------------------------------------

func TestApprovalHookDeniesApply(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "zeta.md")
	writeFile(t, src, "---\nname: zeta\ndescription: Zeta helper\n---\nbody")

	var seen []action
	tl := NewTool(Options{
		ProjectRoot: project,
		HomeDir:     home,
		Approval: func(actions []action) error {
			seen = actions
			return errors.New("user said no")
		},
	})

	resp := execInstall(t, tl, map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
	})
	if resp.OK {
		t.Fatalf("expected denial, got %+v", resp)
	}
	if resp.Status != "denied" {
		t.Errorf("status = %q, want denied", resp.Status)
	}
	if len(seen) != 1 || seen[0].Name != "zeta" {
		t.Errorf("approval should see the planned actions, got %+v", seen)
	}
}

func TestPlanIDMismatchRefusesApply(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "eta.md")
	writeFile(t, src, "---\nname: eta\ndescription: Eta helper\n---\nbody")

	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	raw, _ := json.Marshal(map[string]any{
		"source": src,
		"kind":   "skill",
		"apply":  true,
		"planId": "sha256:00000000000000000000000000000000",
	})
	out, err := tl.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected planId mismatch error, got %s", out)
	}
	if !errors.Is(err, ErrApprovalDenied) {
		t.Errorf("expected ErrApprovalDenied, got %v", err)
	}
}

func TestPlanIDIncludesActionDetails(t *testing.T) {
	req := request{
		Op:     "install",
		Source: "/tmp/example/.mcp.json",
		Kind:   "mcp",
		Scope:  "project",
		Mode:   "auto",
	}
	a := action{Kind: "mcp", Action: "install_mcp_server", Name: "same", URL: "https://mcp.one.example/mcp", Transport: "http", ConfigPath: "/repo/reasonix.toml"}
	b := a
	b.URL = "https://mcp.two.example/mcp"
	if computePlanID(req, []action{a}) == computePlanID(req, []action{b}) {
		t.Fatal("planId should change when action URL changes")
	}
}

// --- sanitizers / parsers ---------------------------------------------------

func TestSanitizeNameEdges(t *testing.T) {
	cases := map[string]string{
		"":                       "mcp",
		"   ":                    "mcp",
		"@@leading":              "leading",    // leading @ is stripped before the prefix rule
		"_underscore":            "underscore", // config requires [a-zA-Z0-9] as first char
		"foo bar baz":            "foo-bar-baz",
		"foo/bar":                "foo-bar",
		"FOO":                    "foo",
		"a.b-c_d":                "a.b-c_d",
		strings.Repeat("x", 100): strings.Repeat("x", 64),
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMCPNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://mcp.stripe.com/mcp":        "stripe",
		"https://api.example.com/mcp":       "example",
		"https://www.foo.com/mcp":           "foo",
		"http://localhost:3000/mcp":         "local-3000",
		"https://mcp.example.co.uk/agent":   "example",
		"https://api.mcp.openai.com/v1/mcp": "openai",
	}
	for in, want := range cases {
		if got := mcpNameFromURL(in); got != want {
			t.Errorf("mcpNameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateMCPEntry(t *testing.T) {
	if err := validateMCPEntry(config.PluginEntry{Name: "x", Command: "y"}); err != nil {
		t.Errorf("stdio with command should validate, got %v", err)
	}
	if err := validateMCPEntry(config.PluginEntry{Name: "x", Type: "http", URL: "http://x"}); err != nil {
		t.Errorf("http with url should validate, got %v", err)
	}
	if err := validateMCPEntry(config.PluginEntry{Name: ""}); err == nil {
		t.Error("empty name should fail")
	}
	if err := validateMCPEntry(config.PluginEntry{Name: "bad/name", Command: "y"}); err == nil {
		t.Error("invalid name should fail")
	}
	if err := validateMCPEntry(config.PluginEntry{Name: "x", Type: "carrier-pigeon", Command: "c"}); err == nil {
		t.Error("unknown transport should fail")
	}
}

func TestComputePlanIDStable(t *testing.T) {
	req := request{Op: "install", Source: "x", Scope: "project", Kind: "skill"}
	actions := []action{{Kind: "skill", Name: "a", Action: "copy_skill"}}
	id1 := computePlanID(req, actions)
	id2 := computePlanID(req, actions)
	if id1 != id2 {
		t.Errorf("planId should be stable, got %q vs %q", id1, id2)
	}
	id3 := computePlanID(request{Op: "install", Source: "y", Scope: "project", Kind: "skill"}, actions)
	if id3 == id1 {
		t.Errorf("planId should change with source")
	}
}

func TestPlanIDUsesResolvedActionScope(t *testing.T) {
	reqOmitted := request{Op: "install", Source: "https://mcp.example.com/mcp", Kind: "mcp"}
	reqGlobal := request{Op: "install", Source: "https://mcp.example.com/mcp", Kind: "mcp", Scope: "global", scopeExplicit: true}
	actions := []action{{
		Kind:       "mcp",
		Action:     "install_mcp_server",
		Name:       "example",
		URL:        "https://mcp.example.com/mcp",
		Transport:  "http",
		Scope:      "global",
		ConfigPath: config.UserConfigPath(),
	}}
	if got, want := computePlanID(reqOmitted, actions), computePlanID(reqGlobal, actions); got != want {
		t.Fatalf("planId with omitted scope = %q, explicit global = %q; want same resolved plan", got, want)
	}
}

// --- local executable -------------------------------------------------------

func TestPlanLocalExecutableDetected(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := writeLocalExecutable(t, bin, "mcp-x")
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tl, map[string]any{
		"source": exe,
		"kind":   "mcp",
	})
	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].Command != exe {
		t.Errorf("command = %q, want the executable path", resp.Actions[0].Command)
	}
}

func TestApplyLocalExecutableHonorsCommandOverride(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	server := writeLocalExecutable(t, bin, "server")

	stub := &stubConnector{toolCount: 1}
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home, ConnectMCP: stub.connector()})
	resp := execInstall(t, tl, map[string]any{
		"source":  server,
		"kind":    "mcp",
		"apply":   true,
		"name":    "wrapped",
		"command": "node",
		"args":    []string{server},
	})

	if !resp.OK || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].Command != "node" || len(resp.Actions[0].Args) != 1 || resp.Actions[0].Args[0] != server {
		t.Fatalf("action command/args = %q %v, want node [%s]", resp.Actions[0].Command, resp.Actions[0].Args, server)
	}
	if len(stub.connected) != 1 || stub.connected[0].Command != "node" || len(stub.connected[0].Args) != 1 || stub.connected[0].Args[0] != server {
		t.Fatalf("connected entry = %+v, want node [%s]", stub.connected, server)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	p, ok := findPlugin(cfg.Plugins, "wrapped")
	if !ok || p.Command != "node" || len(p.Args) != 1 || p.Args[0] != server {
		t.Fatalf("persisted plugins = %+v, want wrapped node [%s]", cfg.Plugins, server)
	}
}

func findPlugin(entries []config.PluginEntry, name string) (config.PluginEntry, bool) {
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return config.PluginEntry{}, false
}

func writeLocalExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		writeFile(t, path, "@echo off\r\nexit /b 0\r\n")
		return path
	}
	path := filepath.Join(dir, name)
	writeFile(t, path, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- plan-only: RiskLevel surfacing -----------------------------------------

func TestLinkRiskIsMedium(t *testing.T) {
	if level, _ := skillActionRisk("link", skillCandidate{SourcePath: "x"}); level != RiskMedium {
		t.Errorf("link mode should be RiskMedium, got %q", level)
	}
	if level, _ := skillActionRisk("copy", skillCandidate{SourcePath: "x"}); level != RiskLow {
		t.Errorf("copy mode should be RiskLow, got %q", level)
	}
}

func TestEagerTierEscalatesRisk(t *testing.T) {
	level, _ := mcpActionRisk(config.PluginEntry{Name: "x", Tier: "eager", URL: "http://x"}, nil)
	if level != RiskHigh {
		t.Errorf("eager tier should escalate to RiskHigh, got %q", level)
	}
}

// --- helpers ----------------------------------------------------------------

// ExampleNewTool is a godoc example that exercises the public surface
// without touching the filesystem. It also serves as smoke coverage that
// the Schema() output is valid JSON and the tool does not panic on a
// well-formed call.
func ExampleNewTool() {
	tl := NewTool(Options{
		ProjectRoot: "/tmp/example",
		HomeDir:     "/tmp/example-home",
	})
	raw, _ := json.Marshal(map[string]any{"source": "https://example.com/mcp"})
	out, _ := tl.Execute(context.Background(), raw)
	var resp response
	_ = json.Unmarshal([]byte(out), &resp)
	fmt.Printf("status=%s kind=%s skill=%d mcp=%d\n",
		resp.Status, resp.Kind, resp.Kinds.Skill, resp.Kinds.MCP)
	// Output: status=planned kind=mcp skill=0 mcp=1
}

// TestGitHubPluginPlanMatchesApply pins the approval contract: the plan the
// user approves must describe exactly the capability set apply installs. Both
// phases resolve the source through pluginSource, so convention-discovered
// capabilities (skills/, commands/ — including nested namespaces) appear in
// the plan, not only after installation.
func TestGitHubPluginPlanMatchesApply(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), `{"name": "pwf", "version": "1.0.0"}`)
	writeFile(t, filepath.Join(src, "skills", "planner", "SKILL.md"), "---\ndescription: planner\n---\nbody")
	writeFile(t, filepath.Join(src, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan: $ARGUMENTS")
	writeFile(t, filepath.Join(src, "commands", "git", "commit.md"), "---\ndescription: commit\n---\nCommit")

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return src, "cafe0001", func() {}, nil
	}

	plan := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/pwf",
		"kind":   "plugin",
	})
	if !plan.OK || plan.Status != "planned" || len(plan.Actions) != 1 {
		t.Fatalf("plan response = %+v", plan)
	}
	planned := plan.Actions[0]
	if planned.SkillCount != 1 || planned.CommandCount != 2 {
		t.Fatalf("planned counts = %d skills / %d commands, want 1/2 (plan must see convention dirs)", planned.SkillCount, planned.CommandCount)
	}

	applied := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/pwf",
		"kind":   "plugin",
		"apply":  true,
	})
	if !applied.OK || applied.Status != "done" || len(applied.Actions) != 1 {
		t.Fatalf("apply response = %+v", applied)
	}
	got := applied.Actions[0]
	if got.SkillCount != planned.SkillCount || got.CommandCount != planned.CommandCount ||
		got.HookCount != planned.HookCount || got.ToolCount != planned.ToolCount {
		t.Fatalf("apply counts (%d/%d/%d/%d) diverge from approved plan (%d/%d/%d/%d)",
			got.SkillCount, got.CommandCount, got.HookCount, got.ToolCount,
			planned.SkillCount, planned.CommandCount, planned.HookCount, planned.ToolCount)
	}
}

// TestGitHubClaudeMarketplacePlansAndAppliesRelativePlugins pins the desktop
// workflow reported by users: entering a GitHub marketplace root should plan
// each relative-path plugin, then install all approved entries from one clone.
func TestGitHubClaudeMarketplacePlansAndAppliesRelativePlugins(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "legal-tools",
  "owner": {"name": "Legal Team"},
  "plugins": [
    {"name": "beta-legal", "source": "./plugins/beta"},
    {"name": "alpha-legal", "source": "./plugins/alpha"}
  ]
}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha-legal","version":"1.0.0"}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "alpha", "skills", "alpha", "SKILL.md"), "---\ndescription: alpha\n---\nAlpha")
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "beta", ".claude-plugin", "plugin.json"), `{"name":"beta-legal","version":"2.0.0"}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "beta", "commands", "review.md"), "---\ndescription: review\n---\nReview")

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	cloneCalls := 0
	cleanupCalls := 0
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		cloneCalls++
		if source != "https://github.com/acme/legal-tools" {
			t.Fatalf("unexpected extra clone for %q", source)
		}
		return marketplaceRoot, "cafe0001", func() { cleanupCalls++ }, nil
	}

	plan := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
	})
	if !plan.OK || plan.Status != "planned" || len(plan.Actions) != 2 {
		t.Fatalf("plan response = %+v", plan)
	}
	if plan.Actions[0].Name != "alpha-legal" || plan.Actions[1].Name != "beta-legal" {
		t.Fatalf("actions = %+v, want stable marketplace-name order", plan.Actions)
	}
	if plan.Actions[0].Source != "https://github.com/acme/legal-tools/tree/main/plugins/alpha" ||
		plan.Actions[1].Source != "https://github.com/acme/legal-tools/tree/main/plugins/beta" {
		t.Fatalf("marketplace action sources = %q / %q", plan.Actions[0].Source, plan.Actions[1].Source)
	}
	if cloneCalls != 1 || cleanupCalls != 1 {
		t.Fatalf("preview clone/cleanup calls = %d/%d, want 1/1", cloneCalls, cleanupCalls)
	}

	applied := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
		"apply":  true,
	})
	if !applied.OK || applied.Status != "done" || len(applied.Actions) != 2 {
		t.Fatalf("apply response = %+v", applied)
	}
	if cloneCalls != 2 || cleanupCalls != 2 {
		t.Fatalf("preview+apply clone/cleanup calls = %d/%d, want 2/2 (one clone per phase)", cloneCalls, cleanupCalls)
	}
	for _, name := range []string{"alpha-legal", "beta-legal"} {
		if _, ok, err := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), name); err != nil || !ok {
			t.Fatalf("installed plugin %q missing: ok=%v err=%v", name, ok, err)
		}
	}
}

func TestGitHubClaudeMarketplaceNameSelectsOnePlugin(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "legal-tools",
  "plugins": [
    {"name": "alpha-legal", "source": "./alpha"},
    {"name": "beta-legal", "source": "./beta"}
  ]
}`)
	for _, name := range []string{"alpha-legal", "beta-legal"} {
		dir := strings.TrimSuffix(name, "-legal")
		writeFile(t, filepath.Join(marketplaceRoot, dir, ".claude-plugin", "plugin.json"), fmt.Sprintf(`{"name":%q}`, name))
	}

	tl := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: t.TempDir()})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return marketplaceRoot, "cafe0001", func() {}, nil
	}
	plan := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
		"name":   "beta-legal",
	})
	if len(plan.Actions) != 1 || plan.Actions[0].Name != "beta-legal" {
		t.Fatalf("selected plan = %+v, want only beta-legal", plan.Actions)
	}
}

func TestGitHubClaudeMarketplaceRejectsEscapingRelativeSource(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "unsafe-tools",
  "plugins": [{"name": "escape", "source": "./../escape"}]
}`)

	tl := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: t.TempDir()})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return marketplaceRoot, "cafe0001", func() {}, nil
	}
	raw, _ := json.Marshal(map[string]any{
		"source": "https://github.com/acme/unsafe-tools",
		"kind":   "plugin",
	})
	_, err := tl.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v, want marketplace path escape rejection", err)
	}
}

func TestGitHubClaudeMarketplaceCleansCloneWhenApprovalIsDenied(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "one-tool",
  "plugins": [{"name": "alpha", "source": "./alpha"}]
}`)
	writeFile(t, filepath.Join(marketplaceRoot, "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha"}`)

	cleanupCalls := 0
	tl := NewTool(Options{
		ProjectRoot: t.TempDir(),
		HomeDir:     t.TempDir(),
		Approval: func(actions []action) error {
			return errors.New("not approved")
		},
	})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return marketplaceRoot, "cafe0001", func() { cleanupCalls++ }, nil
	}
	resp := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/one-tool",
		"kind":   "plugin",
		"apply":  true,
	})
	if resp.Status != "denied" || cleanupCalls != 1 {
		t.Fatalf("response=%+v cleanupCalls=%d, want denied and one cleanup", resp, cleanupCalls)
	}
}

// TestGitHubClaudeMarketplaceAcceptsBarePathsAndSkipsUnsupported pins the
// widened source subset: bare relative paths ("plugins/alpha") plan like
// "./"-prefixed ones, while object sources, external URLs, and invalid names
// skip with a warning instead of failing the whole plan.
func TestGitHubClaudeMarketplaceAcceptsBarePathsAndSkipsUnsupported(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "legal-tools",
  "metadata": {"pluginRoot": "plugins"},
  "plugins": [
    {"name": "alpha-legal", "source": "alpha"},
    {"name": "beta-legal", "source": "./beta"},
    {"name": "external", "source": "https://github.com/acme/elsewhere"},
    {"name": "object", "source": {"source": "github", "repo": "acme/elsewhere"}},
    {"name": "bad/name", "source": "./bad"}
  ]
}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha-legal"}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "beta", ".claude-plugin", "plugin.json"), `{"name":"beta-legal"}`)

	tl := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: t.TempDir()})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return marketplaceRoot, "cafe0001", func() {}, nil
	}
	plan := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
	})
	if !plan.OK || plan.Status != "planned" || len(plan.Actions) != 2 {
		t.Fatalf("plan response = %+v", plan)
	}
	if plan.Actions[0].Name != "alpha-legal" || plan.Actions[1].Name != "beta-legal" {
		t.Fatalf("actions = %+v, want alpha-legal and beta-legal", plan.Actions)
	}
	if plan.Actions[0].Source != "https://github.com/acme/legal-tools/tree/main/plugins/alpha" ||
		plan.Actions[1].Source != "https://github.com/acme/legal-tools/tree/main/plugins/beta" {
		t.Fatalf("action sources = %q / %q", plan.Actions[0].Source, plan.Actions[1].Source)
	}
	joined := strings.Join(plan.Warnings, "\n")
	for _, fragment := range []string{
		`"external": external source`,
		`"object": only relative string sources`,
		`"bad/name": not a valid plugin name`,
	} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("warnings %q missing skip notice %q", plan.Warnings, fragment)
		}
	}
}

// TestGitHubClaudeMarketplaceSelectedUnsupportedSourceFails pins the selection
// contract: skipping is only for bulk installs — when the user names exactly
// one plugin and its source shape is unsupported, the plan must fail loudly.
func TestGitHubClaudeMarketplaceSelectedUnsupportedSourceFails(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "legal-tools",
  "plugins": [
    {"name": "alpha-legal", "source": "./alpha"},
    {"name": "external", "source": "https://github.com/acme/elsewhere"}
  ]
}`)
	writeFile(t, filepath.Join(marketplaceRoot, "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha-legal"}`)

	tl := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: t.TempDir()})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return marketplaceRoot, "cafe0001", func() {}, nil
	}
	raw, _ := json.Marshal(map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
		"name":   "external",
	})
	_, err := tl.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "external source") {
		t.Fatalf("error = %v, want external-source rejection for the selected plugin", err)
	}
}

// TestGitHubClaudeMarketplacePlanIDStableAcrossPlanAndApply pins the approval
// contract the desktop host relies on: the planId returned by the preview must
// match the planId recomputed by the apply call, or every marketplace apply
// with an echoed planId would be refused.
func TestGitHubClaudeMarketplacePlanIDStableAcrossPlanAndApply(t *testing.T) {
	marketplaceRoot := t.TempDir()
	writeFile(t, filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"), `{
  "name": "legal-tools",
  "metadata": {"pluginRoot": "plugins"},
  "plugins": [
    {"name": "alpha-legal", "source": "alpha"},
    {"name": "beta-legal", "source": "beta"}
  ]
}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "alpha", ".claude-plugin", "plugin.json"), `{"name":"alpha-legal"}`)
	writeFile(t, filepath.Join(marketplaceRoot, "plugins", "beta", ".claude-plugin", "plugin.json"), `{"name":"beta-legal"}`)

	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: t.TempDir(), HomeDir: home})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return marketplaceRoot, "cafe0001", func() {}, nil
	}
	plan := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
	})
	if plan.Status != "planned" || len(plan.Actions) != 2 || plan.PlanID == "" {
		t.Fatalf("plan = %+v", plan)
	}
	applied := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/legal-tools",
		"kind":   "plugin",
		"apply":  true,
		"planId": plan.PlanID,
	})
	if !applied.OK || applied.Status != "done" {
		t.Fatalf("apply with echoed planId = %+v", applied)
	}
	if applied.PlanID != plan.PlanID {
		t.Fatalf("plan ID drifted between plan (%s) and apply (%s)", plan.PlanID, applied.PlanID)
	}
	for _, name := range []string{"alpha-legal", "beta-legal"} {
		if _, ok, err := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), name); err != nil || !ok {
			t.Fatalf("installed plugin %q missing: ok=%v err=%v", name, ok, err)
		}
	}
}

// TestGitHubPluginApplyRefusesUnpinnableDrift pins the snapshot contract: when
// the source resolves to a different commit than the plan approved and the
// approved snapshot cannot be restored, apply must refuse instead of
// installing content the approval never covered.
func TestGitHubPluginApplyRefusesUnpinnableDrift(t *testing.T) {
	tree1 := t.TempDir()
	writeFile(t, filepath.Join(tree1, ".claude-plugin", "plugin.json"), `{"name": "pwf", "version": "1.0.0"}`)
	writeFile(t, filepath.Join(tree1, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	tree2 := t.TempDir()
	writeFile(t, filepath.Join(tree2, ".claude-plugin", "plugin.json"), `{"name": "pwf", "version": "1.0.1"}`)
	writeFile(t, filepath.Join(tree2, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	writeFile(t, filepath.Join(tree2, "commands", "extra.md"), "---\ndescription: extra\n---\nExtra")

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	calls := 0
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		calls++
		if calls == 1 {
			return tree1, "cafe0001", func() {}, nil // plan recompute inside the apply call
		}
		return tree2, "cafe0002", func() {}, nil // apply resolution: source moved
	}

	resp := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/pwf",
		"kind":   "plugin",
		"apply":  true,
	})
	if resp.OK || resp.Status != "failed" {
		t.Fatalf("response = %+v, want a failed apply when the source drifted past the approved commit", resp)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Status != "failed" {
		t.Fatalf("actions = %+v, want the single install action failed", resp.Actions)
	}
	if !strings.Contains(resp.Actions[0].Error, "approved commit cafe0001") {
		t.Fatalf("action error = %q, want the approved-commit drift refusal", resp.Actions[0].Error)
	}
	if _, ok, _ := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), "pwf"); ok {
		t.Fatal("drifted plugin must not be installed")
	}
}

// TestCopyMaterializesInRootSymlinkedCommands pins that a command alias
// symlinked to a file inside the package survives copy-mode installs: the
// installed tree must resolve to the same capability set the plan counted.
func TestCopyMaterializesInRootSymlinkedCommands(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), `{"name": "aliases"}`)
	writeFile(t, filepath.Join(src, "skills", "s", "SKILL.md"), "---\ndescription: s\n---\nbody")
	writeFile(t, filepath.Join(src, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	if err := os.Symlink(filepath.Join(src, "commands", "plan.md"), filepath.Join(src, "commands", "pwf.md")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return src, "cafe0001", func() {}, nil
	}

	resp := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/aliases",
		"kind":   "plugin",
		"apply":  true,
	})
	if !resp.OK || resp.Status != "done" || len(resp.Actions) != 1 {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Actions[0].CommandCount != 2 {
		t.Fatalf("planned commands = %d, want 2 (alias followed)", resp.Actions[0].CommandCount)
	}
	installedRoot := filepath.Join(home, ".reasonix", "plugins", "aliases")
	pkg, _, err := pluginpkg.ParseDir(installedRoot)
	if err != nil {
		t.Fatalf("ParseDir installed: %v", err)
	}
	if _, commands, _, _ := pkg.CapabilityCounts(); commands != 2 {
		t.Fatalf("installed commands = %d, want the symlinked alias materialized", commands)
	}
}

// TestCopyRefusesUnmaterializableSymlinkCommands pins the fail-closed path: a
// command symlinked to a file OUTSIDE the package counts during planning but
// cannot be materialized by copy mode, so apply must refuse (and clean up)
// rather than silently install fewer commands than approved.
func TestCopyRefusesUnmaterializableSymlinkCommands(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "evil.md"), "---\ndescription: evil\n---\nEvil")
	src := t.TempDir()
	writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), `{"name": "escapes"}`)
	writeFile(t, filepath.Join(src, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	if err := os.Symlink(filepath.Join(outside, "evil.md"), filepath.Join(src, "commands", "evil.md")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return src, "cafe0001", func() {}, nil
	}

	resp := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/escapes",
		"kind":   "plugin",
		"apply":  true,
	})
	if resp.OK || resp.Status != "failed" || len(resp.Actions) != 1 || resp.Actions[0].Status != "failed" {
		t.Fatalf("response = %+v, want a failed apply for the unmaterializable symlink", resp)
	}
	if !strings.Contains(resp.Actions[0].Error, "approved plan counted") {
		t.Fatalf("action error = %q, want the capability-verification refusal", resp.Actions[0].Error)
	}
	if _, err := os.Stat(filepath.Join(home, ".reasonix", "plugins", "escapes")); !os.IsNotExist(err) {
		t.Fatal("failed install must not leave the copied tree behind")
	}
	if _, ok, _ := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), "escapes"); ok {
		t.Fatal("failed install must not be registered")
	}
}

// TestFailedReplaceKeepsExistingPluginInstall pins the update-safety contract:
// when a replace=true update fails capability verification (e.g. the new
// version ships an unmaterializable symlink), the previously installed
// version must survive on disk and stay registered — a failed update may
// never leave an enabled plugin pointing at a missing or gutted root.
func TestFailedReplaceKeepsExistingPluginInstall(t *testing.T) {
	v1 := t.TempDir()
	writeFile(t, filepath.Join(v1, ".claude-plugin", "plugin.json"), `{"name": "pwf", "version": "1.0.0"}`)
	writeFile(t, filepath.Join(v1, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "evil.md"), "---\ndescription: evil\n---\nEvil")
	v2 := t.TempDir()
	writeFile(t, filepath.Join(v2, ".claude-plugin", "plugin.json"), `{"name": "pwf", "version": "2.0.0"}`)
	writeFile(t, filepath.Join(v2, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	if err := os.Symlink(filepath.Join(outside, "evil.md"), filepath.Join(v2, "commands", "evil.md")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	current, commit := v1, "cafe0001"
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return current, commit, func() {}, nil
	}

	install := execInstall(t, tl, map[string]any{
		"source": "https://github.com/acme/pwf",
		"kind":   "plugin",
		"apply":  true,
	})
	if !install.OK || install.Status != "done" {
		t.Fatalf("initial install = %+v", install)
	}

	current, commit = v2, "cafe0002"
	update := execInstall(t, tl, map[string]any{
		"source":  "https://github.com/acme/pwf",
		"kind":    "plugin",
		"apply":   true,
		"replace": true,
	})
	if update.OK || update.Status != "failed" {
		t.Fatalf("update = %+v, want a failed apply for the unmaterializable symlink", update)
	}

	installedRoot := filepath.Join(home, ".reasonix", "plugins", "pwf")
	if _, err := os.Stat(filepath.Join(installedRoot, "commands", "plan.md")); err != nil {
		t.Fatalf("previous install must survive a failed update: %v", err)
	}
	pkg, _, err := pluginpkg.ParseDir(installedRoot)
	if err != nil {
		t.Fatalf("ParseDir installed: %v", err)
	}
	if pkg.Manifest.Version != "1.0.0" {
		t.Fatalf("installed version = %q, want the previous 1.0.0 kept", pkg.Manifest.Version)
	}
	if p, ok, _ := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), "pwf"); !ok || !p.Enabled {
		t.Fatal("previous registration must survive a failed update")
	}
	if _, err := os.Stat(installedRoot + ".pre-replace"); !os.IsNotExist(err) {
		t.Fatal("failed update must not leave a backup tree behind")
	}
	entries, err := os.ReadDir(filepath.Dir(installedRoot))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".staging-") {
			t.Fatalf("failed update must not leave staging dir %q behind", e.Name())
		}
	}
}

// TestBackupPathCannotCollideWithSiblingPlugin pins the backup-naming
// contract: plugin names may legally contain dots, so a plugin literally
// named "foo.pre-replace" must survive an update of plugin "foo" — the swap
// backup must use a name no valid plugin can occupy.
func TestBackupPathCannotCollideWithSiblingPlugin(t *testing.T) {
	fooV1 := t.TempDir()
	writeFile(t, filepath.Join(fooV1, ".claude-plugin", "plugin.json"), `{"name": "foo", "version": "1.0.0"}`)
	writeFile(t, filepath.Join(fooV1, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan")
	fooV2 := t.TempDir()
	writeFile(t, filepath.Join(fooV2, ".claude-plugin", "plugin.json"), `{"name": "foo", "version": "2.0.0"}`)
	writeFile(t, filepath.Join(fooV2, "commands", "plan.md"), "---\ndescription: plan\n---\nPlan v2")
	sibling := t.TempDir()
	writeFile(t, filepath.Join(sibling, ".claude-plugin", "plugin.json"), `{"name": "foo.pre-replace", "version": "1.0.0"}`)
	writeFile(t, filepath.Join(sibling, "commands", "keep.md"), "---\ndescription: keep\n---\nKeep")

	project := t.TempDir()
	home := t.TempDir()
	tl := NewTool(Options{ProjectRoot: project, HomeDir: home})
	tool := tl.(*installSourceTool)
	sources := map[string]string{
		"https://github.com/acme/foo":     fooV1,
		"https://github.com/acme/sibling": sibling,
	}
	tool.preparePlugin = func(ctx context.Context, source, mode string) (string, string, func(), error) {
		return sources[source], "cafe-" + source, func() {}, nil
	}

	for _, source := range []string{"https://github.com/acme/foo", "https://github.com/acme/sibling"} {
		resp := execInstall(t, tl, map[string]any{"source": source, "kind": "plugin", "apply": true})
		if !resp.OK || resp.Status != "done" {
			t.Fatalf("install %s = %+v", source, resp)
		}
	}

	sources["https://github.com/acme/foo"] = fooV2
	update := execInstall(t, tl, map[string]any{
		"source":  "https://github.com/acme/foo",
		"kind":    "plugin",
		"apply":   true,
		"replace": true,
	})
	if !update.OK || update.Status != "done" {
		t.Fatalf("update = %+v", update)
	}

	siblingRoot := filepath.Join(home, ".reasonix", "plugins", "foo.pre-replace")
	if _, err := os.Stat(filepath.Join(siblingRoot, "commands", "keep.md")); err != nil {
		t.Fatalf("sibling plugin's files must survive the update of foo: %v", err)
	}
	if _, ok, _ := pluginpkg.FindInstalled(filepath.Join(home, ".reasonix"), "foo.pre-replace"); !ok {
		t.Fatal("sibling plugin must stay registered")
	}
	pkg, _, err := pluginpkg.ParseDir(filepath.Join(home, ".reasonix", "plugins", "foo"))
	if err != nil {
		t.Fatalf("ParseDir foo: %v", err)
	}
	if pkg.Manifest.Version != "2.0.0" {
		t.Fatalf("foo version = %q, want the update applied", pkg.Manifest.Version)
	}
}
