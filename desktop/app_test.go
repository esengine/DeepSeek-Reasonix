package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
)

// setTestCtrl creates a minimal workspace tab (if needed) and sets its
// controller, so tests don't depend on the old App.ctrl field.
func (a *App) setTestCtrl(ctrl *control.Controller, model string) {
	if len(a.tabs) == 0 {
		tab := &WorkspaceTab{
			ID:          "test",
			Scope:       "global",
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		}
		a.tabs = map[string]*WorkspaceTab{"test": tab}
		a.activeTabID = "test"
	}
	tab := a.tabs["test"]
	tab.Ctrl = ctrl
	tab.model = model
}

func TestCommandsIncludesEffortNotThinking(t *testing.T) {
	app := NewApp()
	cmds := app.Commands()
	if !hasCommand(cmds, "effort") {
		t.Fatalf("Commands() should include effort: %+v", cmds)
	}
	if hasCommand(cmds, "thinking") {
		t.Fatalf("Commands() should not include thinking: %+v", cmds)
	}
}

func TestEffortDefaultsBeforeStartup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got := NewApp().Effort()
	if !got.Supported || got.Current != "auto" || got.Default != "high" || !hasLevel(got.Levels, "auto") {
		t.Fatalf("pre-startup Effort() = %+v, want auto with DeepSeek default high", got)
	}
}

func TestSetEffortPersistsAndAutoClears(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	app := NewApp()
	if err := app.SetEffort("max"); err != nil {
		t.Fatalf("SetEffort(max): %v", err)
	}
	if got := app.Effort().Current; got != "max" {
		t.Fatalf("Effort current = %q, want max", got)
	}
	if err := app.SetEffort("auto"); err != nil {
		t.Fatalf("SetEffort(auto): %v", err)
	}
	if got := app.Effort().Current; got != "auto" {
		t.Fatalf("Effort current = %q, want auto", got)
	}
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(body), `effort      = "max"`) {
		t.Fatalf("auto should clear explicit max effort:\n%s", body)
	}
}

func TestSetEffortRebuildsController(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	app := NewApp()
	app.ctx = context.Background()
	old := control.New(control.Options{Label: "old-controller"})
	app.setTestCtrl(old, "deepseek-flash/deepseek-v4-flash")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if err := app.SetEffort("max"); err != nil {
		t.Fatalf("SetEffort(max): %v", err)
	}
	if c := app.activeCtrl(); c == nil {
		t.Fatal("SetEffort should leave a rebuilt controller")
	}
	if c := app.activeCtrl(); c == old {
		t.Fatal("SetEffort should rebuild the active controller so the provider sees the new effort")
	}
	if got := app.Effort().Current; got != "max" {
		t.Fatalf("Effort current = %q, want max", got)
	}
}

func TestSetEffortRejectsRunningTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Runner: runner}), "")
	app.activeCtrl().Submit("work")
	<-runner.started

	err := app.SetEffort("max")
	if err == nil || !strings.Contains(err.Error(), "finish or cancel") {
		t.Fatalf("SetEffort while running error = %v, want finish/cancel guard", err)
	}

	close(runner.release)
	waitNotRunning(t, app.activeCtrl())
}

func TestGlobalWorkspaceRootUsesUserConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root, err := ensureGlobalWorkspaceRoot()
	if err != nil {
		t.Fatalf("ensure global workspace root: %v", err)
	}
	userConfig, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	want := filepath.Join(userConfig, "reasonix", "global-workspace")
	if root != want {
		t.Fatalf("global workspace root = %q, want %q", root, want)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("global workspace root should be an existing directory, info=%v err=%v", info, err)
	}
}

