package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
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

func isolateDesktopUserDirs(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	xdg := filepath.Join(home, ".config")
	appData := filepath.Join(home, "AppData")
	for _, dir := range []string{xdg, appData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("AppData", appData)
	return home
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
	isolateDesktopUserDirs(t)

	got := NewApp().Effort()
	if !got.Supported || got.Current != "auto" || got.Default != "high" || !hasLevel(got.Levels, "auto") {
		t.Fatalf("pre-startup Effort() = %+v, want auto with DeepSeek default high", got)
	}
}

func TestBeforeCloseAllowsSystemQuitWhenBackgroundCloseEnabled(t *testing.T) {
	isolateDesktopUserDirs(t)
	consumeSystemQuitRequested()
	t.Cleanup(func() { consumeSystemQuitRequested() })

	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.SetDesktopCloseBehavior("background"); err != nil {
		t.Fatal(err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	markSystemQuitRequested()
	if prevent := NewApp().beforeClose(context.Background()); prevent {
		t.Fatal("system quit should bypass background close-to-tray behavior")
	}
	if consumeSystemQuitRequested() {
		t.Fatal("system quit marker should be consumed by beforeClose")
	}
}

func TestEmitReadyInvokesReadyHook(t *testing.T) {
	app := NewApp()
	var calls int32
	app.readyHook = func() {
		atomic.AddInt32(&calls, 1)
	}

	app.emitReady(nil)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("ready hook calls = %d, want 1", got)
	}
}

func TestSetEffortPersistsAndAutoClears(t *testing.T) {
	isolateDesktopUserDirs(t)

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

func TestSettingsUsesUserDesktopPreferencesNotProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[desktop]
language = "zh"
theme = "light"
theme_style = "glacier"
close_behavior = "quit"
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("set desktop language: %v", err)
	}
	if err := userCfg.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("set desktop appearance: %v", err)
	}
	if err := userCfg.SetDesktopCloseBehavior("background"); err != nil {
		t.Fatalf("set desktop close behavior: %v", err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	got := NewApp().Settings()
	if got.DesktopLanguage != "en" || got.DesktopTheme != "dark" || got.DesktopThemeStyle != "graphite" || got.CloseBehavior != "background" {
		t.Fatalf("desktop settings = lang:%q theme:%q style:%q close:%q, want user-level desktop prefs", got.DesktopLanguage, got.DesktopTheme, got.DesktopThemeStyle, got.CloseBehavior)
	}
}

func TestSettingsSeedsMissingUserConfigFromLegacyProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "legacy-provider/legacy-model"

[desktop]
language = "zh"
theme = "light"
theme_style = "glacier"
close_behavior = "quit"
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir project: %v", err)
	}

	app := NewApp()
	got := app.Settings()
	if got.ConfigPath != config.UserConfigPath() {
		t.Fatalf("Settings configPath = %q, want user config %q", got.ConfigPath, config.UserConfigPath())
	}
	if got.DefaultModel != "legacy-provider/legacy-model" || got.DesktopLanguage != "zh" || got.DesktopTheme != "light" || got.DesktopThemeStyle != "glacier" || got.CloseBehavior != "quit" {
		t.Fatalf("Settings did not seed from legacy project config: %+v", got)
	}
	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("Settings() should not write user config before an edit, stat err = %v", err)
	}
	if err := app.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("SetDesktopLanguage: %v", err)
	}
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if userCfg.DesktopLanguage() != "en" || userCfg.DesktopTheme() != "light" || userCfg.DesktopThemeStyle() != "glacier" || userCfg.DesktopCloseBehavior() != "quit" {
		t.Fatalf("saved user config did not preserve seeded desktop prefs: lang:%q theme:%q style:%q close:%q", userCfg.DesktopLanguage(), userCfg.DesktopTheme(), userCfg.DesktopThemeStyle(), userCfg.DesktopCloseBehavior())
	}
}

func TestMigrateDesktopPreferencesDoesNotOverwriteExistingConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("set desktop language: %v", err)
	}
	if err := userCfg.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("set desktop appearance: %v", err)
	}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	if err := NewApp().MigrateDesktopPreferences("zh", "light", "glacier"); err != nil {
		t.Fatalf("migrate desktop preferences: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	if got.DesktopLanguage() != "en" || got.DesktopTheme() != "dark" || got.DesktopThemeStyle() != "graphite" {
		t.Fatalf("desktop prefs after migration = lang:%q theme:%q style:%q, want existing config preserved", got.DesktopLanguage(), got.DesktopTheme(), got.DesktopThemeStyle())
	}
}

