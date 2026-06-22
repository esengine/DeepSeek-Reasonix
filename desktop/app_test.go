package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func desktopMCPHTTPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "h", "version": "0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "greet",
				"description": "Greet someone.",
				"inputSchema": map[string]any{"type": "object"},
			}}}
		default:
			result = map[string]any{}
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": *req.ID, "result": result}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// setTestCtrl creates a minimal workspace tab (if needed) and sets its
// controller, so tests don't depend on the old App.ctrl field.
func (a *App) setTestCtrl(ctrl control.SessionAPI, model string) {
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
	a.bindControllerDisplayRecorder(ctrl)
	tab.model = model
}

func isolateDesktopUserDirs(t *testing.T) string {
	t.Helper()
	home := robustTempDir(t)
	xdg := filepath.Join(home, ".config")
	appData := filepath.Join(home, "AppData")
	for _, dir := range []string{xdg, appData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("AppData", appData)
	return home
}

func providerNamesFromView(providers []ProviderView) []string {
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		out = append(out, p.Name)
	}
	return out
}

func modelRefsFromView(models []ModelInfo) map[string]bool {
	out := map[string]bool{}
	for _, m := range models {
		out[m.Ref] = true
	}
	return out
}

type desktopFakeTool struct {
	name string
}

func (t desktopFakeTool) Name() string { return t.name }

func (desktopFakeTool) Description() string { return "fake desktop tool" }

func (desktopFakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (desktopFakeTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

func (desktopFakeTool) ReadOnly() bool { return true }

type desktopAskRuntimeRunner struct {
	ask func(context.Context) error
}

func (r *desktopAskRuntimeRunner) Run(ctx context.Context, _ string) error {
	if r.ask == nil {
		return nil
	}
	return r.ask(ctx)
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

func TestMetaForTabIncludesWorkspaceContext(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateDesktopUserDirs(t)

	repo := t.TempDir()
	configuredSandboxRoot := filepath.Join(t.TempDir(), "sandbox")
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Sandbox.WorkspaceRoot = configuredSandboxRoot
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	runGit(t, "init")
	runGit(t, "checkout", "-b", "feature/meta")

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {
		ID:            "tab-1",
		Scope:         "project",
		WorkspaceRoot: repo,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}}
	app.activeTabID = "tab-1"

	got := app.MetaForTab("tab-1")
	if got.Cwd != repo || got.WorkspaceRoot != repo || got.WorkspacePath != repo {
		t.Fatalf("workspace fields = cwd:%q root:%q path:%q, want %q", got.Cwd, got.WorkspaceRoot, got.WorkspacePath, repo)
	}
	if got.WorkspaceName != filepath.Base(repo) {
		t.Fatalf("workspaceName = %q, want %q", got.WorkspaceName, filepath.Base(repo))
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if strings.Contains(string(raw), "sandboxPath") || strings.Contains(string(raw), configuredSandboxRoot) {
		t.Fatalf("meta should not expose configured sandbox root as sandboxPath: %s", raw)
	}
	if got.GitBranch != "feature/meta" {
		t.Fatalf("gitBranch = %q, want feature/meta", got.GitBranch)
	}
}

func TestListTabsDoesNotExposeConfiguredSandboxPath(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := t.TempDir()
	configuredSandboxRoot := filepath.Join(t.TempDir(), "sandbox")
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Sandbox.WorkspaceRoot = configuredSandboxRoot
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {
		ID:            "tab-1",
		Scope:         "project",
		WorkspaceRoot: workspace,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}}
	app.activeTabID = "tab-1"
	app.tabOrder = []string{"tab-1"}

	raw, err := json.Marshal(app.ListTabs())
	if err != nil {
		t.Fatalf("marshal tabs: %v", err)
	}
	if strings.Contains(string(raw), "sandboxPath") || strings.Contains(string(raw), configuredSandboxRoot) {
		t.Fatalf("tab metadata should not expose configured sandbox root as sandboxPath: %s", raw)
	}
}

func TestListTabsExposesStructuredRuntimeStatus(t *testing.T) {
	asks := make(chan event.Ask, 1)
	done := make(chan event.Event, 1)
	runner := &desktopAskRuntimeRunner{}
	ctrl := control.New(control.Options{
		Runner: runner,
		Sink: event.FuncSink(func(e event.Event) {
			switch e.Kind {
			case event.AskRequest:
				asks <- e.Ask
			case event.TurnDone:
				done <- e
			}
		}),
	})
	runner.ask = func(ctx context.Context) error {
		_, err := ctrl.Ask(ctx, []event.AskQuestion{{
			ID:      "choice",
			Prompt:  "Pick one",
			Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
		}})
		return err
	}

	app := NewApp()
	app.setTestCtrl(ctrl, "prov/model")
	app.tabOrder = []string{"test"}
	ctrl.Send("ask user")
	select {
	case <-asks:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ask request")
	}

	tabs := app.ListTabs()
	if len(tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(tabs))
	}
	if !tabs[0].Running || !tabs[0].PendingPrompt || !tabs[0].Cancellable || tabs[0].CancelRequested {
		t.Fatalf("tab runtime = running:%v pending:%v cancellable:%v cancel:%v", tabs[0].Running, tabs[0].PendingPrompt, tabs[0].Cancellable, tabs[0].CancelRequested)
	}

	app.CancelTab("test")
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for turn_done")
	}
}

func TestMetaForTabLeavesGitBranchEmptyOutsideGit(t *testing.T) {
	isolateDesktopUserDirs(t)
	workspace := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"tab-1": {
		ID:            "tab-1",
		Scope:         "project",
		WorkspaceRoot: workspace,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}}
	app.activeTabID = "tab-1"

	if got := app.MetaForTab("tab-1"); got.GitBranch != "" {
		t.Fatalf("gitBranch = %q, want empty", got.GitBranch)
	}
}

func TestEffortDefaultsBeforeStartup(t *testing.T) {
	isolateDesktopUserDirs(t)

	got := NewApp().Effort()
	if !got.Supported || got.Current != "auto" || got.Default != "high" || !hasLevel(got.Levels, "auto") {
		t.Fatalf("pre-startup Effort() = %+v, want auto with DeepSeek default high", got)
	}
}

func TestMemoryViewReturnsNonNilArraysBeforeStartup(t *testing.T) {
	isolateDesktopUserDirs(t)

	view := NewApp().Memory()
	if view.Docs == nil || view.Facts == nil || view.Archives == nil || view.Scopes == nil {
		t.Fatalf("Memory() arrays must be non-nil before startup: %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal Memory(): %v", err)
	}
	for _, bad := range []string{`"docs":null`, `"facts":null`, `"archives":null`, `"scopes":null`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("Memory() JSON contains %s; frontend expects []: %s", bad, raw)
		}
	}
}