func TestBuildTabControllerEmitsReadyAfterTabReady(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(`
default_model = "test-model"

[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	tab := app.createTabEntryWithID("project", workspace, "", "tab_ready")
	app.tabs[tab.ID] = tab
	app.activeTabID = tab.ID
	var readyEvents int
	var readyAtEmit bool
	app.readyHook = func() {
		readyEvents++
		app.mu.RLock()
		readyAtEmit = tab.Ready
		app.mu.RUnlock()
	}

	app.buildTabController(tab)
	defer func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	}()

	if !tab.Ready {
		t.Fatal("tab should be ready after build")
	}
	if readyEvents != 1 {
		t.Fatalf("ready events = %d, want 1", readyEvents)
	}
	if !readyAtEmit {
		t.Fatal("ready event should be emitted after tab.Ready is true")
	}
}

func TestModelsLoadActiveTabWorkspaceConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CWD_MODEL_KEY", "cwd-key")
	t.Setenv("WORKSPACE_MODEL_KEY", "workspace-key")

	cwd := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "reasonix.toml"), []byte(`
default_model = "cwd-provider"

[codegraph]
enabled = false

[[providers]]
name = "cwd-provider"
kind = "openai"
base_url = "https://cwd.invalid"
model = "cwd-model"
api_key_env = "CWD_MODEL_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(`
default_model = "workspace-provider"

[codegraph]
enabled = false

[[providers]]
name = "workspace-provider"
kind = "openai"
base_url = "https://workspace.invalid"
model = "workspace-model"
api_key_env = "WORKSPACE_MODEL_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	app := NewApp()
	tab := app.createTabEntryWithID("project", workspace, "", "tab_models")
	tab.model = "workspace-provider/workspace-model"
	app.tabs[tab.ID] = tab
	app.activeTabID = tab.ID

	models := app.Models()
	if !hasModelRef(models, "workspace-provider/workspace-model") {
		t.Fatalf("Models() should include active workspace provider, got %+v", models)
	}
	if hasModelRef(models, "cwd-provider/cwd-model") {
		t.Fatalf("Models() should not load provider from process cwd, got %+v", models)
	}
}

func hasModelRef(models []ModelInfo, ref string) bool {
	for _, m := range models {
		if m.Ref == ref {
			return true
		}
	}
	return false
}

func TestSearchFileRefsFindsNestedBasename(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "frontend", "wailsjs", "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "wailsjs", "runtime", "runtime.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "runtime.js"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	got := (&App{}).SearchFileRefs("runtime.js")
	if !hasDirEntry(got, "frontend/wailsjs/runtime/runtime.js") {
		t.Fatalf("SearchFileRefs(runtime.js) should find nested workspace file, got %+v", got)
	}
	if hasDirEntry(got, "node_modules/pkg/runtime.js") {
		t.Fatalf("SearchFileRefs should skip node_modules noise, got %+v", got)
	}
}

func TestDeleteSessionRejectsActiveRelativePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "active.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test"}), "")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if err := app.DeleteSession(filepath.Base(path)); err != errActiveSession {
		t.Fatalf("DeleteSession(active basename) error = %v, want errActiveSession", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active session should remain: %v", err)
	}
}

func TestCapabilitiesShowsLazyMCPAsDeferredNotDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "playwright" {
			if s.Status != "deferred" {
				t.Fatalf("lazy MCP status = %q, want deferred; server = %+v", s.Status, s)
			}
			return
		}
	}
	t.Fatalf("playwright MCP missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesShowsDefaultCodegraphDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "codegraph" {
			if s.Status != "disabled" {
				t.Fatalf("codegraph status = %q, want disabled; server = %+v", s.Status, s)
			}
			if !s.BuiltIn || !s.Configured {
				t.Fatalf("codegraph builtIn/configured = %v/%v, want true/true; server = %+v", s.BuiltIn, s.Configured, s)
			}
			if s.AutoStart {
				t.Fatalf("codegraph autoStart = true, want false; server = %+v", s)
			}
			if s.Tier != "lazy" {
				t.Fatalf("codegraph tier = %q, want lazy; server = %+v", s.Tier, s)
			}
			return
		}
	}
	t.Fatalf("codegraph missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesMarksDeferredRemoteMCPAuthPossible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "dida"
type = "http"
url = "https://mcp.dida365.com"
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "dida" {
			if s.Status != "deferred" || s.AuthStatus != "possible" || s.AuthURL != "https://mcp.dida365.com" {
				t.Fatalf("dida auth diagnosis = %+v", s)
			}
			return
		}
	}
	t.Fatalf("dida MCP missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesDoesNotMarkRemoteMCPWithAuthHeaderPossible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "stripe"
type = "http"
url = "https://mcp.stripe.com"
headers = { Authorization = "Bearer ${STRIPE_TOKEN}" }
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "stripe" {
			if s.AuthStatus != "none" {
				t.Fatalf("stripe auth status = %q, want none; server = %+v", s.AuthStatus, s)
			}
			return
		}
	}
	t.Fatalf("stripe MCP missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesMarksAuthFailureRequired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "figma"
type = "http"
url = "https://mcp.figma.com/mcp"
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	host := plugin.NewHost()
	host.RecordFailure(plugin.Spec{Name: "figma", Type: "http", URL: "https://mcp.figma.com/mcp"}, errors.New("connect: 401 unauthorized"))
	app := NewApp()
	ctrl := control.New(control.Options{Host: host})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "figma" {
			if s.Status != "failed" || s.AuthStatus != "required" || s.AuthURL != "https://mcp.figma.com/mcp" {
				t.Fatalf("figma auth diagnosis = %+v", s)
			}
			return
		}
	}
	t.Fatalf("figma MCP missing from Capabilities: %+v", view.Servers)
}