func TestSetEffortRebuildsController(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
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
	isolateDesktopUserDirs(t)

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

func TestFileRefsUseActiveTabWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := t.TempDir()
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(launchRoot, "launch-only.txt"), []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "frontend", "wailsjs", "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	projectFile := filepath.Join(projectRoot, "frontend", "wailsjs", "runtime", "runtime.js")
	if err := os.WriteFile(projectFile, []byte("right workspace"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(launchRoot); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	tab := &WorkspaceTab{ID: "project", Scope: "project", WorkspaceRoot: projectRoot}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID

	listed := app.ListDir("")
	if !hasDirEntry(listed, "frontend") {
		t.Fatalf("ListDir should list active project root, got %+v", listed)
	}
	if hasDirEntry(listed, "launch-only.txt") {
		t.Fatalf("ListDir leaked launch cwd entries, got %+v", listed)
	}

	found := app.SearchFileRefs("runtime.js")
	if !hasDirEntry(found, "frontend/wailsjs/runtime/runtime.js") {
		t.Fatalf("SearchFileRefs should search active project root, got %+v", found)
	}
	preview := app.ReadFile("frontend/wailsjs/runtime/runtime.js")
	if preview.Err != "" || preview.Body != "right workspace" {
		t.Fatalf("ReadFile active project preview = %+v, want project file", preview)
	}
}

func TestDeleteSessionRejectsActiveRelativePath(t *testing.T) {
	isolateDesktopUserDirs(t)

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

func TestDeleteSessionRejectsInactiveOpenTab(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	activePath := filepath.Join(dir, "active.jsonl")
	inactivePath := filepath.Join(dir, "inactive.jsonl")
	otherPath := filepath.Join(dir, "other.jsonl")
	for _, path := range []string{activePath, inactivePath, otherPath} {
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write session %s: %v", path, err)
		}
	}

	activeCtrl := control.New(control.Options{SessionDir: dir, SessionPath: activePath, Label: "active"})
	inactiveCtrl := control.New(control.Options{SessionDir: dir, SessionPath: inactivePath, Label: "inactive"})
	defer activeCtrl.Close()
	defer inactiveCtrl.Close()

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"active":   {ID: "active", Scope: "global", Ctrl: activeCtrl, Ready: true},
			"inactive": {ID: "inactive", Scope: "global", Ctrl: inactiveCtrl, Ready: true},
		},
		tabOrder:    []string{"active", "inactive"},
		activeTabID: "active",
	}

	if err := app.DeleteSession(filepath.Base(inactivePath)); err != errActiveSession {
		t.Fatalf("DeleteSession(inactive open basename) error = %v, want errActiveSession", err)
	}
	if _, err := os.Stat(inactivePath); err != nil {
		t.Fatalf("inactive open session should remain: %v", err)
	}

	sessions := app.ListSessions()
	current := map[string]bool{}
	open := map[string]bool{}
	for _, s := range sessions {
		current[filepath.Base(s.Path)] = s.Current
		open[filepath.Base(s.Path)] = s.Open
	}
	if !current[filepath.Base(activePath)] {
		t.Fatalf("ListSessions should mark active session current, got %#v", current)
	}
	if current[filepath.Base(inactivePath)] {
		t.Fatalf("ListSessions should not mark inactive open session current, got %#v", current)
	}
	if current[filepath.Base(otherPath)] {
		t.Fatalf("ListSessions marked unopened session current, got %#v", current)
	}
	if !open[filepath.Base(activePath)] || !open[filepath.Base(inactivePath)] {
		t.Fatalf("ListSessions should mark active and inactive open sessions open, got %#v", open)
	}
	if open[filepath.Base(otherPath)] {
		t.Fatalf("ListSessions marked unopened session open, got %#v", open)
	}
}

type appendingDesktopRunner struct {
	session *agent.Session
	started chan string
}

func (r *appendingDesktopRunner) Run(_ context.Context, input string) error {
	r.started <- input
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input})
	r.session.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	return nil
}