func TestMemoryViewIncludesActiveAndArchivedFacts(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	store := memory.Store{Dir: filepath.Join(userDir, "projects", "test", "memory")}
	if _, err := store.Save(memory.Memory{
		Name:        "active-fact",
		Title:       "Active fact",
		Description: "Still applies",
		Type:        memory.TypeProject,
		Body:        "Active body",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(memory.Memory{
		Name:        "archived-fact",
		Description: "No longer applies",
		Type:        memory.TypeFeedback,
		Body:        "Archived body",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Archive("archived-fact"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Memory: &memory.Set{
		Docs:    []memory.Source{{Path: filepath.Join(cwd, "AGENTS.md"), Scope: memory.ScopeProject, Body: "Project instructions"}},
		Store:   store,
		CWD:     cwd,
		UserDir: userDir,
	}}), "test-model")

	view := app.Memory()
	if !view.Available || view.StoreDir != store.Dir {
		t.Fatalf("Memory() availability/store = %v/%q, want true/%q", view.Available, view.StoreDir, store.Dir)
	}
	if len(view.Docs) != 1 || view.Docs[0].Scope != "project" || !strings.Contains(view.Docs[0].Body, "Project instructions") {
		t.Fatalf("Memory() docs = %+v", view.Docs)
	}
	if len(view.Facts) != 1 || view.Facts[0].Name != "active-fact" || view.Facts[0].Type != "project" {
		t.Fatalf("Memory() active facts = %+v", view.Facts)
	}
	if len(view.Archives) != 1 || view.Archives[0].Name != "archived-fact" || view.Archives[0].Type != "feedback" ||
		view.Archives[0].Path == "" || view.Archives[0].ArchivedAt == "" {
		t.Fatalf("Memory() archived facts = %+v", view.Archives)
	}
	if len(view.Scopes) != 3 {
		t.Fatalf("Memory() scopes = %+v, want user/project/local", view.Scopes)
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

func TestBackgroundCloseHideStrategyByPlatform(t *testing.T) {
	tests := []struct {
		goos string
		want bool
	}{
		{goos: "darwin", want: true},
		{goos: "windows", want: false},
		{goos: "linux", want: false},
		{goos: "freebsd", want: false},
	}
	for _, tt := range tests {
		if got := backgroundCloseUsesApplicationHide(tt.goos); got != tt.want {
			t.Fatalf("backgroundCloseUsesApplicationHide(%q) = %v, want %v", tt.goos, got, tt.want)
		}
	}
}

func TestBackgroundRestoreMaximiseStrategy(t *testing.T) {
	tests := []struct {
		goos      string
		maximised bool
		want      bool
	}{
		{goos: "windows", maximised: true, want: true},
		{goos: "linux", maximised: true, want: true},
		{goos: "darwin", maximised: true, want: false},
		{goos: "windows", maximised: false, want: false},
	}
	for _, tt := range tests {
		if got := backgroundRestoreShouldMaximise(tt.goos, tt.maximised); got != tt.want {
			t.Fatalf("backgroundRestoreShouldMaximise(%q, %v) = %v, want %v", tt.goos, tt.maximised, got, tt.want)
		}
	}
}

func TestBackgroundRestorePlanAvoidsNormalWindowFlash(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		maximised bool
		want      backgroundRestorePlan
	}{
		{
			name:      "maximised Windows window",
			goos:      "windows",
			maximised: true,
			want:      backgroundRestorePlan{maximiseBeforeShow: true},
		},
		{
			name:      "normal Windows window",
			goos:      "windows",
			maximised: false,
			want:      backgroundRestorePlan{unminimiseAfterShow: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := backgroundRestorePlanFor(tt.goos, tt.maximised)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("backgroundRestorePlanFor(%q, %v) = %v, want %v", tt.goos, tt.maximised, got, tt.want)
			}
		})
	}
}

func TestEmitReadyInvokesReadyHook(t *testing.T) {
	app := NewApp()
	var calls int32
	app.readyHook = func() {
		atomic.AddInt32(&calls, 1)
	}

	app.emitReady(context.TODO())

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

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
[desktop]
language = "zh"
layout_style = "workbench"
theme = "light"
theme_style = "glacier"
close_behavior = "quit"
status_bar_style = "icon"
status_bar_items = ["cost", "balance"]
`), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("set desktop language: %v", err)
	}
	if err := userCfg.SetDesktopLayoutStyle("classic"); err != nil {
		t.Fatalf("set desktop layout style: %v", err)
	}
	if err := userCfg.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("set desktop appearance: %v", err)
	}
	if err := userCfg.SetDesktopCloseBehavior("background"); err != nil {
		t.Fatalf("set desktop close behavior: %v", err)
	}
	if err := userCfg.SetDesktopStatusBarStyle("text"); err != nil {
		t.Fatalf("set desktop status bar style: %v", err)
	}
	if err := userCfg.SetDesktopStatusBarItems([]string{"model", "balance", "cache"}); err != nil {
		t.Fatalf("set desktop status bar items: %v", err)
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
	if got.DesktopLanguage != "en" || got.DesktopLayoutStyle != "classic" || got.DesktopTheme != "dark" || got.DesktopThemeStyle != "graphite" || got.CloseBehavior != "background" || got.StatusBarStyle != "text" {
		t.Fatalf("desktop settings = lang:%q layout:%q theme:%q style:%q close:%q status:%q, want user-level desktop prefs", got.DesktopLanguage, got.DesktopLayoutStyle, got.DesktopTheme, got.DesktopThemeStyle, got.CloseBehavior, got.StatusBarStyle)
	}
	if want := []string{"model", "balance", "cache"}; !reflect.DeepEqual(got.StatusBarItems, want) {
		t.Fatalf("desktop status bar items = %v, want user-level %v", got.StatusBarItems, want)
	}
}

func TestDesktopStartupSettingsUsesUserDesktopPreferencesWithoutFullSettingsPayload(t *testing.T) {
	isolateDesktopUserDirs(t)

	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("set desktop language: %v", err)
	}
	if err := userCfg.SetDesktopLayoutStyle("classic"); err != nil {
		t.Fatalf("set desktop layout style: %v", err)
	}
	if err := userCfg.SetDesktopAppearance("dark", "graphite"); err != nil {
		t.Fatalf("set desktop appearance: %v", err)
	}
	if err := userCfg.SetDesktopStatusBarStyle("icon"); err != nil {
		t.Fatalf("set desktop status bar style: %v", err)
	}
	if err := userCfg.SetDesktopStatusBarItems([]string{"workspace", "git_branch", "model"}); err != nil {
		t.Fatalf("set desktop status bar items: %v", err)
	}
	if err := userCfg.SetDesktopCheckUpdates(false); err != nil {
		t.Fatalf("set desktop check updates: %v", err)
	}
	userCfg.Bot.Enabled = true
	userCfg.Bot.Allowlist.Enabled = true
	userCfg.Bot.Allowlist.QQUsers = []string{"alice"}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	got := NewApp().DesktopStartupSettings()
	if got.DesktopLanguage != "en" || got.DesktopLayoutStyle != "classic" || got.DesktopTheme != "dark" || got.DesktopThemeStyle != "graphite" || got.DisplayMode != "standard" || got.StatusBarStyle != "icon" || got.CheckUpdates {
		t.Fatalf("DesktopStartupSettings desktop prefs = %+v, want user-level startup prefs", got)
	}
	if want := []string{"workspace", "git_branch", "model"}; !reflect.DeepEqual(got.StatusBarItems, want) {
		t.Fatalf("DesktopStartupSettings status bar items = %v, want %v", got.StatusBarItems, want)
	}
	if !got.Bot.Enabled || !got.Bot.Allowlist.Enabled || !reflect.DeepEqual(got.Bot.Allowlist.QQUsers, []string{"alice"}) {
		t.Fatalf("DesktopStartupSettings bot settings = %+v, want lightweight bot snapshot", got.Bot)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal DesktopStartupSettings: %v", err)
	}
	if strings.Contains(string(raw), "providers") || strings.Contains(string(raw), "officialProviders") || strings.Contains(string(raw), "providerKinds") {
		t.Fatalf("DesktopStartupSettings must not include full Settings provider payload: %s", raw)
	}
}

func BenchmarkDesktopSettingsPayloads(b *testing.B) {
	home := b.TempDir()
	xdg := filepath.Join(home, ".config")
	appData := filepath.Join(home, "AppData")
	for _, dir := range []string{xdg, appData} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
	}
	b.Setenv("HOME", home)
	b.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	b.Setenv("USERPROFILE", home)
	b.Setenv("XDG_CONFIG_HOME", xdg)
	b.Setenv("REASONIX_STATE_HOME", filepath.Join(home, "state"))
	b.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
	b.Setenv("AppData", appData)
	b.Setenv("SHARED_PROVIDER_KEY", "sk-test")

	cfg := config.LoadForEdit(config.UserConfigPath())
	for i := 0; i < 40; i++ {
		cfg.Providers = append(cfg.Providers, config.ProviderEntry{
			Name:      fmt.Sprintf("custom-%02d", i),
			Kind:      "openai",
			BaseURL:   "https://example.invalid/v1",
			APIKeyEnv: "SHARED_PROVIDER_KEY",
			Models:    []string{"model-a", "model-b"},
			Default:   "model-a",
		})
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		b.Fatalf("save config: %v", err)
	}
	app := NewApp()

	b.Run("Settings", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = app.Settings()
		}
	})
	b.Run("DesktopStartupSettings", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = app.DesktopStartupSettings()
		}
	})
}

func TestSettingsLoadsActiveWorkspaceCredentialsWithUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := robustTempDir(t)
	launch := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("WORKSPACE_ONLY_KEY=from-project\n"), 0o600); err != nil {
		t.Fatalf("write project env: %v", err)
	}
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.UpsertProvider(config.ProviderEntry{
		Name:      "workspace-provider",
		Kind:      "openai",
		BaseURL:   "https://workspace.example/v1",
		Model:     "workspace-model",
		APIKeyEnv: "WORKSPACE_ONLY_KEY",
	}); err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	userCfg.Desktop.ProviderAccess = []string{"workspace-provider"}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}
	t.Setenv("WORKSPACE_ONLY_KEY", "")
	os.Unsetenv("WORKSPACE_ONLY_KEY")
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(launch); err != nil {
		t.Fatalf("chdir launch: %v", err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"project": {ID: "project", WorkspaceRoot: project}}
	app.activeTabID = "project"
	got := app.Settings()
	for _, p := range got.Providers {
		if p.Name == "workspace-provider" {
			if !p.KeySet {
				t.Fatalf("workspace provider keySet = false, want true from active workspace .env: %+v", p)
			}
			if !p.Configured {
				t.Fatalf("workspace provider configured = false, want true from active workspace .env: %+v", p)
			}
			return
		}
	}
	t.Fatalf("workspace provider missing from settings: %+v", got.Providers)
}

func TestSettingsShowsGlobalCredentialWithoutMutatingWorkspaceEnv(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := robustTempDir(t)
	launch := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("SHARED_SETTINGS_KEY=from-project\n"), 0o600); err != nil {
		t.Fatalf("write project env: %v", err)
	}
	if _, err := config.SetCredential("SHARED_SETTINGS_KEY", "from-credentials"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	userCfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if err := userCfg.UpsertProvider(config.ProviderEntry{
		Name:      "settings-provider",
		Kind:      "openai",
		BaseURL:   "https://settings.example/v1",
		Model:     "settings-model",
		APIKeyEnv: "SHARED_SETTINGS_KEY",
	}); err != nil {
		t.Fatalf("upsert provider: %v", err)
	}
	userCfg.Desktop.ProviderAccess = []string{"settings-provider"}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}
	t.Setenv("SHARED_SETTINGS_KEY", "from-project")
	orig, _ := os.Getwd()
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(launch); err != nil {
		t.Fatalf("chdir launch: %v", err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{"project": {ID: "project", WorkspaceRoot: project}}
	app.activeTabID = "project"
	got := app.Settings()
	for _, p := range got.Providers {
		if p.Name != "settings-provider" {
			continue
		}
		if !p.KeySet || p.KeySource != "Reasonix credentials" {
			t.Fatalf("settings-provider key = set:%v source:%q, want Reasonix credentials: %+v", p.KeySet, p.KeySource, p)
		}
		if env := os.Getenv("SHARED_SETTINGS_KEY"); env != "from-project" {
			t.Fatalf("Settings mutated SHARED_SETTINGS_KEY = %q, want existing project env", env)
		}
		return
	}
	t.Fatalf("settings provider missing from settings: %+v", got.Providers)
}

func TestSettingsSeedsMissingUserConfigFromLegacyProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	project := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "legacy-provider/legacy-model"

[desktop]
language = "zh"
layout_style = "workbench"
theme = "light"
theme_style = "glacier"
close_behavior = "quit"
status_bar_style = "text"
status_bar_items = ["model", "cache", "balance"]
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
	if got.DefaultModel != "legacy-provider/legacy-model" || got.DesktopLanguage != "zh" || got.DesktopLayoutStyle != "workbench" || got.DesktopTheme != "light" || got.DesktopThemeStyle != "glacier" || got.CloseBehavior != "quit" || got.StatusBarStyle != "text" {
		t.Fatalf("Settings did not seed from legacy project config: %+v", got)
	}
	if want := []string{"model", "cache", "balance"}; !reflect.DeepEqual(got.StatusBarItems, want) {
		t.Fatalf("Settings did not seed status bar items from legacy project config: got %v want %v", got.StatusBarItems, want)
	}
	if _, err := os.Stat(config.UserConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("Settings() should not write user config before an edit, stat err = %v", err)
	}
	if err := app.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("SetDesktopLanguage: %v", err)
	}
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if userCfg.DesktopLanguage() != "en" || userCfg.DesktopLayoutStyle() != "workbench" || userCfg.DesktopTheme() != "light" || userCfg.DesktopThemeStyle() != "glacier" || userCfg.DesktopCloseBehavior() != "quit" || userCfg.DesktopStatusBarStyle() != "text" {
		t.Fatalf("saved user config did not preserve seeded desktop prefs: lang:%q layout:%q theme:%q style:%q close:%q status:%q", userCfg.DesktopLanguage(), userCfg.DesktopLayoutStyle(), userCfg.DesktopTheme(), userCfg.DesktopThemeStyle(), userCfg.DesktopCloseBehavior(), userCfg.DesktopStatusBarStyle())
	}
	if want := []string{"model", "cache", "balance"}; !reflect.DeepEqual(userCfg.DesktopStatusBarItems(), want) {
		t.Fatalf("saved user config did not preserve seeded status bar items: got %v want %v", userCfg.DesktopStatusBarItems(), want)
	}
}

func TestSettingsSubagentDefaultsRoundTrip(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	app := NewApp()
	if err := app.SetSubagentModel("deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetSubagentModel: %v", err)
	}
	if err := app.SetSubagentEffort("max"); err != nil {
		t.Fatalf("SetSubagentEffort: %v", err)
	}

	got := app.Settings()
	if got.SubagentModel != "deepseek/deepseek-v4-pro" || got.SubagentEffort != "max" {
		t.Fatalf("subagent settings = model:%q effort:%q", got.SubagentModel, got.SubagentEffort)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.SubagentModel != "deepseek/deepseek-v4-pro" || cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("saved config = model:%q effort:%q", cfg.Agent.SubagentModel, cfg.Agent.SubagentEffort)
	}
}

func TestSettingsSurfacesOfficialProviderTemplatesSeparately(t *testing.T) {
	isolateDesktopUserDirs(t)

	got := NewApp().Settings()
	providers := providerAccessSet(providerNamesFromView(got.Providers))
	official := providerAccessSet(providerNamesFromView(got.OfficialProviders))
	if providers["mimo-api"] {
		t.Fatalf("mimo-api should not be mixed into configured providers: %+v", got.Providers)
	}
	if !official["deepseek"] || official["mimo-api"] || official["mimo-token-plan"] {
		t.Fatalf("official providers = %+v, want only deepseek", got.OfficialProviders)
	}
}

func TestSettingsRepairsLegacyOfficialProviderWithoutModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek-flash"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := NewApp().Settings()
	for _, p := range got.Providers {
		if p.Name != "deepseek" {
			continue
		}
		if !p.BuiltIn {
			t.Fatalf("deepseek provider should be marked built-in for official endpoint: %+v", p)
		}
		if !p.Added || !p.KeySet || len(p.Models) != 2 || p.Models[0] != "deepseek-v4-flash" || p.Models[1] != "deepseek-v4-pro" || p.Default != "deepseek-v4-flash" {
			t.Fatalf("deepseek provider = %+v, want added repaired official model list", p)
		}
		if got.DefaultModel != "deepseek/deepseek-v4-flash" {
			t.Fatalf("default_model = %q, want deepseek/deepseek-v4-flash", got.DefaultModel)
		}
		return
	}
	t.Fatalf("settings providers missing deepseek: %+v", got.Providers)
}

func TestSettingsTreatsReservedProviderNameWithExternalEndpointAsCustom(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-Flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://opencode.ai/zen/go/v1"
models = ["deepseek-v4-Flash", "deepseek-v4-pro", "glm-5"]
default = "deepseek-v4-Flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := NewApp().Settings()
	var custom *ProviderView
	for i := range got.Providers {
		if got.Providers[i].Name == "deepseek" {
			custom = &got.Providers[i]
			break
		}
	}
	if custom == nil {
		t.Fatalf("settings providers missing deepseek: %+v", got.Providers)
	}
	if custom.BuiltIn {
		t.Fatalf("external deepseek endpoint should be custom, got built-in provider: %+v", *custom)
	}
	if !custom.Added || !custom.KeySet || custom.BaseURL != "https://opencode.ai/zen/go/v1" {
		t.Fatalf("external deepseek provider = %+v, want added key-set custom opencode endpoint", *custom)
	}
	for _, p := range got.OfficialProviders {
		if p.Name == "deepseek" && p.Added {
			t.Fatalf("official DeepSeek template should not be marked added by external endpoint: %+v", p)
		}
	}
}

func TestSettingsInfersLegacyProviderAccessWhenMissing(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek-flash/deepseek-v4-pro"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "mimo-pro"
kind = "openai"
base_url = "https://token-plan-cn.xiaomimimo.com/v1"
model = "mimo-v2.5-pro"
api_key_env = "MIMO_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := NewApp().Settings()
	providers := map[string]ProviderView{}
	for _, p := range got.Providers {
		providers[p.Name] = p
	}
	if !providers["deepseek"].Added || !providers["deepseek"].KeySet {
		t.Fatalf("deepseek provider = %+v, want inferred added key-set provider", providers["deepseek"])
	}
	if !providers["mimo-pro"].Added || !providers["mimo-pro"].KeySet || providers["mimo-pro"].BuiltIn {
		t.Fatalf("mimo-pro provider = %+v, want inferred custom key-set provider", providers["mimo-pro"])
	}
	if got.DefaultModel != "deepseek/deepseek-v4-pro" {
		t.Fatalf("default_model = %q, want deepseek/deepseek-v4-pro", got.DefaultModel)
	}
}

func TestSettingsDoesNotInferProviderAccessWhenExplicitlyEmpty(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek-flash/deepseek-v4-flash"

[desktop]
provider_access = []

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got := NewApp().Settings()
	for _, p := range got.Providers {
		if p.Added {
			t.Fatalf("provider %+v should not be inferred as added when provider_access is explicit empty", p)
		}
	}
}

func TestSettingsInfersConfiguredBuiltInsWithoutConfigFile(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "sk-test")

	got := NewApp().Settings()
	providers := map[string]ProviderView{}
	for _, p := range got.Providers {
		providers[p.Name] = p
	}
	if !providers["deepseek"].Added || !providers["deepseek"].KeySet {
		t.Fatalf("deepseek provider = %+v, want inferred added provider from configured key", providers["deepseek"])
	}
	if _, ok := providers["mimo-token-plan"]; ok {
		t.Fatalf("mimo-token-plan should not be inferred from MIMO_API_KEY alone: %+v", providers["mimo-token-plan"])
	}
}

func TestSettingsDoesNotInferBuiltInsWithoutKeys(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")

	got := NewApp().Settings()
	for _, p := range got.Providers {
		if p.Added {
			t.Fatalf("provider %+v should not be inferred as added without a configured key", p)
		}
	}
}

func TestAddOfficialProviderAccessReplacesLegacyProviderWithoutModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	os.Unsetenv("DEEPSEEK_API_KEY")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek-flash"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().AddOfficialProviderAccess("deepseek", "test-key"); err != nil {
		t.Fatalf("AddOfficialProviderAccess: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	p, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider not saved")
	}
	if len(p.Models) != 2 || p.Models[0] != "deepseek-v4-flash" || p.Models[1] != "deepseek-v4-pro" || p.Default != "deepseek-v4-flash" {
		t.Fatalf("deepseek provider after add = %+v, want official model list", p)
	}
	if !providerAccessSet(cfg.Desktop.ProviderAccess)["deepseek"] {
		t.Fatalf("provider_access missing deepseek: %+v", cfg.Desktop.ProviderAccess)
	}
	if cfg.DefaultModel != "deepseek/deepseek-v4-flash" {
		t.Fatalf("default_model = %q, want deepseek/deepseek-v4-flash", cfg.DefaultModel)
	}
}

func TestAddOfficialProviderAccessRejectsBackgroundJobsBeforeSavingKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	os.Unsetenv("DEEPSEEK_API_KEY")

	app := NewApp()
	app.ctx = context.Background()
	app.setTestCtrl(newBackgroundJobController(t, "provider-access-job"), "deepseek-flash/deepseek-v4-flash")

	_, err := app.AddOfficialProviderAccess("deepseek", "sk-test")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("AddOfficialProviderAccess with background job error = %v, want active-work guard", err)
	}
	if data, readErr := os.ReadFile(config.UserCredentialsPath()); readErr == nil && strings.Contains(string(data), "DEEPSEEK_API_KEY") {
		t.Fatalf("provider key should not be saved after rejected add access:\n%s", data)
	}
}

func TestSetProviderKeyRestoresOfficialProviderAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	os.Unsetenv("DEEPSEEK_API_KEY")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek/deepseek-v4-flash"

[desktop]
provider_access = []

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().SetProviderKey("DEEPSEEK_API_KEY", "sk-test"); err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if !providerAccessSet(cfg.Desktop.ProviderAccess)["deepseek"] {
		t.Fatalf("provider_access = %+v, want deepseek restored", cfg.Desktop.ProviderAccess)
	}
	got := NewApp().Settings()
	for _, p := range got.Providers {
		if p.Name == "deepseek" {
			if !p.Added || !p.KeySet {
				t.Fatalf("deepseek settings = %+v, want added and key-set", p)
			}
			return
		}
	}
	t.Fatalf("settings providers missing deepseek: %+v", got.Providers)
}

func TestSetProviderKeyKeepsCustomAliasProviderAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("PROXY_DEEPSEEK_KEY", "")
	os.Unsetenv("PROXY_DEEPSEEK_KEY")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[desktop]
provider_access = []

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://proxy.example/v1"
model = "deepseek-v4-flash"
api_key_env = "PROXY_DEEPSEEK_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().SetProviderKey("PROXY_DEEPSEEK_KEY", "sk-test"); err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	cfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	if !access["deepseek-flash"] {
		t.Fatalf("provider_access = %+v, want custom alias deepseek-flash", cfg.Desktop.ProviderAccess)
	}
	if access["deepseek"] {
		t.Fatalf("provider_access = %+v, should not canonicalize custom proxy to deepseek", cfg.Desktop.ProviderAccess)
	}
}

func TestAddOfficialProviderAccessUsesDesktopLanguagePricing(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[desktop]
language = "zh"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().AddOfficialProviderAccess("deepseek", ""); err != nil {
		t.Fatalf("AddOfficialProviderAccess: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	p, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider not saved")
	}
	flash := p.Prices["deepseek-v4-flash"]
	pro := p.Prices["deepseek-v4-pro"]
	if flash == nil || flash.Output != 2 || flash.Currency != "¥" {
		t.Fatalf("flash price = %+v, want CNY preset", flash)
	}
	if pro == nil || pro.Output != 6 || pro.Currency != "¥" {
		t.Fatalf("pro price = %+v, want CNY preset", pro)
	}
}

func TestRemoveBuiltInProviderAccessRetargetsDefaultToRemainingAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek-flash/deepseek-v4-pro"

[desktop]
provider_access = ["deepseek-flash", "mimo-pro"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "mimo-pro"
kind = "openai"
base_url = "https://token-plan-cn.xiaomimimo.com/v1"
model = "mimo-v2.5-pro"
api_key_env = "MIMO_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := NewApp().RemoveProviderAccess("deepseek"); err != nil {
		t.Fatalf("RemoveProviderAccess: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	if access["deepseek"] || !access["mimo-pro"] {
		t.Fatalf("provider_access = %+v, want only mimo-pro", cfg.Desktop.ProviderAccess)
	}
	if cfg.DefaultModel != "mimo-pro/mimo-v2.5-pro" {
		t.Fatalf("default_model = %q, want mimo-pro/mimo-v2.5-pro", cfg.DefaultModel)
	}
}

func TestModelsForTabOnlyListsProviderAccessWhenConfigured(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "deepseek-flash/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek-flash", "mimo-pro"}
	deepseek, _ := cfg.Provider("deepseek-flash")
	deepseek.Model = ""
	deepseek.Models = []string{"deepseek-v4-flash", "deepseek-v4-pro"}
	deepseek.Default = "deepseek-v4-flash"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	models := NewApp().Models()
	refs := modelRefsFromView(models)
	for _, want := range []string{
		"deepseek/deepseek-v4-flash",
		"deepseek/deepseek-v4-pro",
		"mimo-pro/mimo-v2.5-pro",
		"mimo-pro/mimo-v2.5",
	} {
		if !refs[want] {
			t.Fatalf("Models() refs = %+v, missing %s", models, want)
		}
	}
	for _, hidden := range []string{
		"deepseek-pro/deepseek-v4-pro",
		"mimo-flash/mimo-v2.5",
	} {
		if refs[hidden] {
			t.Fatalf("Models() refs = %+v, should not include hidden provider %s", models, hidden)
		}
	}
	if len(models) != 4 {
		t.Fatalf("Models() len = %d, want 4: %+v", len(models), models)
	}
}

func TestModelsForTabListsCustomMultiModelProviderWithoutMetadata(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("LOCAL_API_KEY", "sk-test")
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "local/model-a"

[desktop]
provider_access = ["local"]

[[providers]]
name = "local"
kind = "openai"
base_url = "http://127.0.0.1:23333/v1"
models = ["model-a", "model-b"]
default = "model-a"
api_key_env = "LOCAL_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	models := NewApp().Models()
	refs := modelRefsFromView(models)
	for _, want := range []string{"local/model-a", "local/model-b"} {
		if !refs[want] {
			t.Fatalf("Models() refs = %+v, missing %s", models, want)
		}
	}
	if len(models) != 2 {
		t.Fatalf("Models() len = %d, want 2: %+v", len(models), models)
	}
}

func TestModelsForTabListsKeylessCustomMultiModelProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "local/model-a"

[desktop]
provider_access = ["local"]

[[providers]]
name = "local"
kind = "openai"
base_url = "http://127.0.0.1:23333/v1"
models = ["model-a", "model-b"]
default = "model-a"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	models := NewApp().Models()
	refs := modelRefsFromView(models)
	for _, want := range []string{"local/model-a", "local/model-b"} {
		if !refs[want] {
			t.Fatalf("Models() refs = %+v, missing %s", models, want)
		}
	}
}

func TestModelsForTabListsLoopbackCustomProviderWithMissingKeyEnv(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "local/model-a"

[desktop]
provider_access = ["local"]

[[providers]]
name = "local"
kind = "openai"
base_url = "http://127.0.0.1:23333/v1"
models = ["model-a", "model-b"]
default = "model-a"
api_key_env = "LOCAL_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	models := NewApp().Models()
	refs := modelRefsFromView(models)
	for _, want := range []string{"local/model-a", "local/model-b"} {
		if !refs[want] {
			t.Fatalf("Models() refs = %+v, missing %s", models, want)
		}
	}
}

func TestModelsForTabListsMimoAPIPaidAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("MIMO_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "mimo-api/mimo-v2.5-pro"
	cfg.Desktop.ProviderAccess = []string{"mimo-api"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	models := NewApp().Models()
	refs := modelRefsFromView(models)
	for _, want := range []string{
		"mimo-api/mimo-v2.5-pro",
		"mimo-api/mimo-v2.5",
		"mimo-api/mimo-v2-omni",
	} {
		if !refs[want] {
			t.Fatalf("Models() refs = %+v, missing %s", models, want)
		}
	}
	if len(models) != 3 {
		t.Fatalf("Models() len = %d, want 3: %+v", len(models), models)
	}
}

func TestModelsForTabKeepsUserProvidersWithProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "sk-test")

	userCfg := config.Default()
	userCfg.DefaultModel = "mimo-pro/mimo-v2.5-pro"
	userCfg.Desktop.ProviderAccess = []string{"deepseek-flash", "mimo-pro"}
	if err := userCfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save user config: %v", err)
	}

	projectRoot := t.TempDir()
	projectConfig := `default_model = "deepseek-flash/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek-flash"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	app := NewApp()
	tab := &WorkspaceTab{ID: "project", WorkspaceRoot: projectRoot, Ready: true}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.activeTabID = tab.ID

	models := app.ModelsForTab(tab.ID)
	refs := modelRefsFromView(models)
	for _, want := range []string{
		"deepseek/deepseek-v4-flash",
		"mimo-pro/mimo-v2.5-pro",
	} {
		if !refs[want] {
			t.Fatalf("ModelsForTab refs = %+v, missing %s", models, want)
		}
	}
}

func TestSetModelForTabRejectsProviderOutsideAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "deepseek-flash/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek-flash"}
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "other", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "other-model"})
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{ID: "tab_a", Scope: "global", Ready: true, model: "deepseek-flash/deepseek-v4-flash"}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	err := app.SetModelForTab(tab.ID, "other/other-model")
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("SetModelForTab hidden provider error = %v, want not available", err)
	}
}