func TestClearMCPServerAuthenticationClearsConfigAndFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "figma"
type = "http"
url = "https://mcp.figma.com/mcp?access_token=abc&workspace=main"
headers = { Authorization = "Bearer ${FIGMA_TOKEN}", "X-Org" = "team" }
env = { FIGMA_TOKEN = "${FIGMA_TOKEN}", DEBUG = "1" }
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	host := plugin.NewHost()
	host.RecordFailure(plugin.Spec{Name: "figma", Type: "http", URL: "https://mcp.figma.com/mcp"}, errors.New("connect: 401 unauthorized"))
	app := NewApp()
	ctrl := control.New(control.Options{Host: host})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	if err := app.ClearMCPServerAuthentication("figma"); err != nil {
		t.Fatalf("ClearMCPServerAuthentication: %v", err)
	}
	if failures := host.Failures(); len(failures) != 0 {
		t.Fatalf("failure should be cleared: %+v", failures)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Plugins[0]
	if p.URL != "https://mcp.figma.com/mcp?workspace=main" {
		t.Fatalf("url = %q", p.URL)
	}
	if _, ok := p.Headers["Authorization"]; ok {
		t.Fatalf("auth header should be removed: %v", p.Headers)
	}
	if p.Headers["X-Org"] != "team" {
		t.Fatalf("ordinary header should be preserved: %v", p.Headers)
	}
	if _, ok := p.Env["FIGMA_TOKEN"]; ok {
		t.Fatalf("auth env should be removed: %v", p.Env)
	}
	if p.Env["DEBUG"] != "1" {
		t.Fatalf("ordinary env should be preserved: %v", p.Env)
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "figma" {
			if s.Status != "deferred" || s.AuthStatus != "possible" {
				t.Fatalf("figma should return to deferred possible auth: %+v", s)
			}
			return
		}
	}
	t.Fatalf("figma MCP missing from Capabilities: %+v", view.Servers)
}

func TestUpdateMCPServerKeepsLazyMCPDeferred(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp"]
env = { TOKEN = "${PLAYWRIGHT_TOKEN}" }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if err := app.UpdateMCPServer("playwright", MCPServerInput{
		Name:      "playwright",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"server.js"},
		Tier:      "lazy",
	}); err != nil {
		t.Fatalf("UpdateMCPServer: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins[0].Command; got != "node" {
		t.Fatalf("updated command = %q, want node", got)
	}
	if got := cfg.Plugins[0].Env["TOKEN"]; got != "${PLAYWRIGHT_TOKEN}" {
		t.Fatalf("env TOKEN = %q, want preserved env", got)
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "playwright" {
			if s.Status != "deferred" {
				t.Fatalf("updated lazy MCP status = %q, want deferred; server = %+v", s.Status, s)
			}
			if s.Command != "node" || len(s.Args) != 1 || s.Args[0] != "server.js" {
				t.Fatalf("server command not refreshed: %+v", s)
			}
			return
		}
	}
	t.Fatalf("playwright MCP missing from Capabilities: %+v", view.Servers)
}

func TestUpdateMCPServerRecordsReconnectFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "broken"
command = "npx"
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	if err := app.UpdateMCPServer("broken", MCPServerInput{
		Name:      "broken",
		Transport: "stdio",
		Command:   "reasonix-missing-mcp-binary",
		Tier:      "background",
	}); err != nil {
		t.Fatalf("UpdateMCPServer should persist config even when reconnect fails: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins[0].Command; got != "reasonix-missing-mcp-binary" {
		t.Fatalf("updated command = %q, want missing binary", got)
	}
	if got := cfg.Plugins[0].Tier; got != "background" {
		t.Fatalf("updated tier = %q, want background", got)
	}
	if !mcpFailed(ctrl, "broken") {
		t.Fatalf("Host.Failures() = %+v, want broken failure recorded", ctrl.Host().Failures())
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "broken" {
			if s.Status != "failed" {
				t.Fatalf("server status = %q, want failed; server = %+v", s.Status, s)
			}
			if s.Command != "reasonix-missing-mcp-binary" || s.Tier != "background" {
				t.Fatalf("server config not refreshed after failed reconnect: %+v", s)
			}
			return
		}
	}
	t.Fatalf("broken MCP missing from Capabilities: %+v", view.Servers)
}

func TestSetMCPServerTierRecordsConnectFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "broken"
command = "reasonix-missing-mcp-binary"
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if err := app.SetMCPServerTier("broken", "background"); err != nil {
		t.Fatalf("SetMCPServerTier should persist tier even when immediate connect fails: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins[0].Tier; got != "background" {
		t.Fatalf("saved tier = %q, want background", got)
	}
	if !mcpFailed(app.activeCtrl(), "broken") {
		t.Fatalf("Host.Failures() = %+v, want broken failure recorded", app.activeCtrl().Host().Failures())
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "broken" {
			if s.Status != "failed" {
				t.Fatalf("server status = %q, want failed; server = %+v", s.Status, s)
			}
			if s.Tier != "background" {
				t.Fatalf("server tier = %q, want background so radio selection does not jump back", s.Tier)
			}
			return
		}
	}
	t.Fatalf("broken MCP missing from Capabilities: %+v", view.Servers)
}

func TestSetMCPServerTierPersistsCodegraphConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("REASONIX_CACHE_DIR", t.TempDir()) // isolate the codegraph bundle cache so Resolve fails deterministically
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false
auto_install = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	if err := app.SetMCPServerTier("codegraph", "background"); err != nil {
		t.Fatalf("SetMCPServerTier(codegraph): %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Codegraph.Enabled {
		t.Fatal("codegraph enabled = false, want true after selecting a startup tier")
	}
	if got := cfg.Codegraph.Tier; got != "background" {
		t.Fatalf("codegraph tier = %q, want background", got)
	}
	if !mcpFailed(ctrl, "codegraph") {
		t.Fatalf("Host.Failures() = %+v, want codegraph failure recorded for missing runtime", ctrl.Host().Failures())
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "codegraph" {
			if s.Status != "failed" {
				t.Fatalf("codegraph status = %q, want failed; server = %+v", s.Status, s)
			}
			if !s.BuiltIn || !s.Configured || s.Tier != "background" || !s.AutoStart {
				t.Fatalf("codegraph view did not preserve built-in config: %+v", s)
			}
			return
		}
	}
	t.Fatalf("codegraph missing from Capabilities: %+v", view.Servers)
}

func TestSetMCPServerEnabledPersistsCodegraphOff(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = true
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	if err := app.SetMCPServerEnabled("codegraph", false); err != nil {
		t.Fatalf("SetMCPServerEnabled(codegraph,false): %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Codegraph.Enabled {
		t.Fatal("codegraph enabled = true, want false after disabling")
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "codegraph" {
			if s.Status != "disabled" || s.AutoStart {
				t.Fatalf("codegraph disabled view = %+v, want disabled with autoStart=false", s)
			}
			return
		}
	}
	t.Fatalf("codegraph missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesKeepsFailedMCPConfiguredTierAfterRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[[plugins]]
name = "broken"
command = "reasonix-missing-mcp-binary"
tier = "eager"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()
	recordMCPFailure(ctrl, config.PluginEntry{
		Name:    "broken",
		Command: "reasonix-missing-mcp-binary",
		Tier:    "eager",
	}, errors.New("connect: missing binary"))

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "broken" {
			if s.Status != "failed" {
				t.Fatalf("server status = %q, want failed; server = %+v", s.Status, s)
			}
			if s.Tier != "eager" {
				t.Fatalf("server tier = %q, want eager so failed UI preserves the configured selection", s.Tier)
			}
			if !s.Configured {
				t.Fatalf("server configured = false, want true; server = %+v", s)
			}
			return
		}
	}
	t.Fatalf("broken MCP missing from Capabilities: %+v", view.Servers)
}

type blockingRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingRunner) Run(ctx context.Context, _ string) error {
	close(r.started)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return nil
	}
}

func waitNotRunning(t *testing.T, ctrl *control.Controller) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("controller still running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func hasLevel(levels []string, want string) bool {
	for _, level := range levels {
		if level == want {
			return true
		}
	}
	return false
}

func hasCommand(cmds []CommandInfo, name string) bool {
	for _, cmd := range cmds {
		if cmd.Name == name {
			return true
		}
	}
	return false
}

func hasDirEntry(entries []DirEntry, name string) bool {
	for _, entry := range entries {
		if entry.Name == name {
			return true
		}
	}
	return false
}