func TestForkCreatesActiveTabWithoutSwitchingSourceController(t *testing.T) {
	isolateDesktopUserDirs(t)

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte("[codegraph]\nenabled = false\n"), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := agent.NewSessionPath(dir, "test")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &appendingDesktopRunner{session: sess, started: make(chan string, 2)}
	ctrl := control.New(control.Options{
		Runner:        runner,
		Executor:      exec,
		Sink:          event.Discard,
		SessionDir:    dir,
		SessionPath:   path,
		Label:         "test",
		WorkspaceRoot: workspace,
	})
	app := NewApp()
	app.setTestCtrl(ctrl, "deepseek/test")
	app.tabs["test"].Scope = "project"
	app.tabs["test"].WorkspaceRoot = workspace
	app.tabs["test"].TopicID = "topic_source"
	app.tabs["test"].TopicTitle = "Source topic"
	defer ctrl.Close()

	ctrl.Submit("first")
	<-runner.started
	waitNotRunning(t, ctrl)
	ctrl.Submit("second")
	<-runner.started
	waitNotRunning(t, ctrl)
	if got := len(ctrl.History()); got != 5 {
		t.Fatalf("source history len before fork = %d, want 5", got)
	}

	meta, err := app.Fork(1)
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if !meta.Active || meta.ID == "" || meta.ID == "test" {
		t.Fatalf("fork meta = %+v, want a new active tab", meta)
	}
	if got := app.activeTabID; got != meta.ID {
		t.Fatalf("active tab = %q, want fork tab %q", got, meta.ID)
	}
	if got := ctrl.SessionPath(); got != path {
		t.Fatalf("source controller session path = %q, want %q", got, path)
	}
	if got := len(ctrl.History()); got != 5 {
		t.Fatalf("source history len after fork = %d, want 5", got)
	}
	if got, want := meta.TopicTitle, "Source topic · 分叉"; got != want {
		t.Fatalf("fork topic title = %q, want %q", got, want)
	}

	var forkPath string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read session dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		if candidate == path {
			continue
		}
		m, ok, err := agent.LoadBranchMeta(candidate)
		if err != nil {
			t.Fatalf("load fork meta: %v", err)
		}
		if ok && m.TopicID == meta.TopicID {
			forkPath = candidate
			if m.ParentID != agent.BranchID(path) || m.ForkTurn != 1 || m.ForkMessageIndex != 3 {
				t.Fatalf("fork branch meta = %+v, want parent %q turn 1 index 3", m, agent.BranchID(path))
			}
			if m.Scope != "project" || m.WorkspaceRoot != workspace || m.TopicTitle != "Source topic · 分叉" {
				t.Fatalf("fork topic meta = %+v", m)
			}
		}
	}
	if forkPath == "" {
		t.Fatalf("fork session with topic %q not found in %s", meta.TopicID, dir)
	}
}

func TestCapabilitiesShowsLazyMCPAsDeferredNotDisabled(t *testing.T) {
	isolateDesktopUserDirs(t)
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
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	t.Chdir(dir)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: host}), "")
	defer app.activeCtrl().Close()

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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: host}), "")
	defer app.activeCtrl().Close()

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
	isolateDesktopUserDirs(t)
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
	userCfg := config.LoadForEdit(config.UserConfigPath())
	userPlugin, ok := findPluginEntry(userCfg.Plugins, "playwright")
	if !ok {
		t.Fatalf("playwright should be migrated to user config: %+v", userCfg.Plugins)
	}
	if userPlugin.Command != "node" || userPlugin.Env["TOKEN"] != "${PLAYWRIGHT_TOKEN}" {
		t.Fatalf("user plugin after migration = %+v", userPlugin)
	}
	projectCfg := config.LoadForEdit(filepath.Join(dir, "reasonix.toml"))
	if _, ok := findPluginEntry(projectCfg.Plugins, "playwright"); ok {
		t.Fatalf("project plugin should be removed after desktop migration: %+v", projectCfg.Plugins)
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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

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
	if !mcpFailed(app.activeCtrl(), "broken") {
		t.Fatalf("Host.Failures() = %+v, want broken failure recorded", app.activeCtrl().Host().Failures())
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
	isolateDesktopUserDirs(t)
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
	userCfg := config.LoadForEdit(config.UserConfigPath())
	userPlugin, ok := findPluginEntry(userCfg.Plugins, "broken")
	if !ok {
		t.Fatalf("broken should be migrated to user config: %+v", userCfg.Plugins)
	}
	if userPlugin.Tier != "background" {
		t.Fatalf("user plugin tier = %q, want background", userPlugin.Tier)
	}
	projectCfg := config.LoadForEdit(filepath.Join(dir, "reasonix.toml"))
	if _, ok := findPluginEntry(projectCfg.Plugins, "broken"); ok {
		t.Fatalf("project plugin should be removed after desktop migration: %+v", projectCfg.Plugins)
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
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

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
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if !userCfg.Codegraph.Enabled {
		t.Fatal("user codegraph enabled = false, want true after selecting a startup tier")
	}
	if got := userCfg.Codegraph.Tier; got != "background" {
		t.Fatalf("user codegraph tier = %q, want background", got)
	}
	if !mcpFailed(app.activeCtrl(), "codegraph") {
		t.Fatalf("Host.Failures() = %+v, want codegraph failure recorded for missing runtime", app.activeCtrl().Host().Failures())
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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

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
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if userCfg.Codegraph.Enabled {
		t.Fatal("user codegraph enabled = true, want false after disabling")
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
	isolateDesktopUserDirs(t)
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
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()
	recordMCPFailure(app.activeCtrl(), config.PluginEntry{
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