func TestSetDefaultModelRejectsProviderWithoutKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("MIMO_API_KEY", "")

	cfg := config.Default()
	cfg.Desktop.ProviderAccess = []string{"mimo-api"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	tab := &WorkspaceTab{ID: "tab_a", Scope: "global", Ready: true, model: "deepseek-flash/deepseek-v4-flash"}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	err := app.SetDefaultModel("mimo-api/mimo-v2.5-pro")
	if err == nil || !strings.Contains(err.Error(), "has no key") {
		t.Fatalf("SetDefaultModel no-key error = %v, want has no key", err)
	}
	if tab.model != "deepseek-flash/deepseek-v4-flash" {
		t.Fatalf("tab model after failed default change = %q, want previous", tab.model)
	}
}

func TestSaveProviderPersistsReasoningProtocol(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:              "deepseek-proxy",
		Kind:              "openai",
		BaseURL:           "https://proxy.example.com/v1",
		Models:            []string{"deepseek-v4-flash"},
		Default:           "deepseek-v4-flash",
		APIKeyEnv:         "DEEPSEEK_PROXY_KEY",
		ReasoningProtocol: "none",
		SupportedEfforts:  []string{"high", "max"},
		DefaultEffort:     "max",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("deepseek-proxy")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.ReasoningProtocol != "none" || got.DefaultEffort != "max" {
		t.Fatalf("saved provider = %+v, want reasoning_protocol none and default_effort max", got)
	}

	view := app.Settings()
	for _, p := range view.Providers {
		if p.Name == "deepseek-proxy" {
			if p.ReasoningProtocol != "none" {
				t.Fatalf("settings reasoningProtocol = %q, want none", p.ReasoningProtocol)
			}
			return
		}
	}
	t.Fatalf("Settings() missing saved provider: %+v", view.Providers)
}

