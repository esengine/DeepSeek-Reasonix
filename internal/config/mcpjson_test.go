package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestLoadMCPJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, mcpJSONFile)
	doc := `{
  "mcpServers": {
    "stripe": {
      "type": "http",
      "url": "https://mcp.stripe.com",
      "headers": { "Authorization": "Bearer ${STRIPE_KEY}" }
    },
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "FOO": "bar" }
    }
  }
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadMCPJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted by name: filesystem before stripe.
	if len(got) != 2 || got[0].Name != "filesystem" || got[1].Name != "stripe" {
		t.Fatalf("entries = %+v, want [filesystem stripe] sorted", got)
	}
	fs := got[0]
	if fs.Command != "npx" || len(fs.Args) != 3 || fs.Env["FOO"] != "bar" {
		t.Errorf("filesystem decoded wrong: %+v", fs)
	}
	if fs.Source != MCPSourceProjectMCPJSON {
		t.Errorf("filesystem source = %q, want project .mcp.json", fs.Source)
	}
	st := got[1]
	if st.Type != "http" || st.URL != "https://mcp.stripe.com" ||
		st.Headers["Authorization"] != "Bearer ${STRIPE_KEY}" {
		t.Errorf("stripe decoded wrong: %+v", st)
	}
}

func TestLoadMCPJSONDecodesGB18030(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, mcpJSONFile)
	doc := `{"mcpServers":{"local":{"command":"工具.exe","env":{"LABEL":"中文"}}}}`
	if err := os.WriteFile(path, fileencoding.Encode(doc, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadMCPJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "工具.exe" || got[0].Env["LABEL"] != "中文" {
		t.Fatalf("decoded .mcp.json entries = %+v", got)
	}
}

func TestMCPJSONDropsRemovedTrustedReadOnlyToolsSetting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, mcpJSONFile)
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"github":{"command":"old","trusted_read_only_tools":["issue_read"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertMCPJSONPlugin(path, PluginEntry{
		Name:    "github",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-github"},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "trusted_read_only_tools") {
		t.Fatalf("updated .mcp.json retained removed reader setting:\n%s", body)
	}
	got, err := loadMCPJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %+v, want one github entry", got)
	}
}

func TestMCPJSONCallTimeoutsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, mcpJSONFile)
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "maker": {
      "command": "old-maker",
      "unknown_field": true
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertMCPJSONPlugin(path, PluginEntry{
		Name:               "maker",
		Command:            "maker-mcp",
		CallTimeoutSeconds: 600,
		ToolTimeoutSeconds: map[string]int{
			"generate/video": 1800,
			"search":         120,
			"ignored_zero":   0,
		},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := loadMCPJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("entries = %+v, want one maker entry", got)
	}
	if got[0].CallTimeoutSeconds != 600 {
		t.Fatalf("call_timeout_seconds = %d, want 600", got[0].CallTimeoutSeconds)
	}
	if got[0].ToolTimeoutSeconds["generate/video"] != 1800 || got[0].ToolTimeoutSeconds["search"] != 120 {
		t.Fatalf("tool_timeout_seconds = %+v, want generate/video=1800 search=120", got[0].ToolTimeoutSeconds)
	}
	if _, ok := got[0].ToolTimeoutSeconds["ignored_zero"]; ok {
		t.Fatalf("zero timeout should not be written: %+v", got[0].ToolTimeoutSeconds)
	}

	root, servers, err := readMCPJSONRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(root) == 0 || len(servers) != 1 {
		t.Fatalf("raw root/servers = %+v/%+v", root, servers)
	}
	var server map[string]any
	if err := json.Unmarshal(servers["maker"], &server); err != nil {
		t.Fatal(err)
	}
	if server["unknown_field"] != true {
		t.Fatalf("unknown per-server field was not preserved: %+v", server)
	}
}

func TestMCPJSONUpdateRemovesRetiredApprovalFieldsAndPreservesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), mcpJSONFile)
	if err := os.WriteFile(path, []byte(`{
  "mcpServers": {
    "admin": {
      "command": "old-admin-mcp",
      "future_server_field": {"version": 2},
      "tools": {
        "wipe": {"approval_mode": "prompt", "enabled": false, "future": {"audit": true}},
        "external_only": {"enabled": false},
        "remove_keep": {"approval_mode": "writes", "enabled": true},
        "remove_entirely": {"approval_mode": "approve"}
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := UpsertMCPJSONPlugin(path, PluginEntry{Name: "admin", Command: "admin-mcp"}); err != nil {
		t.Fatal(err)
	}

	root, servers, err := readMCPJSONRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(root) == 0 {
		t.Fatal("raw root is empty")
	}
	var server map[string]json.RawMessage
	if err := json.Unmarshal(servers["admin"], &server); err != nil {
		t.Fatal(err)
	}
	if _, ok := server["future_server_field"]; !ok {
		t.Fatal("unknown per-server field was removed")
	}
	var tools map[string]map[string]json.RawMessage
	if err := json.Unmarshal(server["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 3 {
		t.Fatalf("raw tools = %+v, want wipe, external_only, and remove_keep", tools)
	}
	if _, ok := tools["wipe"]["enabled"]; !ok {
		t.Fatal("known tool lost external enabled field")
	}
	if _, ok := tools["wipe"]["future"]; !ok {
		t.Fatal("known tool lost future nested field")
	}
	if _, ok := tools["external_only"]; !ok {
		t.Fatal("unknown-only tool entry was removed")
	}
	if _, ok := tools["remove_keep"]["approval_mode"]; ok {
		t.Fatal("removed Reasonix approval mode survived")
	}
	if _, ok := tools["remove_keep"]["enabled"]; !ok {
		t.Fatal("removing approval mode removed external fields")
	}
	if _, ok := tools["remove_entirely"]; ok {
		t.Fatal("approval-only entry should be removed when its policy is cleared")
	}
}

func TestNormalizePluginCommandLine(t *testing.T) {
	cases := []struct {
		name        string
		in          PluginEntry
		wantCommand string
		wantArgs    []string
		wantChanged bool
	}{
		{
			name:        "npx pasted with args",
			in:          PluginEntry{Name: "playwright", Command: "npx -y @playwright/mcp"},
			wantCommand: "npx",
			wantArgs:    []string{"-y", "@playwright/mcp"},
			wantChanged: true,
		},
		{
			name:        "custom command pasted with args",
			in:          PluginEntry{Name: "custom", Command: "custom-mcp --stdio"},
			wantCommand: "custom-mcp",
			wantArgs:    []string{"--stdio"},
			wantChanged: true,
		},
		{
			name:        "quoted command path",
			in:          PluginEntry{Name: "quoted", Command: `"C:\Program Files\nodejs\npx.cmd" -y @example/mcp`},
			wantCommand: `C:\Program Files\nodejs\npx.cmd`,
			wantArgs:    []string{"-y", "@example/mcp"},
			wantChanged: true,
		},
		{
			name:        "empty quoted arg preserved",
			in:          PluginEntry{Name: "empty", Command: `npx --token "" @example/mcp`},
			wantCommand: "npx",
			wantArgs:    []string{"--token", "", "@example/mcp"},
			wantChanged: true,
		},
		{
			name:        "quoted arg with spaces preserved",
			in:          PluginEntry{Name: "quoted-arg", Command: `npx --label "My Server" @example/mcp`},
			wantCommand: "npx",
			wantArgs:    []string{"--label", "My Server", "@example/mcp"},
			wantChanged: true,
		},
		{
			name:        "shell control syntax untouched",
			in:          PluginEntry{Name: "control", Command: `npx @example/mcp && rm -rf tmp`},
			wantCommand: "npx @example/mcp && rm -rf tmp",
			wantChanged: false,
		},
		{
			name:        "unquoted command path with spaces stays literal",
			in:          PluginEntry{Name: "literal", Command: `C:\Program Files\nodejs\npx.cmd`},
			wantCommand: `C:\Program Files\nodejs\npx.cmd`,
			wantChanged: false,
		},
		{
			name:        "remote entry untouched",
			in:          PluginEntry{Name: "remote", Type: "http", URL: "https://mcp.example.com/mcp", Command: "npx -y nope"},
			wantCommand: "npx -y nope",
			wantChanged: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := NormalizePluginCommandLine(tc.in)
			if changed != tc.wantChanged {
				t.Fatalf("changed = %v, want %v", changed, tc.wantChanged)
			}
			if got.Command != tc.wantCommand {
				t.Fatalf("command = %q, want %q", got.Command, tc.wantCommand)
			}
			if strings.Join(got.Args, "\x00") != strings.Join(tc.wantArgs, "\x00") {
				t.Fatalf("args = %v, want %v", got.Args, tc.wantArgs)
			}
		})
	}
}

func TestParseLegacyMCPSpecSplitsCustomCommandArgs(t *testing.T) {
	got, ok := parseLegacyMCPSpec("fs=custom-mcp --stdio")
	if !ok {
		t.Fatal("parseLegacyMCPSpec returned false")
	}
	if got.Name != "fs" || got.Command != "custom-mcp" || strings.Join(got.Args, "\x00") != "--stdio" {
		t.Fatalf("legacy custom MCP spec = %+v, want name fs command custom-mcp args [--stdio]", got)
	}
}

func TestUpsertPluginNormalizesPastedCommandLine(t *testing.T) {
	cfg := &Config{}
	if err := cfg.UpsertPlugin(PluginEntry{Name: "playwright", Command: "npx -y @playwright/mcp"}); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins[0].Command; got != "npx" {
		t.Fatalf("command = %q, want npx", got)
	}
	if got := cfg.Plugins[0].Args; len(got) != 2 || got[0] != "-y" || got[1] != "@playwright/mcp" {
		t.Fatalf("args = %v, want [-y @playwright/mcp]", got)
	}
}

func TestLoadMCPJSONAbsentAndMalformed(t *testing.T) {
	dir := t.TempDir()

	// Absent file: not an error, no entries.
	got, err := loadMCPJSON(filepath.Join(dir, "missing.json"))
	if err != nil || got != nil {
		t.Errorf("absent file: got (%v, %v), want (nil, nil)", got, err)
	}

	// Malformed file: an error so a typo surfaces instead of dropping servers.
	bad := filepath.Join(dir, mcpJSONFile)
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMCPJSON(bad); err == nil {
		t.Error("malformed .mcp.json: want error, got nil")
	}
}

func TestLoadMergesMCPJSON(t *testing.T) {
	// Point the user-config and home dirs at an empty temp dir so Load picks up
	// no global config, then chdir into a project dir holding both files.
	empty := t.TempDir()
	t.Setenv("HOME", empty)
	t.Setenv("XDG_CONFIG_HOME", empty)
	t.Chdir(t.TempDir())

	toml := `[[plugins]]
name = "shared"
command = "local-bin"
`
	if err := os.WriteFile("reasonix.toml", []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{ "mcpServers": {
  "shared": { "type": "http", "url": "https://override.example" },
  "extra":  { "command": "extra-bin", "auto_start": false }
} }`
	if err := os.WriteFile(mcpJSONFile, []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]PluginEntry{}
	for _, p := range cfg.Plugins {
		byName[p.Name] = p
	}
	if len(byName) != 2 {
		t.Fatalf("plugins = %+v, want shared + extra", cfg.Plugins)
	}
	if byName["shared"].Command != "local-bin" || byName["shared"].URL != "" {
		t.Errorf("reasonix.toml should win the collision, got %+v", byName["shared"])
	}
	if byName["extra"].Command != "extra-bin" {
		t.Errorf("extra not merged from .mcp.json, got %+v", byName["extra"])
	}
	if byName["extra"].AutoStart == nil || *byName["extra"].AutoStart {
		t.Errorf("extra auto_start=false not preserved, got %+v", byName["extra"].AutoStart)
	}
}

func TestLoadMergesPluginsAcrossTOMLSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("AppData", filepath.Join(root, "AppData")) // os.UserConfigDir reads AppData on Windows
	t.Chdir(t.TempDir())

	gpath := UserConfigPath()
	if gpath == "" {
		t.Fatal("UserConfigPath empty under isolated env")
	}
	if err := os.MkdirAll(filepath.Dir(gpath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gpath, []byte("[[plugins]]\nname = \"globalmcp\"\ncommand = \"global-bin\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("reasonix.toml", []byte("[[plugins]]\nname = \"projectmcp\"\ncommand = \"project-bin\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	sources := map[string]MCPConfigSource{}
	for _, p := range cfg.Plugins {
		names[p.Name] = true
		sources[p.Name] = p.Source
	}
	if !names["globalmcp"] || !names["projectmcp"] {
		t.Fatalf("a project reasonix.toml [[plugins]] dropped the global config's server; got %+v", cfg.Plugins)
	}
	if sources["globalmcp"] != MCPSourceUserConfig || sources["projectmcp"] != MCPSourceProjectConfig {
		t.Fatalf("plugin provenance = %+v", sources)
	}
}

func TestLoadProjectMCPPriorityIsReasonixThenMCPJSONThenGlobal(t *testing.T) {
	_, userConfig, _ := legacyHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`
[[plugins]]
name = "shared"
command = "global-mcp"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, mcpJSONFile), []byte(`{
  "mcpServers": {
    "shared": { "command": "project-json-mcp" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := pluginEntryByName(cfg.Plugins, "shared")
	if !ok || entry.Command != "project-json-mcp" || entry.Source != MCPSourceProjectMCPJSON {
		t.Fatalf("global + .mcp.json effective entry = %+v, want project .mcp.json", entry)
	}

	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[[plugins]]
name = "shared"
command = "project-reasonix-mcp"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok = pluginEntryByName(cfg.Plugins, "shared")
	if !ok || entry.Command != "project-reasonix-mcp" || entry.Source != MCPSourceProjectConfig {
		t.Fatalf("reasonix.toml + .mcp.json + global effective entry = %+v, want project reasonix.toml", entry)
	}
}

func TestUpsertPluginInSourcePreservesGlobalAndProjectBoundaries(t *testing.T) {
	_, userConfig, _ := legacyHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`
[[plugins]]
name = "global"
command = "global-old"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte(`
[[plugins]]
name = "project"
command = "project-old"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if path, err := UpsertPluginInSourceForRoot(root, PluginEntry{
		Name: "global", Command: "global-new", Source: MCPSourceUserConfig,
	}); err != nil || !samePath(path, userConfig) {
		t.Fatalf("upsert global path=%q err=%v, want %q", path, err, userConfig)
	}
	if path, err := UpsertPluginInSourceForRoot(root, PluginEntry{
		Name: "project", Command: "project-new", Source: MCPSourceProjectConfig,
	}); err != nil || !samePath(path, projectPath) {
		t.Fatalf("upsert project path=%q err=%v, want %q", path, err, projectPath)
	}

	globalCfg := LoadForEdit(userConfig)
	if entry, ok := pluginEntryByName(globalCfg.Plugins, "global"); !ok || entry.Command != "global-new" {
		t.Fatalf("global config entry = %+v, found=%v", entry, ok)
	}
	if _, ok := pluginEntryByName(globalCfg.Plugins, "project"); ok {
		t.Fatalf("project MCP leaked into global config: %+v", globalCfg.Plugins)
	}
	projectCfg := LoadForEdit(projectPath)
	if entry, ok := pluginEntryByName(projectCfg.Plugins, "project"); !ok || entry.Command != "project-new" {
		t.Fatalf("project config entry = %+v, found=%v", entry, ok)
	}
	if _, ok := pluginEntryByName(projectCfg.Plugins, "global"); ok {
		t.Fatalf("global MCP leaked into project config: %+v", projectCfg.Plugins)
	}
}

func TestRemoveEffectivePluginRevealsLowerPriorityDeclaration(t *testing.T) {
	_, userConfig, _ := legacyHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`
[[plugins]]
name = "shared"
command = "global-mcp"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(projectPath, []byte(`
[[plugins]]
name = "shared"
command = "project-reasonix-mcp"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(root, mcpJSONFile)
	if err := os.WriteFile(mcpPath, []byte(`{
  "mcpServers": {
    "shared": { "command": "project-json-mcp" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, ok, path, err := RemovePluginFromEffectiveSourceForRoot(root, "shared")
	if err != nil || !ok || removed.Source != MCPSourceProjectConfig || !samePath(path, projectPath) {
		t.Fatalf("remove project TOML = entry:%+v removed:%v path:%q err:%v", removed, ok, path, err)
	}
	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, found := pluginEntryByName(cfg.Plugins, "shared")
	if !found || entry.Source != MCPSourceProjectMCPJSON || entry.Command != "project-json-mcp" {
		t.Fatalf("after removing project TOML effective entry = %+v, found=%v", entry, found)
	}

	removed, ok, path, err = RemovePluginFromEffectiveSourceForRoot(root, "shared")
	if err != nil || !ok || removed.Source != MCPSourceProjectMCPJSON || !samePath(path, mcpPath) {
		t.Fatalf("remove project .mcp.json = entry:%+v removed:%v path:%q err:%v", removed, ok, path, err)
	}
	cfg, err = LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, found = pluginEntryByName(cfg.Plugins, "shared")
	if !found || entry.Source != MCPSourceUserConfig || entry.Command != "global-mcp" {
		t.Fatalf("after removing project sources effective entry = %+v, found=%v", entry, found)
	}
}

func TestLoadNormalizesTOMLPastedCommandLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())

	if err := os.WriteFile("reasonix.toml", []byte("[[plugins]]\nname = \"playwright\"\ncommand = \"npx -y @playwright/mcp\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("plugins = %+v", cfg.Plugins)
	}
	if cfg.Plugins[0].Command != "npx" {
		t.Fatalf("command = %q, want npx", cfg.Plugins[0].Command)
	}
	if got := cfg.Plugins[0].Args; len(got) != 2 || got[0] != "-y" || got[1] != "@playwright/mcp" {
		t.Fatalf("args = %v, want [-y @playwright/mcp]", got)
	}
}

func TestMergeMCPJSONPrecedence(t *testing.T) {
	// reasonix.toml already declares "shared" (stdio); .mcp.json offers a colliding
	// "shared" (http) plus a fresh "extra". reasonix.toml must win on the collision;
	// "extra" gets appended.
	cfg := &Config{Plugins: []PluginEntry{
		{Name: "shared", Command: "local-bin"},
	}}
	cfg.mergeMCPJSON([]PluginEntry{
		{Name: "shared", Type: "http", URL: "https://override.example"},
		{Name: "extra", Command: "extra-bin"},
	})

	if len(cfg.Plugins) != 2 {
		t.Fatalf("plugins = %+v, want 2 (shared kept, extra added)", cfg.Plugins)
	}
	if cfg.Plugins[0].Name != "shared" || cfg.Plugins[0].Command != "local-bin" || cfg.Plugins[0].URL != "" {
		t.Errorf("collision not won by reasonix.toml: %+v", cfg.Plugins[0])
	}
	if cfg.Plugins[1].Name != "extra" || cfg.Plugins[1].Command != "extra-bin" {
		t.Errorf("non-colliding entry not appended: %+v", cfg.Plugins[1])
	}
}

func TestClearPluginAuthenticationInSourceUsesMCPJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("AppData", filepath.Join(root, "AppData"))
	t.Chdir(t.TempDir())

	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("[[plugins]]\nname = \"global\"\ncommand = \"global-bin\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{
  "mcpServers": {
    "dida": {
      "type": "http",
      "url": "https://mcp.dida365.com/mcp?access_token=abc&workspace=main",
      "headers": { "Authorization": "Bearer ${DIDA_TOKEN}", "X-Org": "team" },
      "env": { "DIDA_TOKEN": "${DIDA_TOKEN}", "DEBUG": "1" }
    }
  }
}`
	if err := os.WriteFile(mcpJSONFile, []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}

	updated, changed, source, err := ClearPluginAuthenticationInSource("dida")
	if err != nil {
		t.Fatalf("ClearPluginAuthenticationInSource: %v", err)
	}
	if !changed {
		t.Fatal("ClearPluginAuthenticationInSource should report changed")
	}
	if source != mcpJSONFile {
		t.Fatalf("source = %q, want %q", source, mcpJSONFile)
	}
	if updated.URL != "https://mcp.dida365.com/mcp?workspace=main" {
		t.Fatalf("updated URL = %q", updated.URL)
	}

	userRaw, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(userRaw), "dida") {
		t.Fatalf("user config should not receive .mcp.json server:\n%s", userRaw)
	}
	entries, err := loadMCPJSON(mcpJSONFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one dida entry", entries)
	}
	got := entries[0]
	if got.URL != "https://mcp.dida365.com/mcp?workspace=main" {
		t.Fatalf(".mcp.json URL = %q", got.URL)
	}
	if _, ok := got.Headers["Authorization"]; ok {
		t.Fatalf("auth header should be removed: %+v", got.Headers)
	}
	if got.Headers["X-Org"] != "team" {
		t.Fatalf("ordinary header should be preserved: %+v", got.Headers)
	}
	if _, ok := got.Env["DIDA_TOKEN"]; ok {
		t.Fatalf("auth env should be removed: %+v", got.Env)
	}
	if got.Env["DEBUG"] != "1" {
		t.Fatalf("ordinary env should be preserved: %+v", got.Env)
	}
}

func TestClearPluginAuthenticationInSourcePrefersTOML(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("AppData", filepath.Join(root, "AppData"))
	t.Chdir(t.TempDir())

	if err := os.WriteFile("reasonix.toml", []byte(`[[plugins]]
name = "dida"
type = "http"
url = "https://reasonix.example/mcp?access_token=toml"
[plugins.headers]
Authorization = "Bearer ${TOML_TOKEN}"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	mcp := `{ "mcpServers": {
  "dida": {
    "type": "http",
    "url": "https://mcp-json.example/mcp?access_token=json",
    "headers": { "Authorization": "Bearer ${JSON_TOKEN}" }
  }
} }`
	if err := os.WriteFile(mcpJSONFile, []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}

	updated, changed, source, err := ClearPluginAuthenticationInSource("dida")
	if err != nil {
		t.Fatalf("ClearPluginAuthenticationInSource: %v", err)
	}
	if !changed {
		t.Fatal("ClearPluginAuthenticationInSource should report changed")
	}
	if source != "reasonix.toml" {
		t.Fatalf("source = %q, want reasonix.toml", source)
	}
	if updated.URL != "https://reasonix.example/mcp" {
		t.Fatalf("updated URL = %q", updated.URL)
	}

	projectRaw, err := os.ReadFile("reasonix.toml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(projectRaw), "access_token=toml") || strings.Contains(string(projectRaw), "Authorization") {
		t.Fatalf("reasonix.toml auth material should be removed:\n%s", projectRaw)
	}
	mcpRaw, err := os.ReadFile(mcpJSONFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mcpRaw), "access_token=json") {
		t.Fatalf(".mcp.json collision entry should be left untouched:\n%s", mcpRaw)
	}
}

func TestClearPluginAuthenticationInSourceForRootDoesNotFollowWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	rootA := t.TempDir()
	rootB := t.TempDir()
	write := func(root, token string) {
		t.Helper()
		raw := fmt.Sprintf(`[[plugins]]
name = "dida"
type = "http"
url = "https://example.test/mcp?access_token=%s&workspace=main"
`, token)
		if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(rootA, "root-a")
	write(rootB, "root-b")
	t.Chdir(rootB)

	updated, changed, source, err := ClearPluginAuthenticationInSourceForRoot(rootA, "dida")
	if err != nil {
		t.Fatalf("ClearPluginAuthenticationInSourceForRoot: %v", err)
	}
	if !changed || updated.URL != "https://example.test/mcp?workspace=main" {
		t.Fatalf("updated = %+v, changed = %v", updated, changed)
	}
	if want := filepath.Join(rootA, "reasonix.toml"); !samePath(source, want) {
		t.Fatalf("source = %q, want %q", source, want)
	}
	rootBRaw, err := os.ReadFile(filepath.Join(rootB, "reasonix.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootBRaw), "access_token=root-b") {
		t.Fatalf("non-target workspace was modified:\n%s", rootBRaw)
	}
}

func TestLoadLegacyMCP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	doc := `{
  "mcpServers": {
    "github":  { "command": "npx", "args": ["-y", "server-github"], "env": { "TOKEN": "x" } },
    "old":     { "command": "foo" },
    "remote":  { "type": "sse", "url": "https://x/sse", "headers": { "Authorization": "Bearer y" } }
  },
  "mcpDisabled": ["old"],
  "projects": { "/some/root": { "shellAllowed": [] } }
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadLegacyMCP(path)
	// "old" is in mcpDisabled and dropped; github + remote remain, name-sorted.
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(got), got)
	}
	if got[0].Name != "github" || got[1].Name != "remote" {
		t.Fatalf("names = %q, %q; want github, remote", got[0].Name, got[1].Name)
	}
	if got[0].Command != "npx" || got[0].Env["TOKEN"] != "x" {
		t.Errorf("github mapped wrong: %+v", got[0])
	}
	if got[1].Type != "sse" || got[1].URL != "https://x/sse" || got[1].Headers["Authorization"] != "Bearer y" {
		t.Errorf("remote mapped wrong: %+v", got[1])
	}

	doc = `{
  "mcp": [
    "memory=npx -y @modelcontextprotocol/server-memory",
    "remote=https://x/sse",
    "stream=streamable+https://x/http",
    "github=node dupe.js",
    "off=npx server-off",
    "uvx run anonymous-server"
  ],
  "mcpServers": { "github": { "command": "npx" } },
  "mcpEnv": { "memory": { "MEMORY_PATH": "/tmp/mem" } },
  "mcpDisabled": ["off"]
}`
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got = loadLegacyMCP(path)
	byName := map[string]PluginEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if m := byName["memory"]; m.Command != "npx" || m.Env["MEMORY_PATH"] != "/tmp/mem" {
		t.Errorf("legacy mcp string entry mapped wrong: %+v", m)
	}
	if r := byName["remote"]; r.Type != "sse" || r.URL != "https://x/sse" {
		t.Errorf("plain URL should map to SSE: %+v", r)
	}
	if s := byName["stream"]; s.Type != "http" || s.URL != "https://x/http" {
		t.Errorf("streamable+ URL should map to http: %+v", s)
	}
	if g := byName["github"]; g.Command != "npx" || len(g.Args) != 0 {
		t.Errorf("mcpServers should win the github name collision: %+v", g)
	}
	if a := byName["mcp-6"]; a.Command != "uvx" || len(a.Args) != 2 {
		t.Errorf("anonymous spec should get a synthesized name: %+v", a)
	}
	if _, hasOff := byName["off"]; hasOff || len(got) != 5 {
		t.Errorf("disabled entry should be skipped, got %d: %+v", len(got), got)
	}

	// Absent, malformed, and empty paths must not error — just yield nil, so a
	// stale legacy file can never block startup.
	if got := loadLegacyMCP(filepath.Join(dir, "nope.json")); got != nil {
		t.Errorf("absent file: got %+v, want nil", got)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadLegacyMCP(path); got != nil {
		t.Errorf("malformed file: got %+v, want nil", got)
	}
	if got := loadLegacyMCP(""); got != nil {
		t.Errorf("empty path: got %+v, want nil", got)
	}
}

func TestRemovePluginFromSourcesForRootRemovesEveryWritableDeclaration(t *testing.T) {
	_, userConfig, _ := legacyHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{userConfig, filepath.Join(root, "reasonix.toml")} {
		if err := os.WriteFile(path, []byte(`
[[plugins]]
name = "duplicate"
command = "duplicate-mcp"
`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mcpPath := filepath.Join(root, mcpJSONFile)
	if err := os.WriteFile(mcpPath, []byte(`{
  "mcpServers": {
    "duplicate": { "command": "duplicate-json" },
    "keep": { "command": "keep-json" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := RemovePluginFromSourcesForRoot(root, "duplicate")
	if err != nil {
		t.Fatalf("RemovePluginFromSourcesForRoot: %v", err)
	}
	if !removed {
		t.Fatal("RemovePluginFromSourcesForRoot reported no removal")
	}
	for _, path := range []string{userConfig, filepath.Join(root, "reasonix.toml")} {
		for _, p := range LoadForEdit(path).Plugins {
			if p.Name == "duplicate" {
				t.Fatalf("duplicate MCP survived in %s: %+v", path, p)
			}
		}
	}
	if _, found, err := LoadMCPJSONPlugin(mcpPath, "duplicate"); err != nil || found {
		t.Fatalf("duplicate .mcp.json entry survived: found=%v err=%v", found, err)
	}
	if _, found, err := LoadMCPJSONPlugin(mcpPath, "keep"); err != nil || !found {
		t.Fatalf("unrelated .mcp.json entry was lost: found=%v err=%v", found, err)
	}
}

func TestRemovePluginFromSourcesForRootPreflightsEverySource(t *testing.T) {
	_, userConfig, _ := legacyHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = `[[plugins]]
name = "duplicate"
command = "duplicate-mcp"
`
	if err := os.WriteFile(userConfig, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, mcpJSONFile), []byte(`{"mcpServers":`), 0o644); err != nil {
		t.Fatal(err)
	}

	if removed, err := RemovePluginFromSourcesForRoot(root, "duplicate"); err == nil || removed {
		t.Fatalf("RemovePluginFromSourcesForRoot = (%v, %v), want false and malformed .mcp.json error", removed, err)
	}
	got, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("user config changed before every source was validated:\n%s", got)
	}
}

func TestApplyConfigSourceEditsRollsBackEarlierWrites(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.toml")
	second := filepath.Join(dir, "second.toml")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	firstEdit, err := newConfigSourceEdit(first, func() error {
		return os.WriteFile(first, []byte("after\n"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	secondEdit, err := newConfigSourceEdit(second, func() error {
		return errors.New("publish failed")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := applyConfigSourceEdits([]configSourceEdit{firstEdit, secondEdit}); err == nil {
		t.Fatal("applyConfigSourceEdits unexpectedly succeeded")
	}
	for _, path := range []string{first, second} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "before\n" {
			t.Fatalf("%s was not rolled back: %q", path, got)
		}
	}
}
