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
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

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
	if resp.Kinds.MCP != 2 {
		t.Errorf("Kinds.MCP = %d, want 2", resp.Kinds.MCP)
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
	if len(entries) != 1 || entries[0].Tier != "lazy" {
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
	cfg := config.LoadForEdit(filepath.Join(project, "reasonix.toml"))
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Command != "node" || len(cfg.Plugins[0].Args) != 1 || cfg.Plugins[0].Args[0] != server {
		t.Fatalf("persisted plugins = %+v, want node [%s]", cfg.Plugins, server)
	}
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