func TestDeleteProviderMigratesConfigAndOpenTabs(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "prov-a/model-a2"
	cfg.Providers = []config.ProviderEntry{
		{Name: "prov-a", Kind: "openai", BaseURL: "https://a.example.com", Model: "model-a1", Models: []string{"model-a1", "model-a2"}, APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	cfg.Agent.PlannerModel = "prov-a"
	cfg.Desktop.ProviderAccess = []string{"prov-a", "prov-b"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	ctrl := control.New(control.Options{Label: "old"})
	defer ctrl.Close()
	app := NewApp()
	tab := &WorkspaceTab{ID: "tab_a", Scope: "global", Ctrl: ctrl, Label: "prov-a/model-a1", Ready: true, model: "prov-a/model-a1"}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.DeleteProvider("prov-a"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	if _, ok := got.Provider("prov-a"); ok {
		t.Fatal("prov-a should be removed")
	}
	if got.DefaultModel != "prov-b" || got.Agent.PlannerModel != "prov-b" {
		t.Fatalf("model refs after delete = default:%q planner:%q, want prov-b", got.DefaultModel, got.Agent.PlannerModel)
	}
	if providerAccessSet(got.Desktop.ProviderAccess)["prov-a"] {
		t.Fatalf("provider access still contains prov-a: %+v", got.Desktop.ProviderAccess)
	}
	if tab.model != "prov-b/model-b1" || tab.Label != "prov-b/model-b1" {
		t.Fatalf("tab model after delete = model:%q label:%q, want prov-b/model-b1", tab.model, tab.Label)
	}
	if tab.Ctrl != nil {
		t.Fatal("tab controller should be closed and cleared when retargeted without a running app context")
	}
}

func TestDeleteProviderRejectsRunningAffectedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "prov-a/model-a1"
	cfg.Providers = []config.ProviderEntry{
		{Name: "prov-a", Kind: "openai", BaseURL: "https://a.example.com", Model: "model-a1", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Runner: runner}), "prov-a/model-a1")
	ctrl := app.activeCtrl()
	ctrl.Submit("work")
	<-runner.started

	err := app.DeleteProvider("prov-a")
	if err == nil || !strings.Contains(err.Error(), "finish or cancel") {
		t.Fatalf("DeleteProvider while running error = %v, want finish/cancel guard", err)
	}
	if _, ok := config.LoadForEdit(config.UserConfigPath()).Provider("prov-a"); !ok {
		t.Fatal("provider should remain after rejected deletion")
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
	ctrl.Close()
}

func TestDeleteProviderRejectsAffectedBackgroundJobs(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "prov-a/model-a1"
	cfg.Providers = []config.ProviderEntry{
		{Name: "prov-a", Kind: "openai", BaseURL: "https://a.example.com", Model: "model-a1", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "provider-job.jsonl")
	jm := jobs.NewManager(event.Discard)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test", Jobs: jm})
	defer ctrl.Close()
	app := NewApp()
	app.setTestCtrl(ctrl, "prov-a/model-a1")
	jm.StartForSession(agent.BranchID(path), "bash", "provider job", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	err := app.DeleteProvider("prov-a")
	if err == nil || !strings.Contains(err.Error(), "active work") {
		t.Fatalf("DeleteProvider with background job error = %v, want active-work guard", err)
	}
	if _, ok := config.LoadForEdit(config.UserConfigPath()).Provider("prov-a"); !ok {
		t.Fatal("provider should remain after rejected deletion")
	}
}

func TestDeleteProviderRejectsUnaffectedBackgroundJobsBeforeSavingConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "prov-b/model-b1"
	cfg.Providers = []config.ProviderEntry{
		{Name: "prov-a", Kind: "openai", BaseURL: "https://a.example.com", Model: "model-a1", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.setTestCtrl(newBackgroundJobController(t, "provider-unaffected-job"), "prov-b/model-b1")

	err := app.DeleteProvider("prov-a")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("DeleteProvider with unaffected background job error = %v, want active-work guard", err)
	}
	if _, ok := config.LoadForEdit(config.UserConfigPath()).Provider("prov-a"); !ok {
		t.Fatal("unaffected provider should remain after rejected deletion")
	}
}

func TestRemoveBuiltInProviderAccessRejectsBackgroundJobsBeforeSavingConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "mimo-pro/mimo-v2.5-pro"

[desktop]
provider_access = ["deepseek-flash", "mimo-pro"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"

[[providers]]
name = "mimo-pro"
kind = "openai"
base_url = "https://token-plan-cn.xiaomimimo.com/v1"
model = "mimo-v2.5-pro"
api_key_env = "MIMO_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.setTestCtrl(newBackgroundJobController(t, "provider-access-unaffected-job"), "mimo-token-plan/mimo-v2.5-pro")

	err := app.RemoveProviderAccess("deepseek")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("RemoveProviderAccess with background job error = %v, want active-work guard", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	if !access["deepseek"] && !access["deepseek-flash"] {
		t.Fatalf("provider_access should still contain deepseek after rejected removal: %+v", cfg.Desktop.ProviderAccess)
	}
}

func TestConnectKeyRejectsBackgroundJobsBeforeSavingKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	os.Unsetenv("DEEPSEEK_API_KEY")

	app := NewApp()
	app.ctx = context.Background()
	app.setTestCtrl(newBackgroundJobController(t, "connect-key-job"), "deepseek-flash/deepseek-v4-flash")

	_, err := app.ConnectKey("sk-test")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("ConnectKey with background job error = %v, want active-work guard", err)
	}
	if data, readErr := os.ReadFile(config.UserCredentialsPath()); readErr == nil && strings.Contains(string(data), "DEEPSEEK_API_KEY") {
		t.Fatalf("onboarding key should not be saved after rejected connect:\n%s", data)
	}
}

func TestMigrateDesktopPreferencesDoesNotOverwriteExistingConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	userCfg := config.LoadForEdit(config.UserConfigPath())
	if err := userCfg.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("set desktop language: %v", err)
	}
	if err := userCfg.SetDesktopLayoutStyle("workbench"); err != nil {
		t.Fatalf("set desktop layout style: %v", err)
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
	if got.DesktopLanguage() != "en" || got.DesktopLayoutStyle() != "workbench" || got.DesktopTheme() != "dark" || got.DesktopThemeStyle() != "graphite" {
		t.Fatalf("desktop prefs after migration = lang:%q layout:%q theme:%q style:%q, want existing config preserved", got.DesktopLanguage(), got.DesktopLayoutStyle(), got.DesktopTheme(), got.DesktopThemeStyle())
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

func TestSetEffortMigratesStaleOfficialDeepSeekTabModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "deepseek",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Model:     "glm-5",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

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
	tab := app.activeTab()
	if tab == nil {
		t.Fatal("active tab missing")
	}
	if tab.model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("tab model = %q, want migrated official ref", tab.model)
	}
}

func TestSetTokenModeRebuildsController(t *testing.T) {
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

	if err := app.SetTokenMode("economy"); err != nil {
		t.Fatalf("SetTokenMode(economy): %v", err)
	}
	if c := app.activeCtrl(); c == nil {
		t.Fatal("SetTokenMode should leave a rebuilt controller")
	}
	if c := app.activeCtrl(); c == old {
		t.Fatal("SetTokenMode should rebuild the active controller so the provider sees the new tool profile")
	}
	tab := app.activeTab()
	if tab == nil {
		t.Fatal("active tab missing")
	}
	if got := currentTabTokenMode(tab); got != "economy" {
		t.Fatalf("token mode = %q, want economy", got)
	}
	if got := app.Meta().TokenMode; got != "economy" {
		t.Fatalf("Meta token mode = %q, want economy", got)
	}
	saved := loadTabsFile()
	if len(saved.Tabs) != 1 || saved.Tabs[0].TokenMode != "economy" {
		t.Fatalf("saved tabs = %+v, want economy token mode", saved.Tabs)
	}
}

func TestSetTokenModeMigratesStaleOfficialDeepSeekTabModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "deepseek",
		Kind:      "openai",
		BaseURL:   "https://api.deepseek.com",
		Model:     "glm-5",
		APIKeyEnv: "DEEPSEEK_API_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

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

	if err := app.SetTokenMode("economy"); err != nil {
		t.Fatalf("SetTokenMode(economy): %v", err)
	}
	tab := app.activeTab()
	if tab == nil {
		t.Fatal("active tab missing")
	}
	if tab.model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("tab model = %q, want migrated official ref", tab.model)
	}
	if got := currentTabTokenMode(tab); got != "economy" {
		t.Fatalf("token mode = %q, want economy", got)
	}
}

func TestSetTokenModeKeepsControllerWhenRebuildFails(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("MIMO_API_KEY", "")

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	old := control.New(control.Options{Label: "old-controller"})
	app.setTestCtrl(old, "missing-token-mode-model")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	err := app.SetTokenMode("economy")
	if err == nil {
		t.Fatal("SetTokenMode(economy) with an unknown model should fail")
	}
	if c := app.activeCtrl(); c != old {
		t.Fatalf("SetTokenMode failure replaced controller: got %p want %p", c, old)
	}
	tab := app.activeTab()
	if tab == nil {
		t.Fatal("active tab missing")
	}
	if got := currentTabTokenMode(tab); got != "full" {
		t.Fatalf("token mode after failed rebuild = %q, want full", got)
	}
	if got := app.Meta().TokenMode; got != "full" {
		t.Fatalf("Meta token mode after failed rebuild = %q, want full", got)
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

func TestSetTokenModeRejectsRunningTurn(t *testing.T) {
	isolateDesktopUserDirs(t)

	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Runner: runner}), "")
	app.activeCtrl().Submit("work")
	<-runner.started

	err := app.SetTokenMode("economy")
	if err == nil || !strings.Contains(err.Error(), "finish or cancel") {
		t.Fatalf("SetTokenMode while running error = %v, want finish/cancel guard", err)
	}

	close(runner.release)
	waitNotRunning(t, app.activeCtrl())
}

func TestSetTokenModeRejectsBackgroundJobs(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "jobs.jsonl")
	jm := jobs.NewManager(event.Discard)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test", Jobs: jm})
	defer ctrl.Close()
	app := NewApp()
	app.setTestCtrl(ctrl, "")

	release := make(chan struct{})
	jm.StartForSession(agent.BranchID(path), "bash", "long job", func(ctx context.Context, _ io.Writer) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "", nil
		}
	})
	defer close(release)

	err := app.SetTokenMode("economy")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("SetTokenMode with background job error = %v, want background-job guard", err)
	}
}

func TestSettingsRebuildRejectsBackgroundJobs(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "settings-job.jsonl")
	jm := jobs.NewManager(event.Discard)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test", Jobs: jm})
	defer ctrl.Close()
	app := NewApp()
	app.ctx = context.Background()
	app.setTestCtrl(ctrl, "deepseek-flash/deepseek-v4-flash")

	jm.StartForSession(agent.BranchID(path), "bash", "settings job", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	err := app.SetSandbox("enforce", true, "", nil, "")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("SetSandbox with background job error = %v, want background-job guard", err)
	}
}

func TestClearSessionCancelsRunningRuntimeAndKeepsTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "clear-running.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"old"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	oldCtrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: path, Label: "test"})
	app := NewApp()
	app.projectTreeChangedHook = func() {}
	app.setTestCtrl(oldCtrl, "deepseek-flash/deepseek-v4-flash")
	app.tabs["test"].TopicID = "topic_clear"
	app.tabs["test"].TopicTitle = "Clear topic"
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	oldCtrl.Submit("work")
	<-runner.started
	if err := app.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	waitNotRunning(t, oldCtrl)
	tab := app.activeTab()
	if tab == nil || tab.Ctrl == nil {
		t.Fatalf("active tab/controller missing after clear")
	}
	if tab.Ctrl == oldCtrl {
		t.Fatalf("clear should replace the active controller after cancelling old work")
	}
	if tab.TopicID != "topic_clear" || tab.TopicTitle != "Clear topic" {
		t.Fatalf("clear changed topic identity: %+v", tab)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("old cleared session artifacts should be removed, stat err = %v", err)
	}
	if got := tab.currentSessionPath(); got == "" || got == path {
		t.Fatalf("new session path = %q, want fresh path", got)
	}
}

func TestClearSessionRemovesRunningJobArtifacts(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "clear-running-job.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"old"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	jm := jobs.NewManager(event.Discard)
	oldCtrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test", Jobs: jm})
	app := NewApp()
	app.projectTreeChangedHook = func() {}
	app.setTestCtrl(oldCtrl, "deepseek-flash/deepseek-v4-flash")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	started := make(chan struct{})
	jm.StartForSession(agent.BranchID(path), "bash", "clear artifact", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	})
	<-started
	jobsDir := jobs.ArtifactDir(path)
	if _, err := os.Stat(jobsDir); err != nil {
		t.Fatalf("job sidecar should exist before clear: %v", err)
	}

	if err := app.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if _, err := os.Stat(jobsDir); !os.IsNotExist(err) {
		t.Fatalf("old job sidecar should be removed after clear, stat err = %v", err)
	}
}

func TestSearchFileRefsFindsNestedBasename(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	dir := robustTempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "frontend", "wailsjs", "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "wailsjs", "runtime", "runtime.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", "Thumbs.db"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend", ".DS_Store"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "runtime.js"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, noise := range []string{".codex", ".npm", ".pnpm-store", "bin", "dist", "stage", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, noise), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, noise, "runtime.js"), []byte("noise"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "desktop", "frontend", "wailsjs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "desktop", "frontend", "wailsjs", "runtime.js"), []byte("generated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "product", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "product", "bin", "runtime.js"), []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	app := &App{}
	listed := app.ListDir("")
	for _, hidden := range []string{".codex", ".npm", ".pnpm-store", "bin", "dist", "stage", "tmp"} {
		if hasDirEntry(listed, hidden) {
			t.Fatalf("ListDir should hide local noise %q, got %+v", hidden, listed)
		}
	}
	desktopFrontend := app.ListDir("desktop/frontend")
	if hasDirEntry(desktopFrontend, "wailsjs") {
		t.Fatalf("ListDir should hide generated Wails bindings, got %+v", desktopFrontend)
	}
	frontendEntries := app.ListDir("frontend")
	for _, hidden := range []string{".DS_Store", "Thumbs.db"} {
		if hasDirEntry(frontendEntries, hidden) {
			t.Fatalf("ListDir should hide local noise file %q, got %+v", hidden, frontendEntries)
		}
	}

	got := app.SearchFileRefs("runtime.js")
	if !hasDirEntry(got, "frontend/wailsjs/runtime/runtime.js") {
		t.Fatalf("SearchFileRefs(runtime.js) should find nested workspace file, got %+v", got)
	}
	if !hasDirEntry(got, "product/bin/runtime.js") {
		t.Fatalf("SearchFileRefs should keep non-root bin directories searchable, got %+v", got)
	}
	if hasDirEntry(got, "node_modules/pkg/runtime.js") {
		t.Fatalf("SearchFileRefs should skip node_modules noise, got %+v", got)
	}
	for _, hidden := range []string{
		".codex/runtime.js",
		".npm/runtime.js",
		".pnpm-store/runtime.js",
		"bin/runtime.js",
		"desktop/frontend/wailsjs/runtime.js",
		"dist/runtime.js",
		"stage/runtime.js",
		"tmp/runtime.js",
	} {
		if hasDirEntry(got, hidden) {
			t.Fatalf("SearchFileRefs should skip local noise %q, got %+v", hidden, got)
		}
	}
	if noise := app.SearchFileRefs("Thumbs"); hasDirEntry(noise, "frontend/Thumbs.db") {
		t.Fatalf("SearchFileRefs should skip Thumbs.db noise, got %+v", noise)
	}
	if noise := app.SearchFileRefs(".DS"); hasDirEntry(noise, "frontend/.DS_Store") {
		t.Fatalf("SearchFileRefs should skip .DS_Store noise even for dot-prefixed search, got %+v", noise)
	}
}

func TestFileRefsUseActiveTabWorkspaceRoot(t *testing.T) {
	orig, _ := os.Getwd()
	defer os.Chdir(orig)

	launchRoot := robustTempDir(t)
	projectRoot := robustTempDir(t)
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

func TestDeleteSessionCancelsActiveRuntime(t *testing.T) {
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
	activeCtrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test"})
	keepPath := filepath.Join(dir, "keep.jsonl")
	if err := os.WriteFile(keepPath, []byte(`{"role":"user","content":"keep"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write keep session: %v", err)
	}
	keepCtrl := control.New(control.Options{SessionDir: dir, SessionPath: keepPath, Label: "keep"})
	defer keepCtrl.Close()
	app.setTestCtrl(activeCtrl, "")
	app.tabs["keep"] = &WorkspaceTab{ID: "keep", Scope: "global", Ctrl: keepCtrl, Ready: true}
	app.tabOrder = []string{"test", "keep"}

	if err := app.DeleteSession(filepath.Base(path)); err != nil {
		t.Fatalf("DeleteSession(active basename): %v", err)
	}
	if _, ok := app.tabs["test"]; ok {
		t.Fatalf("deleted active session runtime should be removed")
	}
	if got := app.activeTabID; got != "keep" {
		t.Fatalf("active tab after delete = %q, want keep", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("active session should be moved out of active history, stat err = %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "active.jsonl", "active.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("active session should be moved to trash: %v", err)
	}
}

func TestDeleteSessionWithStuckJobReturnsAfterSingleGrace(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "stuck-delete.jsonl")
	keepPath := filepath.Join(dir, "keep.jsonl")
	for _, p := range []string{path, keepPath} {
		if err := os.WriteFile(p, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write session %s: %v", p, err)
		}
	}

	grace := 500 * time.Millisecond
	slack := 300 * time.Millisecond
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(grace))
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test", Jobs: jm})
	keepCtrl := control.New(control.Options{SessionDir: dir, SessionPath: keepPath, Label: "keep"})
	releaseJob := startNonCooperativeSessionJob(t, jm, path)
	defer func() {
		releaseJob()
		ctrl.Close()
		keepCtrl.Close()
	}()

	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.tabs["keep"] = &WorkspaceTab{ID: "keep", Scope: "global", Ctrl: keepCtrl, Ready: true}
	app.tabOrder = []string{"test", "keep"}

	start := time.Now()
	if err := app.DeleteSession(filepath.Base(path)); err != nil {
		t.Fatalf("DeleteSession(stuck job): %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > grace+slack {
		t.Fatalf("DeleteSession took %s, want one teardown grace plus scheduling slack", elapsed)
	}
	if !agent.IsCleanupPending(path) {
		t.Fatalf("stuck delete should mark cleanup pending")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stuck session file should remain until delayed cleanup: %v", err)
	}
}

func TestDeleteSessionTrashConflictKeepsRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "active-conflict.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, sessionTrashDir, filepath.Base(path)), 0o755); err != nil {
		t.Fatalf("create trash conflict: %v", err)
	}

	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, SessionDir: dir, SessionPath: path, Label: "test"})
	app := NewApp()
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()
	ctrl.Submit("work")
	<-runner.started

	err := app.DeleteSession(filepath.Base(path))
	if err == nil || !strings.Contains(err.Error(), "already exists in trash") {
		t.Fatalf("DeleteSession conflict error = %v, want trash conflict", err)
	}
	if app.activeCtrl() != ctrl {
		t.Fatalf("active runtime should remain bound after preflight failure")
	}
	if !ctrl.Running() {
		t.Fatalf("running turn should not be cancelled on preflight failure")
	}
	if _, ok := app.tabs["test"]; !ok {
		t.Fatalf("tab should remain after preflight failure")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active session file should remain: %v", err)
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
}

func TestDeleteSessionCancelsInactiveOpenRuntime(t *testing.T) {
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

	if err := app.DeleteSession(filepath.Base(inactivePath)); err != nil {
		t.Fatalf("DeleteSession(inactive open basename): %v", err)
	}
	if _, ok := app.tabs["inactive"]; ok {
		t.Fatalf("deleted inactive session runtime should be removed")
	}
	if _, err := os.Stat(inactivePath); !os.IsNotExist(err) {
		t.Fatalf("inactive open session should be moved out of active history, stat err = %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "inactive.jsonl", "inactive.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("inactive open session should be moved to trash: %v", err)
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
	if current[filepath.Base(otherPath)] {
		t.Fatalf("ListSessions marked unopened session current, got %#v", current)
	}
	if !open[filepath.Base(activePath)] {
		t.Fatalf("ListSessions should mark active and inactive open sessions open, got %#v", open)
	}
	if open[filepath.Base(inactivePath)] || open[filepath.Base(otherPath)] {
		t.Fatalf("ListSessions marked unopened session open, got %#v", open)
	}
}

func TestTrashTopicWithStuckJobReturnsAfterSingleGrace(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_stuck_trash"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Stuck trash"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessionPath := writeTopicSession(t, dir, "stuck-topic.jsonl", topicID, "Stuck trash", projectRoot)

	grace := 500 * time.Millisecond
	slack := 300 * time.Millisecond
	jm := jobs.NewManager(event.Discard, jobs.WithTeardownGrace(grace))
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: sessionPath, Label: "test", Jobs: jm, WorkspaceRoot: projectRoot})
	releaseJob := startNonCooperativeSessionJob(t, jm, sessionPath)
	defer func() {
		releaseJob()
		ctrl.Close()
	}()

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"stuck": {
				ID:            "stuck",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Stuck trash",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"keep": {
				ID:            "keep",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       "topic_keep",
				TopicTitle:    "Keep",
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"stuck", "keep"},
		activeTabID: "stuck",
	}

	start := time.Now()
	if err := app.TrashTopic(topicID); err != nil {
		t.Fatalf("TrashTopic(stuck job): %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > grace+slack {
		t.Fatalf("TrashTopic took %s, want one teardown grace plus scheduling slack", elapsed)
	}
	if !agent.IsCleanupPending(sessionPath) {
		t.Fatalf("stuck topic trash should mark cleanup pending")
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("stuck topic session should remain until delayed trash: %v", err)
	}
}

func TestRestoreSessionRejectsDestroyingSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "trash-me.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("deleteSessionFile: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(sessionPath), filepath.Base(sessionPath))

	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl"), Label: "active", Jobs: jm})
	defer ctrl.Close()
	destroy := ctrl.BeginDestroySession(sessionPath)
	defer destroy.Finish()

	app := NewApp()
	app.setTestCtrl(ctrl, "")
	if err := app.RestoreSession(trashPath); err == nil || !strings.Contains(err.Error(), "cleanup is still in progress") {
		t.Fatalf("RestoreSession while destroying error = %v, want cleanup-in-progress", err)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("trashed session should remain after rejected restore: %v", err)
	}

	destroy.Finish()
	if err := app.RestoreSession(trashPath); err != nil {
		t.Fatalf("RestoreSession after finish: %v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("session should be restored: %v", err)
	}
}

func TestDesktopSessionAPIsUseControllerSessionDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	dirA := filepath.Join(t.TempDir(), "workspace-a-sessions")
	dirB := filepath.Join(t.TempDir(), "workspace-b-sessions")
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatalf("mkdir dirA: %v", err)
	}
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatalf("mkdir dirB: %v", err)
	}
	pathA := filepath.Join(dirA, "a.jsonl")
	pathB := filepath.Join(dirB, "b.jsonl")
	if err := os.WriteFile(pathA, []byte(`{"role":"user","content":"workspace A"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write pathA: %v", err)
	}
	if err := os.WriteFile(pathB, []byte(`{"role":"user","content":"workspace B"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write pathB: %v", err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{SessionDir: dirA, SessionPath: pathA, Label: "test"}), "")
	defer app.activeCtrl().Close()

	sessions := app.ListSessions()
	if len(sessions) != 1 || sessions[0].Path != pathA || sessions[0].Preview != "workspace A" {
		t.Fatalf("ListSessions should read the active controller session dir only, got %+v", sessions)
	}
	if err := app.RenameSession(pathA, "A title"); err != nil {
		t.Fatalf("RenameSession in active session dir: %v", err)
	}
	if titles := loadSessionTitles(dirA); titles["a.jsonl"] != "A title" {
		t.Fatalf("title should be written beside the active session, got %+v", titles)
	}
	if titles := loadSessionTitles(dirB); len(titles) != 0 {
		t.Fatalf("inactive workspace title sidecar should remain untouched, got %+v", titles)
	}
}

func TestListSessionsMarksAutoBotSessionAsChannel(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "bot-channel.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"from channel"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected",
		SessionMappings: []config.BotConnectionSessionMapping{{
			RemoteID: "wx-chat-1", SessionID: "path:" + path, SessionSource: "auto",
		}},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl"), Label: "test"}), "")
	defer app.activeCtrl().Close()

	sessions := app.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("ListSessions len = %d, want 1: %+v", len(sessions), sessions)
	}
	got := sessions[0]
	if got.Kind != "channel" || got.Channel != "weixin" || got.ChannelLabel != "微信" || got.RemoteID != "wx-chat-1" || got.SessionSource != "auto" {
		t.Fatalf("channel session meta = %+v", got)
	}
}

func TestOpenChannelSessionForTabIsReadOnly(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "bot-channel.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"from channel"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl"), Label: "test"})
	app.setTestCtrl(ctrl, "")
	defer app.activeCtrl().Close()

	if _, err := app.OpenChannelSessionForTab("test", path); err != nil {
		t.Fatalf("OpenChannelSessionForTab: %v", err)
	}
	if meta := app.tabMeta(app.activeTab(), true); !meta.ReadOnly {
		t.Fatalf("channel tab should be read-only: %+v", meta)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	app.SubmitToTab("test", "must not append")
	app.RunShellForTab("test", "echo must-not-run")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("read-only channel transcript changed:\nbefore=%s\nafter=%s", before, after)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString(`{"role":"user","content":"external follow-up"}` + "\n"); err != nil {
		f.Close()
		t.Fatalf("append external message: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close append: %v", err)
	}
	app.snapshotAllTabs()
	afterSnapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after snapshot: %v", err)
	}
	if !strings.Contains(string(afterSnapshot), "external follow-up") {
		t.Fatalf("read-only channel snapshot overwrote external append:\n%s", afterSnapshot)
	}
}

func TestCloseReadOnlyChannelTabDoesNotSnapshotTranscript(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "bot-channel.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"from channel"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl"), Label: "test"})
	app.setTestCtrl(ctrl, "")
	defer ctrl.Close()

	if _, err := app.OpenChannelSessionForTab("test", path); err != nil {
		t.Fatalf("OpenChannelSessionForTab: %v", err)
	}
	app.mu.Lock()
	app.tabs["survivor"] = &WorkspaceTab{ID: "survivor", Scope: "global", Ready: true, disabledMCP: map[string]ServerView{}}
	app.tabOrder = []string{"test", "survivor"}
	app.activeTabID = "test"
	app.mu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open append: %v", err)
	}
	if _, err := f.WriteString(`{"role":"user","content":"external close follow-up"}` + "\n"); err != nil {
		f.Close()
		t.Fatalf("append external message: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close append: %v", err)
	}

	if err := app.CloseTab("test"); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	afterClose, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after close: %v", err)
	}
	if !strings.Contains(string(afterClose), "external close follow-up") {
		t.Fatalf("closing read-only channel tab overwrote external append:\n%s", afterClose)
	}
}

func TestResumeSessionRejectsCleanupPending(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	activePath := filepath.Join(dir, "active.jsonl")
	pendingPath := filepath.Join(dir, "pending.jsonl")
	for _, path := range []string{activePath, pendingPath} {
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := agent.MarkCleanupPending(pendingPath, "delete"); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: activePath, Label: "test"})
	app.setTestCtrl(ctrl, "")
	defer app.activeCtrl().Close()

	if _, err := app.ResumeSession(pendingPath); err == nil || !strings.Contains(err.Error(), "pending cleanup") {
		t.Fatalf("ResumeSession cleanup-pending error = %v, want pending cleanup", err)
	}
	if got := app.activeCtrl().SessionPath(); filepath.Clean(got) != filepath.Clean(activePath) {
		t.Fatalf("active session path after rejected resume = %q, want %q", got, activePath)
	}
	if _, err := app.OpenChannelSessionForTab("test", pendingPath); err == nil || !strings.Contains(err.Error(), "pending cleanup") {
		t.Fatalf("OpenChannelSessionForTab cleanup-pending error = %v, want pending cleanup", err)
	}
	if meta := app.tabMeta(app.activeTab(), true); meta.ReadOnly {
		t.Fatalf("rejected channel open should not make tab read-only: %+v", meta)
	}
}

func TestResumeSessionRejectsPathOutsideControllerSessionDir(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	activePath := filepath.Join(dirA, "active.jsonl")
	outsidePath := filepath.Join(dirB, "outside.jsonl")
	for _, path := range []string{activePath, outsidePath} {
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{SessionDir: dirA, SessionPath: activePath, Label: "test"}), "")
	defer app.activeCtrl().Close()

	if _, err := app.ResumeSession(outsidePath); err == nil {
		t.Fatal("ResumeSession should reject a transcript outside the active session dir")
	}
	if _, err := app.PreviewSession(outsidePath); err == nil {
		t.Fatal("PreviewSession should reject a transcript outside the active session dir")
	}
}

func BenchmarkDesktopListSessionsScoped(b *testing.B) {
	dirA := filepath.Join(b.TempDir(), "workspace-a-sessions")
	dirB := filepath.Join(b.TempDir(), "workspace-b-sessions")
	for _, dir := range []string{dirA, dirB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatalf("mkdir %s: %v", dir, err)
		}
		for i := 0; i < 120; i++ {
			path := filepath.Join(dir, fmt.Sprintf("session-%03d.jsonl", i))
			body := fmt.Sprintf(`{"role":"user","content":"session %03d"}`+"\n", i)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				b.Fatalf("write session: %v", err)
			}
		}
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{SessionDir: dirA, SessionPath: filepath.Join(dirA, "session-000.jsonl"), Label: "test"}), "")
	defer app.activeCtrl().Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessions := app.ListSessions()
		if len(sessions) != 120 {
			b.Fatalf("ListSessions len = %d, want 120", len(sessions))
		}
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

func TestSubmitToTabHistoryDisplaysRawInputAfterMemoryCompose(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "memory-display.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	runner := &appendingDesktopRunner{session: sess, started: make(chan string, 1)}
	ctrl := control.New(control.Options{
		Runner:      runner,
		Executor:    exec,
		Sink:        event.Discard,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
	})
	defer ctrl.Close()

	app := NewApp()
	app.setTestCtrl(ctrl, "deepseek/test")
	ctrl.QueueMemory(`Saved memory "reasonix-contributions": contribution count updated`)

	const prompt = "不要，删了"
	app.SubmitToTab("test", prompt)
	composed := <-runner.started
	waitNotRunning(t, ctrl)

	if !strings.Contains(composed, "<memory-update>") || !strings.HasSuffix(composed, prompt) {
		t.Fatalf("model input should include memory update followed by prompt, got %q", composed)
	}
	got := app.HistoryForTab("test")
	if len(got) < 2 {
		t.Fatalf("history length = %d, want user + assistant", len(got))
	}
	if got[0].Role != "system" || got[1].Role != "user" {
		t.Fatalf("history roles = %+v, want system then user", got[:min(len(got), 2)])
	}
	if got[1].Content != prompt {
		t.Fatalf("displayed user content = %q, want %q", got[1].Content, prompt)
	}
	if strings.Contains(got[1].Content, "<memory-update>") {
		t.Fatalf("displayed user content leaked memory update: %q", got[1].Content)
	}
}

func TestForkCreatesActiveTabWithoutSwitchingSourceController(t *testing.T) {
	isolateDesktopUserDirs(t)

	workspace := robustTempDir(t)
	if err := os.WriteFile(filepath.Join(workspace, "reasonix.toml"), []byte(""), 0o644); err != nil {
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

func TestCapabilitiesShowsDefaultMCPAsAutomaticIdleNotDisabled(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
			if s.Status != "deferred" || s.StartIntent != "automatic" || s.RuntimeState != "idle" {
				t.Fatalf("default MCP view = %+v, want deferred automatic idle", s)
			}
			return
		}
	}
	t.Fatalf("playwright MCP missing from Capabilities: %+v", view.Servers)
}

func TestDesktopSharedHostBackgroundMCPAutoConnectsOnBoot(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	srv := desktopMCPHTTPServer(t)
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "h"
type = "http"
url = %q
`, srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sharedHost := plugin.NewHost()
	defer sharedHost.Close()
	ctrl, err := boot.Build(ctx, boot.Options{
		WorkspaceRoot: dir,
		SessionDir:    filepath.Join(dir, "sessions"),
		SharedHost:    sharedHost,
		Stderr:        io.Discard,
	})
	if err != nil {
		t.Fatalf("boot.Build: %v", err)
	}
	defer ctrl.Close()

	deadline := time.Now().Add(3 * time.Second)
	for !sharedHost.HasClient("h") && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if !sharedHost.HasClient("h") {
		t.Fatalf("background MCP did not auto-connect; connecting=%v failures=%+v", sharedHost.ConnectingServers(), sharedHost.Failures())
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"test": {
			ID:            "test",
			Scope:         "global",
			WorkspaceRoot: dir,
			Ready:         true,
			Ctrl:          ctrl,
			SharedHostKey: dir,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "test"

	view := app.MCPServers()
	if len(view) != 1 || view[0].Name != "h" || view[0].Status != "connected" || view[0].StartIntent != "automatic" || view[0].RuntimeState != "ready" || view[0].Tools != 1 {
		t.Fatalf("MCPServers() = %+v, want h connected automatic ready with one tool", view)
	}
}

func TestMCPServersMatchesCapabilitiesServerProjection(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if got, want := app.MCPServers(), app.Capabilities().Servers; !reflect.DeepEqual(got, want) {
		t.Fatalf("MCPServers() = %+v, want Capabilities().Servers %+v", got, want)
	}
}

func TestConfiguredMCPWithFormerBuiltInNameIsUserServer(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "time"
command = "custom-time"
args = ["serve"]
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	found := false
	for _, s := range view.Servers {
		if s.Name != "time" {
			continue
		}
		found = true
		if s.BuiltIn || !s.Configured || s.Command != "custom-time" || !reflect.DeepEqual(s.Args, []string{"serve"}) {
			t.Fatalf("configured time view = %+v, want ordinary user MCP config", s)
		}
	}
	if !found {
		t.Fatalf("configured time server missing from Capabilities: %+v", view.Servers)
	}

	if err := app.SetMCPServerEnabled("time", false); err != nil {
		t.Fatalf("SetMCPServerEnabled(time,false): %v", err)
	}
	view = app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "time" {
			if s.Status != "disabled" || s.BuiltIn || s.Command != "custom-time" {
				t.Fatalf("disabled configured time view = %+v, want disabled external config", s)
			}
			return
		}
	}
	t.Fatalf("time missing after disable: %+v", view.Servers)
}

func TestSetMCPServerEnabledSharedHostPreservesSiblingTabs(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	srv := desktopMCPHTTPServer(t)
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(fmt.Sprintf(`
[[plugins]]
name = "h"
type = "http"
url = %q
`, srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sharedHost := plugin.NewHost()
	defer sharedHost.Close()
	tools, err := sharedHost.Add(ctx, plugin.Spec{Name: "h", Type: "http", URL: srv.URL})
	if err != nil {
		t.Fatalf("sharedHost.Add: %v", err)
	}

	activeRegistry := tool.NewRegistry()
	siblingRegistry := tool.NewRegistry()
	for _, mt := range tools {
		activeRegistry.Add(mt)
		siblingRegistry.Add(mt)
	}
	activeCtrl := control.New(control.Options{Host: sharedHost, Registry: activeRegistry, PluginCtx: context.Background()})
	siblingCtrl := control.New(control.Options{Host: sharedHost, Registry: siblingRegistry, PluginCtx: context.Background()})
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"active": {
			ID:            "active",
			Scope:         "global",
			WorkspaceRoot: dir,
			Ready:         true,
			Ctrl:          activeCtrl,
			SharedHostKey: dir,
			disabledMCP:   map[string]ServerView{},
		},
		"sibling": {
			ID:            "sibling",
			Scope:         "global",
			WorkspaceRoot: dir,
			Ready:         true,
			Ctrl:          siblingCtrl,
			SharedHostKey: dir,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "active"

	if err := app.SetMCPServerEnabled("h", false); err != nil {
		t.Fatalf("SetMCPServerEnabled(h,false): %v", err)
	}
	if _, found := activeRegistry.Get("mcp__h__greet"); found {
		t.Fatal("active tab still has h tools after disabling the shared server")
	}
	if _, found := siblingRegistry.Get("mcp__h__greet"); !found {
		t.Fatal("sibling tab lost h tools when active tab disabled the shared server")
	}
	if !sharedHost.HasClient("h") {
		t.Fatal("shared host client was removed by a per-tab disable")
	}
	view := app.Capabilities()
	if len(view.Servers) != 1 || view.Servers[0].Name != "h" || view.Servers[0].Status != "disabled" {
		t.Fatalf("Capabilities after disable = %+v, want h disabled for the active tab", view.Servers)
	}

	if err := app.SetMCPServerEnabled("h", true); err != nil {
		t.Fatalf("SetMCPServerEnabled(h,true): %v", err)
	}
	if _, found := activeRegistry.Get("mcp__h__greet"); !found {
		t.Fatal("active tab did not re-register h tools from the existing shared client")
	}
	view = app.Capabilities()
	if len(view.Servers) != 1 || view.Servers[0].Name != "h" || view.Servers[0].Status != "connected" {
		t.Fatalf("Capabilities after re-enable = %+v, want h connected for the active tab", view.Servers)
	}
}

func TestSetMCPServerEnabledRejectsBackgroundJobs(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	app.setTestCtrl(newBackgroundJobController(t, "mcp-enabled-job"), "")

	err := app.SetMCPServerEnabled("time", false)
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("SetMCPServerEnabled with background job error = %v, want active-work guard", err)
	}
	if tab := app.activeTab(); tab == nil || len(tab.disabledMCP) != 0 {
		t.Fatalf("disabled MCP state changed after rejected toggle: %+v", tab)
	}
}

func TestEditAndRemoveConfiguredMCPWithBuiltInName(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "time"
command = "custom-time"
args = ["serve"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.UpdateMCPServer("time", MCPServerInput{
		Name:      "time",
		Transport: "stdio",
		Command:   "updated-time",
		Args:      []string{"run"},
	}); err != nil {
		t.Fatalf("UpdateMCPServer(time): %v", err)
	}
	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	updated, ok := findPluginEntry(cfg.Plugins, "time")
	if !ok || updated.Command != "updated-time" || !reflect.DeepEqual(updated.Args, []string{"run"}) {
		t.Fatalf("updated time plugin = %+v, found=%v", updated, ok)
	}

	if err := app.RemoveMCPServer("time"); err != nil {
		t.Fatalf("RemoveMCPServer(time): %v", err)
	}
	cfg, err = config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findPluginEntry(cfg.Plugins, "time"); ok {
		t.Fatalf("time plugin still configured after remove: %+v", cfg.Plugins)
	}
}

func TestRemoveMCPServerDeletesProjectMCPJSONEntry(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
  "mcpServers": {
    "codegraph": { "command": "codegraph", "args": ["serve", "--mcp"] },
    "keep": { "command": "keep-mcp" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.RemoveMCPServer("codegraph"); err != nil {
		t.Fatalf("RemoveMCPServer(.mcp.json codegraph): %v", err)
	}
	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findPluginEntry(cfg.Plugins, "codegraph"); ok {
		t.Fatalf("codegraph still merged after remove: %+v", cfg.Plugins)
	}
	if _, ok := findPluginEntry(cfg.Plugins, "keep"); !ok {
		t.Fatalf("unrelated .mcp.json server should be preserved: %+v", cfg.Plugins)
	}
}

func TestUpdateMCPServerEditsProjectMCPJSONEntry(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
  "mcpServers": {
    "codegraph": { "command": "codegraph", "args": ["serve", "--mcp"] }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.UpdateMCPServer("codegraph", MCPServerInput{
		Name:      "codegraph",
		Transport: "stdio",
		Command:   "reasonix-missing-mcp-binary",
		Args:      []string{"serve", "--mcp"},
		Env:       map[string]string{"CODEGRAPH_LOG": "debug"},
	}); err != nil {
		t.Fatalf("UpdateMCPServer(.mcp.json codegraph): %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	got := doc.MCPServers["codegraph"]
	if got.Command != "reasonix-missing-mcp-binary" || !reflect.DeepEqual(got.Args, []string{"serve", "--mcp"}) || got.Env["CODEGRAPH_LOG"] != "debug" {
		t.Fatalf(".mcp.json codegraph = %+v, want updated command/args/env", got)
	}
	if _, ok := findPluginEntry(config.LoadForEdit(config.UserConfigPath()).Plugins, "codegraph"); ok {
		t.Fatalf(".mcp.json update should not create a user config shadow entry")
	}
}

func TestAddMCPServerPersistsRemoteHeaders(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("STRIPE_TOKEN", "stripe-test-token")
	srv := desktopMCPHTTPServer(t)
	defer srv.Close()

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	tools, err := app.AddMCPServer(MCPServerInput{
		Name:      "stripe",
		Transport: "http",
		URL:       srv.URL,
		Headers: map[string]string{
			"Authorization": "Bearer ${STRIPE_TOKEN}",
			"X-Org":         "team",
		},
	})
	if err != nil {
		t.Fatalf("AddMCPServer(stripe): %v", err)
	}
	if tools != 1 {
		t.Fatalf("tools = %d, want 1", tools)
	}

	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := findPluginEntry(cfg.Plugins, "stripe")
	if !ok {
		t.Fatalf("stripe plugin missing from config: %+v", cfg.Plugins)
	}
	if p.Type != "http" || p.URL != srv.URL {
		t.Fatalf("stripe plugin transport = %q url = %q", p.Type, p.URL)
	}
	if p.Headers["Authorization"] != "Bearer ${STRIPE_TOKEN}" || p.Headers["X-Org"] != "team" {
		t.Fatalf("stripe headers = %+v", p.Headers)
	}

	view := app.MCPServers()
	for _, s := range view {
		if s.Name == "stripe" {
			if !reflect.DeepEqual(s.HeaderKeys, []string{"Authorization", "X-Org"}) {
				t.Fatalf("stripe header keys = %+v", s.HeaderKeys)
			}
			return
		}
	}
	t.Fatalf("stripe MCP missing from view: %+v", view)
}

func TestCapabilitiesMarksBackgroundRemoteMCPAuthPossible(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
			if s.Status != "deferred" || s.StartIntent != "automatic" || s.RuntimeState != "idle" || s.AuthStatus != "possible" || s.AuthURL != "https://mcp.dida365.com" {
				t.Fatalf("dida auth diagnosis = %+v", s)
			}
			return
		}
	}
	t.Fatalf("dida MCP missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesDoesNotMarkRemoteMCPWithAuthHeaderPossible(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
			if s.Status != "deferred" || s.StartIntent != "automatic" || s.RuntimeState != "idle" || s.AuthStatus != "possible" {
				t.Fatalf("figma should return to background possible auth: %+v", s)
			}
			return
		}
	}
	t.Fatalf("figma MCP missing from Capabilities: %+v", view.Servers)
}

func TestUpdateMCPServerMigratesLegacyTierToBackground(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp"]
env = { TOKEN = "${PLAYWRIGHT_TOKEN}" }
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

	if err := app.UpdateMCPServer("playwright", MCPServerInput{
		Name:      "playwright",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"server.js"},
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
	if userPlugin.Tier != "" {
		t.Fatalf("user plugin tier = %q, want migrated empty", userPlugin.Tier)
	}
	projectCfg := config.LoadForEdit(filepath.Join(dir, "reasonix.toml"))
	if _, ok := findPluginEntry(projectCfg.Plugins, "playwright"); ok {
		t.Fatalf("project plugin should be removed after desktop migration: %+v", projectCfg.Plugins)
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "playwright" {
			if s.Status != "failed" {
				t.Fatalf("updated MCP status = %q, want failed after immediate reconnect attempt; server = %+v", s.Status, s)
			}
			if s.Command != "node" || len(s.Args) != 1 || s.Args[0] != "server.js" {
				t.Fatalf("server command not refreshed: %+v", s)
			}
			return
		}
	}
	t.Fatalf("playwright MCP missing from Capabilities: %+v", view.Servers)
}

func TestUpdateMCPServerSplitsPastedCommandLine(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "playwright"
command = "npx"
args = ["-y", "@playwright/mcp"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.UpdateMCPServer("playwright", MCPServerInput{
		Name:      "playwright",
		Transport: "stdio",
		Command:   "npx -y @modelcontextprotocol/server-filesystem .",
	}); err != nil {
		t.Fatalf("UpdateMCPServer: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Plugins[0]
	if p.Command != "npx" {
		t.Fatalf("command = %q, want npx", p.Command)
	}
	if got := strings.Join(p.Args, "\x00"); got != strings.Join([]string{"-y", "@modelcontextprotocol/server-filesystem", "."}, "\x00") {
		t.Fatalf("args = %v", p.Args)
	}
}

func TestUpdateMCPServerRecordsReconnectFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "broken"
command = "npx"
tier = "background"
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
	if got := cfg.Plugins[0].Tier; got != "" {
		t.Fatalf("updated tier = %q, want migrated empty", got)
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

func TestReconnectMCPServerClearsInitializingPlaceholderAndRecordsFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "codegraph"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tool.NewRegistry()
	reg.Add(desktopFakeTool{name: "mcp__codegraph__connect"})
	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost(), Registry: reg}), "")
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	foundIdle := false
	for _, s := range view.Servers {
		if s.Name == "codegraph" {
			foundIdle = true
			if s.Status != "deferred" || s.StartIntent != "automatic" || s.RuntimeState != "idle" {
				t.Fatalf("initial codegraph server = %+v, want automatic idle background state", s)
			}
		}
	}
	if !foundIdle {
		t.Fatalf("codegraph missing before reconnect: %+v", view.Servers)
	}
	if _, ok := reg.Get("mcp__codegraph__connect"); !ok {
		t.Fatal("test setup expected stale codegraph connect placeholder")
	}

	if err := app.ReconnectMCPServer("codegraph"); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("ReconnectMCPServer error = %v, want missing command", err)
	}
	if _, ok := reg.Get("mcp__codegraph__connect"); ok {
		t.Fatalf("stale codegraph placeholder still registered after reconnect failure; names=%v", reg.Names())
	}
	if !mcpFailed(app.activeCtrl(), "codegraph") {
		t.Fatalf("Host.Failures() = %+v, want codegraph failure recorded", app.activeCtrl().Host().Failures())
	}

	view = app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "codegraph" {
			if s.Status != "failed" || s.Error == "" {
				t.Fatalf("codegraph after failed reconnect = %+v, want failed with error", s)
			}
			return
		}
	}
	t.Fatalf("codegraph missing after reconnect: %+v", view.Servers)
}

func TestSetMCPServerTierRecordsConnectFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
		t.Fatalf("SetMCPServerTier legacy binding: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Plugins[0].Tier; got != "" {
		t.Fatalf("saved tier = %q, want migrated empty", got)
	}
	userCfg := config.LoadForEdit(config.UserConfigPath())
	userPlugin, ok := findPluginEntry(userCfg.Plugins, "broken")
	if !ok {
		t.Fatalf("broken should be migrated to user config: %+v", userCfg.Plugins)
	}
	if userPlugin.Tier != "" {
		t.Fatalf("user plugin tier = %q, want migrated empty", userPlugin.Tier)
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

func TestSetMCPServerTierRejectsBackgroundJobsBeforeSavingConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[[plugins]]
name = "broken"
command = "reasonix-missing-mcp-binary"
tier = "lazy"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(newBackgroundJobController(t, "mcp-tier-job"), "")

	err := app.SetMCPServerTier("broken", "background")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("SetMCPServerTier with background job error = %v, want active-work guard", err)
	}
	data, readErr := os.ReadFile(config.UserConfigPath())
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if !strings.Contains(string(data), `tier = "lazy"`) {
		t.Fatalf("plugin config changed after rejected tier update:\n%s", data)
	}
}

func TestCapabilitiesMigratesFailedMCPConfiguredTierAfterRestart(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
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
			if s.Tier != "background" {
				t.Fatalf("server tier = %q, want migrated background default", s.Tier)
			}
			if !s.Configured {
				t.Fatalf("server configured = false, want true; server = %+v", s)
			}
			return
		}
	}
	t.Fatalf("broken MCP missing from Capabilities: %+v", view.Servers)
}

func TestRunShellForTabRoutesToRequestedTab(t *testing.T) {
	isolateDesktopUserDirs(t)

	activeEvents := make(chan event.Event, 16)
	inactiveEvents := make(chan event.Event, 16)
	activeCtrl := control.New(control.Options{Sink: event.FuncSink(func(e event.Event) { activeEvents <- e })})
	inactiveCtrl := control.New(control.Options{Sink: event.FuncSink(func(e event.Event) { inactiveEvents <- e })})
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

	app.RunShellForTab("inactive", "echo route-test")

	sawDispatch := false
	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-inactiveEvents:
			if e.Kind == event.ToolDispatch && strings.Contains(e.Tool.Args, "route-test") {
				sawDispatch = true
			}
			if e.Kind == event.TurnDone {
				if !sawDispatch {
					t.Fatal("inactive tab finished without receiving shell dispatch")
				}
				select {
				case active := <-activeEvents:
					t.Fatalf("active tab received event for inactive shell: %+v", active)
				default:
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for inactive shell turn")
		}
	}
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

func startNonCooperativeSessionJob(t *testing.T, jm *jobs.Manager, sessionPath string) func() {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	jm.StartForSession(agent.BranchID(sessionPath), "bash", "stuck job", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		<-release
		return "", ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background job never started")
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		close(release)
	}
}

func waitNotRunning(t *testing.T, ctrl control.SessionAPI) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("controller still running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newBackgroundJobController(t *testing.T, label string) *control.Controller {
	t.Helper()
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, label+".jsonl")
	jm := jobs.NewManager(event.Discard)
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "test", Jobs: jm})
	t.Cleanup(ctrl.Close)
	jm.StartForSession(agent.BranchID(path), "bash", label, func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	return ctrl
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

func TestSessionActionsWithoutControllerReturnError(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	if err := app.NewSession(); err == nil {
		t.Error("NewSession with no controller must surface an error, not silently no-op")
	}
	if err := app.ClearSession(); err == nil {
		t.Error("ClearSession with no controller must surface an error")
	}

	app = &App{
		tabs:        map[string]*WorkspaceTab{"t1": {ID: "t1", StartupErr: "boot exploded"}},
		activeTabID: "t1",
	}
	err := app.NewSession()
	if err == nil || !strings.Contains(err.Error(), "boot exploded") {
		t.Errorf("error should carry the tab's startup failure, got %v", err)
	}
}

// --- Prompt history scanning tests ------------------------------------------

func identityPromptDisplay(text string) string { return text }

// TestCollectPromptHistoryEntriesLegacyEvent verifies that the legacy event format
// {"kind":"user.message","text":"..."} is correctly extracted.
func TestCollectPromptHistoryEntriesLegacyEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"user.message","text":"hello world"}
{"kind":"user.message","text":"second prompt"}
{"kind":"model.final","content":"response"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", entries[0].Text)
	}
	if entries[1].Text != "second prompt" {
		t.Errorf("expected 'second prompt', got %q", entries[1].Text)
	}
	if entries[0].Turn != 0 || entries[1].Turn != 1 {
		t.Errorf("expected turns 0,1; got %d,%d", entries[0].Turn, entries[1].Turn)
	}
	if entries[0].SessionPath != path {
		t.Errorf("expected session path %q, got %q", path, entries[0].SessionPath)
	}
}

// TestCollectPromptHistoryEntriesEarlyEvent verifies that the migrated legacy event
// format {"type":"user.message","text":"..."} is correctly extracted.
func TestCollectPromptHistoryEntriesEarlyEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user.message","text":"v0 prompt"}
{"type":"model.final","content":"response"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Text != "v0 prompt" {
		t.Errorf("expected 'v0 prompt', got %q", entries[0].Text)
	}
}

// TestCollectPromptHistoryEntriesProviderMessage verifies that the current
// provider.Message format {"role":"user","content":"..."} is correctly extracted.
func TestCollectPromptHistoryEntriesProviderMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"hello from provider"}
{"role":"assistant","content":"response"}
{"role":"user","content":"another prompt"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Text != "hello from provider" {
		t.Errorf("expected 'hello from provider', got %q", entries[0].Text)
	}
	if entries[1].Text != "another prompt" {
		t.Errorf("expected 'another prompt', got %q", entries[1].Text)
	}
}

// TestCollectPromptHistoryEntriesMixedFormats verifies that both formats in the
// same file are extracted.
func TestCollectPromptHistoryEntriesMixedFormats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"user.message","text":"legacy prompt"}
{"role":"user","content":"modern prompt"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Text != "legacy prompt" {
		t.Errorf("expected 'legacy prompt', got %q", entries[0].Text)
	}
	if entries[1].Text != "modern prompt" {
		t.Errorf("expected 'modern prompt', got %q", entries[1].Text)
	}
}

func TestCollectPromptHistoryEntriesReadsEventTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	rfcTime := time.Date(2026, 6, 14, 10, 30, 5, 6_000_000, time.UTC)
	if err := os.WriteFile(path, []byte(`{"kind":"user.message","text":"legacy timed","time":1800000000123}
{"role":"user","content":"modern timed","createdAt":`+strconv.Quote(rfcTime.Format(time.RFC3339Nano))+`}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].At != 1800000000123 {
		t.Errorf("numeric event time = %d, want 1800000000123", entries[0].At)
	}
	if entries[1].At != rfcTime.UnixMilli() {
		t.Errorf("RFC3339 event time = %d, want %d", entries[1].At, rfcTime.UnixMilli())
	}
}

// TestCollectPromptHistoryEntriesUsesDisplayResolver verifies history recall uses
// the user-visible prompt text, not the controller-expanded model input.
func TestCollectPromptHistoryEntriesUsesDisplayResolver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	expanded := "<memory-update>\nSaved memory\n</memory-update>\n\nvisible prompt"
	if err := os.WriteFile(path, []byte(`{"role":"user","content":`+strconv.Quote(expanded)+`}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recordSessionDisplay(dir, path, expanded, "visible prompt"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, sessionDisplayResolver(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Text != "visible prompt" {
		t.Errorf("expected visible prompt, got %q", entries[0].Text)
	}
}

func TestCollectPromptHistoryEntriesSkipsSyntheticMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"Plan approved — plan mode is off"}
{"role":"user","content":"real prompt"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Text != "real prompt" {
		t.Errorf("expected real prompt, got %q", entries[0].Text)
	}
}

// TestCollectPromptHistoryEntriesNoUserMessages verifies that a file with only
// assistant/tool messages returns no entries.
func TestCollectPromptHistoryEntriesNoUserMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"model.final","content":"response"}
{"kind":"tool.result","output":"done"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestCollectPromptHistoryEntriesEmptyFile verifies that an empty JSONL file
// returns no entries without error.
func TestCollectPromptHistoryEntriesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := collectPromptHistoryEntries(path, info, identityPromptDisplay)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestScanPromptHistoryFromDir verifies that scanPromptHistoryFromDir scans
// multiple JSONL files and returns prompts newest-first.
func TestScanPromptHistoryFromDir(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{"t1": {ID: "t1", Ctrl: nil, WorkspaceRoot: ""}}}
	_ = app

	dir := t.TempDir()
	// Write two session files with different mtimes (sleep to ensure ordering).
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte(`{"role":"user","content":"older prompt"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "b.jsonl"), []byte(`{"role":"user","content":"newer prompt"}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := app.scanPromptHistoryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest-first: "newer prompt" should be first.
	if entries[0].Text != "newer prompt" {
		t.Errorf("expected 'newer prompt' first, got %q", entries[0].Text)
	}
	if entries[1].Text != "older prompt" {
		t.Errorf("expected 'older prompt' second, got %q", entries[1].Text)
	}
}

func TestScanPromptHistoryFromDirUsesSessionActivityBeforeEventInterleaving(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	base := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	early := filepath.Join(dir, "early.jsonl")
	late := filepath.Join(dir, "late.jsonl")

	if err := os.WriteFile(early, []byte(fmt.Sprintf(`{"role":"user","content":"early first","time":%d}
{"role":"assistant","content":"ok"}
{"role":"user","content":"early second","time":%d}
`, base.UnixMilli(), base.Add(time.Minute).UnixMilli())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(late, []byte(fmt.Sprintf(`{"role":"user","content":"late newest","time":%d}
`, base.Add(2*time.Minute).UnixMilli())), 0o644); err != nil {
		t.Fatal(err)
	}
	// Invert file mtimes: session activity should keep each session grouped
	// before event timestamps are considered within that session.
	if err := os.Chtimes(early, base.Add(3*time.Hour), base.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(late, base.Add(-3*time.Hour), base.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	entries, err := app.scanPromptHistoryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	want := []string{"early second", "early first", "late newest"}
	for i, w := range want {
		if entries[i].Text != w {
			t.Fatalf("entries[%d] = %q, want %q; all=%+v", i, entries[i].Text, w, entries)
		}
	}
}

func TestScanPromptHistoryFromDirUsesBranchMetaActivityFallback(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	base := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	early := filepath.Join(dir, "early.jsonl")
	late := filepath.Join(dir, "late.jsonl")

	if err := os.WriteFile(early, []byte(`{"role":"user","content":"early first"}
{"role":"assistant","content":"ok"}
{"role":"user","content":"early second"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(late, []byte(`{"role":"user","content":"late newest"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(early, agent.BranchMeta{
		CreatedAt: base,
		UpdatedAt: base.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(late, agent.BranchMeta{
		CreatedAt: base.Add(time.Minute),
		UpdatedAt: base.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	// Invert file mtimes: branch UpdatedAt should be the activity clock.
	if err := os.Chtimes(early, base.Add(3*time.Hour), base.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(late, base.Add(-3*time.Hour), base.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	entries, err := app.scanPromptHistoryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	want := []string{"late newest", "early second", "early first"}
	for i, w := range want {
		if entries[i].Text != w {
			t.Fatalf("entries[%d] = %q, want %q; all=%+v", i, entries[i].Text, w, entries)
		}
	}
}

func TestScanPromptHistoryFromDirSkipsEmptyOrderedSessions(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	base := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	empty := filepath.Join(dir, "empty.jsonl")
	real := filepath.Join(dir, "real.jsonl")

	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte(`{"role":"user","content":"real prompt"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(empty, agent.BranchMeta{
		CreatedAt: base,
		UpdatedAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(real, agent.BranchMeta{
		CreatedAt: base,
		UpdatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := app.scanPromptHistoryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != "real prompt" {
		t.Fatalf("entries = %+v, want only real prompt after skipping empty session", entries)
	}
}

func TestScanPromptHistoryUsesCurrentSessionBeforeCrossSession(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.jsonl")
	other := filepath.Join(dir, "other.jsonl")
	if err := os.WriteFile(current, []byte(`{"role":"user","content":"current first"}
{"role":"assistant","content":"ok"}
{"role":"user","content":"current second"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(`{"role":"user","content":"other newest"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	if err := agent.SaveBranchMetaPreserveUpdated(current, agent.BranchMeta{
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(other, agent.BranchMeta{
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: current, Label: "test"})
	defer ctrl.Close()
	app.setTestCtrl(ctrl, "")

	result, err := app.ScanPromptHistory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected current-session entries followed by cross-session fallback, got %d: %+v", len(result.Entries), result.Entries)
	}
	want := []string{"current second", "current first", "other newest"}
	for i, w := range want {
		if result.Entries[i].Text != w {
			t.Fatalf("entries[%d] = %q, want %q; all=%+v", i, result.Entries[i].Text, w, result.Entries)
		}
	}
}

func TestScanPromptHistoryPaginatesCurrentSessionBeforeCrossSession(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current.jsonl")
	other := filepath.Join(dir, "other.jsonl")
	var lines []byte
	for i := range 55 {
		lines = append(lines, []byte(fmt.Sprintf(`{"role":"user","content":"current %d"}
`, i))...)
	}
	if err := os.WriteFile(current, lines, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte(`{"role":"user","content":"other newest"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 14, 8, 0, 0, 0, time.UTC)
	if err := agent.SaveBranchMetaPreserveUpdated(current, agent.BranchMeta{
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMetaPreserveUpdated(other, agent.BranchMeta{
		CreatedAt: now.Add(time.Minute),
		UpdatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: current, Label: "test"})
	defer ctrl.Close()
	app.setTestCtrl(ctrl, "")

	result, err := app.ScanPromptHistory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != promptHistoryPageLimit {
		t.Fatalf("expected %d entries, got %d", promptHistoryPageLimit, len(result.Entries))
	}
	if result.Entries[0].Text != "current 54" {
		t.Fatalf("first entry = %q, want current 54", result.Entries[0].Text)
	}
	if result.Entries[len(result.Entries)-1].Text != "current 5" {
		t.Fatalf("last first-page entry = %q, want current 5", result.Entries[len(result.Entries)-1].Text)
	}
	if !result.HasOlder || result.OlderCursor == "" {
		t.Fatalf("first page should expose an older cursor: %+v", result)
	}
	for _, entry := range result.Entries {
		if entry.Text == "other newest" {
			t.Fatalf("cross-session entry appeared before current-session page was exhausted: %+v", result.Entries)
		}
	}

	nextRequest, err := json.Marshal(promptHistoryRequest{Cursor: result.OlderCursor})
	if err != nil {
		t.Fatal(err)
	}
	next, err := app.ScanPromptHistory(string(nextRequest))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"current 4", "current 3", "current 2", "current 1", "current 0", "other newest"}
	if len(next.Entries) != len(want) {
		t.Fatalf("second page entries = %+v, want %d entries", next.Entries, len(want))
	}
	for i, w := range want {
		if next.Entries[i].Text != w {
			t.Fatalf("second page entries[%d] = %q, want %q; all=%+v", i, next.Entries[i].Text, w, next.Entries)
		}
	}
}

func TestScanPromptHistoryFromDirReadsAllEntriesForInternalHelper(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	var lines []byte
	for i := range 250 {
		lines = append(lines, []byte(fmt.Sprintf(`{"role":"user","content":"prompt %d"}
`, i))...)
	}
	if err := os.WriteFile(filepath.Join(dir, "many.jsonl"), lines, 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := app.scanPromptHistoryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 250 {
		t.Fatalf("expected 250 entries, got %d", len(entries))
	}
	if entries[0].Text != "prompt 249" {
		t.Errorf("expected newest 'prompt 249' first, got %q", entries[0].Text)
	}
}

// TestScanPromptHistoryFromDirEmpty verifies an empty directory returns nil.
func TestScanPromptHistoryFromDirEmpty(t *testing.T) {
	app := &App{}
	dir := t.TempDir()
	entries, err := app.scanPromptHistoryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

// TestScanPromptHistoryCacheHit verifies that ScanPromptHistory returns nil
// on cache hit (nonce matches).
func TestScanPromptHistoryCacheHit(t *testing.T) {
	app := &App{tabs: map[string]*WorkspaceTab{}}
	result, err := app.ScanPromptHistory("")
	if err != nil {
		t.Fatal(err)
	}
	nonce := result.Nonce
	if nonce == "" {
		t.Error("expected a non-empty nonce on first call")
	}

	// Second call with the same nonce should be a cache hit (nil entries).
	result2, err := app.ScanPromptHistory(nonce)
	if err != nil {
		t.Fatal(err)
	}
	if result2.Entries != nil {
		t.Error("expected nil entries on cache hit")
	}
	if result2.Nonce != nonce {
		t.Errorf("expected nonce %q unchanged, got %q", nonce, result2.Nonce)
	}
}

func TestScanPromptHistoryCacheIsScopedBySessionDir(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	pathA := filepath.Join(dirA, "a.jsonl")
	pathB := filepath.Join(dirB, "b.jsonl")
	if err := os.WriteFile(pathA, []byte(`{"role":"user","content":"workspace A"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(`{"role":"user","content":"workspace B"}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrlA := control.New(control.Options{SessionDir: dirA, SessionPath: pathA, Label: "test"})
	ctrlB := control.New(control.Options{SessionDir: dirB, SessionPath: pathB, Label: "test"})
	defer ctrlA.Close()
	defer ctrlB.Close()

	app.setTestCtrl(ctrlA, "")
	first, err := app.ScanPromptHistory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.Entries[0].Text != "workspace A" {
		t.Fatalf("first entries = %+v, want workspace A", first.Entries)
	}

	app.setTestCtrl(ctrlB, "")
	second, err := app.ScanPromptHistory(first.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if second.Entries == nil {
		t.Fatal("expected rescan after session dir changes, got cache hit")
	}
	if len(second.Entries) != 1 || second.Entries[0].Text != "workspace B" {
		t.Fatalf("second entries = %+v, want workspace B", second.Entries)
	}
}

func TestScanPromptHistoryCacheIsScopedBySessionPath(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.jsonl")
	pathB := filepath.Join(dir, "b.jsonl")
	if err := os.WriteFile(pathA, []byte(`{"role":"user","content":"session A"}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(`{"role":"user","content":"session B"}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	ctrlA := control.New(control.Options{SessionDir: dir, SessionPath: pathA, Label: "test"})
	ctrlB := control.New(control.Options{SessionDir: dir, SessionPath: pathB, Label: "test"})
	defer ctrlA.Close()
	defer ctrlB.Close()

	app.setTestCtrl(ctrlA, "")
	first, err := app.ScanPromptHistory("")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || first.Entries[0].Text != "session A" || first.Entries[1].Text != "session B" {
		t.Fatalf("first entries = %+v, want session A followed by session B", first.Entries)
	}

	app.setTestCtrl(ctrlB, "")
	second, err := app.ScanPromptHistory(first.Nonce)
	if err != nil {
		t.Fatal(err)
	}
	if second.Entries == nil {
		t.Fatal("expected rescan after session path changes, got cache hit")
	}
	if len(second.Entries) != 2 || second.Entries[0].Text != "session B" || second.Entries[1].Text != "session A" {
		t.Fatalf("second entries = %+v, want session B followed by session A", second.Entries)
	}
}
