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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/plugin"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/store"
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

func primarySessionFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if store.IsSessionTranscriptName(filepath.Base(path)) {
			out = append(out, path)
		}
	}
	return out
}

func readConflictLogLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read conflict log: %v", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func setDesktopTestCredential(t *testing.T, key, value string) {
	t.Helper()
	if _, err := config.SetCredential(key, value); err != nil {
		t.Fatalf("SetCredential(%s): %v", key, err)
	}
}

func TestNeedsOnboardingIgnoresInheritedEnv(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv(onboardingKeyEnv, "inherited-key")

	app := NewApp()
	if !app.NeedsOnboarding() {
		t.Fatal("NeedsOnboarding should require a key saved in Reasonix global .env")
	}
	setDesktopTestCredential(t, onboardingKeyEnv, "saved-key")
	if app.NeedsOnboarding() {
		t.Fatal("NeedsOnboarding should be false after saving the global credential")
	}
}

func TestNeedsOnboardingTreatsBlankSavedKeyAsMissing(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserCredentialsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserCredentialsPath(), []byte(onboardingKeyEnv+"=\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	if !app.NeedsOnboarding() {
		t.Fatal("NeedsOnboarding should require a non-empty saved credential")
	}
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
	resetWorkspaceGitBranchMetaCacheForTest(t)

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
	deadline := time.Now().Add(time.Second)
	for {
		if got = app.MetaForTab("tab-1"); got.GitBranch == "feature/meta" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gitBranch = %q, want feature/meta after async refresh", got.GitBranch)
		}
		time.Sleep(10 * time.Millisecond)
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

func TestBackgroundCloseRequiresRestorePath(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		trayStarted bool
		trayReady   bool
		want        bool
	}{
		{name: "macOS restores from Dock", goos: "darwin", trayStarted: false, trayReady: false, want: true},
		{name: "Windows tray ready", goos: "windows", trayStarted: true, trayReady: true, want: true},
		{name: "Windows tray started but not ready", goos: "windows", trayStarted: true, trayReady: false, want: false},
		{name: "Linux tray ready", goos: "linux", trayStarted: true, trayReady: true, want: true},
		{name: "Linux tray started but not ready", goos: "linux", trayStarted: true, trayReady: false, want: false},
		{name: "Linux no tray", goos: "linux", trayStarted: false, trayReady: false, want: false},
		{name: "other Unix no tray", goos: "freebsd", trayStarted: false, trayReady: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backgroundCloseHasRestorePathFor(tt.goos, tt.trayStarted, tt.trayReady); got != tt.want {
				t.Fatalf("backgroundCloseHasRestorePathFor(%q, %v, %v) = %v, want %v", tt.goos, tt.trayStarted, tt.trayReady, got, tt.want)
			}
		})
	}
}

func TestBackgroundCloseReadySignalRequiresCurrentReadyState(t *testing.T) {
	app := NewApp()
	tray := newDesktopTray()
	app.mu.Lock()
	app.tray = tray
	app.mu.Unlock()

	if app.waitForTrayReady(0) {
		t.Fatal("tray should not be ready before its ready signal")
	}

	tray.markReady()
	if app.waitForTrayReady(0) {
		t.Fatal("closed ready signal should not count without the current ready state")
	}

	app.mu.Lock()
	app.trayReady = true
	app.mu.Unlock()
	if !app.waitForTrayReady(0) {
		t.Fatal("ready state should be accepted after the tray is marked ready")
	}

	app.mu.Lock()
	app.trayReady = false
	app.mu.Unlock()
	if app.waitForTrayReady(0) {
		t.Fatal("stale ready signal should not count after the tray exits")
	}
}

func TestBackgroundCloseWaitsForTrayReadySignal(t *testing.T) {
	app := NewApp()
	tray := newDesktopTray()
	app.mu.Lock()
	app.tray = tray
	app.mu.Unlock()

	go func() {
		time.Sleep(10 * time.Millisecond)
		app.mu.Lock()
		app.trayReady = true
		app.mu.Unlock()
		tray.markReady()
	}()

	if !app.waitForTrayReady(200 * time.Millisecond) {
		t.Fatal("waitForTrayReady should observe the tray becoming ready")
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

func TestSettingsIgnoresActiveWorkspaceDotEnvCredentialsWithUserConfig(t *testing.T) {
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
			if p.KeySet {
				t.Fatalf("workspace provider keySet = true, want false because workspace .env is ignored: %+v", p)
			}
			if p.Configured {
				t.Fatalf("workspace provider configured = true, want false because workspace .env is ignored: %+v", p)
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
		if !p.KeySet || !strings.Contains(p.KeySource, "Reasonix credentials") {
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
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
	if got := app.Settings().Agent.MaxSubagentDepth; got != agent.DefaultMaxSubagentDepth {
		t.Fatalf("default max subagent depth = %d, want %d", got, agent.DefaultMaxSubagentDepth)
	}
	if err := app.SetSubagentModel("deepseek/deepseek-v4-pro"); err != nil {
		t.Fatalf("SetSubagentModel: %v", err)
	}
	if err := app.SetSubagentEffort("max"); err != nil {
		t.Fatalf("SetSubagentEffort: %v", err)
	}
	if err := app.SetMaxSubagentDepth(1); err != nil {
		t.Fatalf("SetMaxSubagentDepth(1): %v", err)
	}
	if err := app.SetMaxSubagentDepth(2); err != nil {
		t.Fatalf("SetMaxSubagentDepth(2): %v", err)
	}

	got := app.Settings()
	if got.SubagentModel != "deepseek/deepseek-v4-pro" || got.SubagentEffort != "max" {
		t.Fatalf("subagent settings = model:%q effort:%q", got.SubagentModel, got.SubagentEffort)
	}
	if got.Agent.MaxSubagentDepth != 2 {
		t.Fatalf("max subagent depth = %d, want 2", got.Agent.MaxSubagentDepth)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.SubagentModel != "deepseek/deepseek-v4-pro" || cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("saved config = model:%q effort:%q", cfg.Agent.SubagentModel, cfg.Agent.SubagentEffort)
	}
	if cfg.Agent.MaxSubagentDepth != 2 {
		t.Fatalf("saved max_subagent_depth = %d, want 2", cfg.Agent.MaxSubagentDepth)
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")

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

func TestSettingsSurfacesCuratedProviderPresets(t *testing.T) {
	isolateDesktopUserDirs(t)

	view := NewApp().Settings()
	if len(view.ProviderPresets) < 18 {
		t.Fatalf("Settings().ProviderPresets length = %d, want curated custom presets", len(view.ProviderPresets))
	}
	got := map[string]ProviderPresetView{}
	for _, preset := range view.ProviderPresets {
		got[preset.ID] = preset
	}
	for _, curated := range config.CuratedProviderPresets() {
		id := curated.ID
		preset, ok := got[id]
		if !ok {
			t.Fatalf("Settings().ProviderPresets missing %q: %+v", id, view.ProviderPresets)
		}
		if preset.KeyEnv == "" || len(preset.ProviderNames) == 0 || len(preset.Models) == 0 {
			t.Fatalf("preset %q view has missing fields: %+v", id, preset)
		}
	}
}

func providerPresetViewByID(t *testing.T, view SettingsView, id string) ProviderPresetView {
	t.Helper()
	for _, preset := range view.ProviderPresets {
		if preset.ID == id {
			return preset
		}
	}
	t.Fatalf("Settings().ProviderPresets missing %q: %+v", id, view.ProviderPresets)
	return ProviderPresetView{}
}

func TestSettingsMarksPresetAddedWhenSameNameProviderExistsWithoutAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[desktop]
provider_access = []

[[providers]]
name = "mimo-api"
kind = "openai"
base_url = "https://custom.example/v1"
models = ["custom-model"]
default = "custom-model"
api_key_env = "MIMO_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	view := NewApp().Settings()
	presetView := providerPresetViewByID(t, view, "mimo-api")
	if !presetView.Added || presetView.Status != providerPresetStatusNameConflict || !reflect.DeepEqual(presetView.StatusProviderNames, []string{"mimo-api"}) {
		t.Fatalf("mimo-api preset view = %+v, want name-conflict because a different same-name provider exists", presetView)
	}

	var providerView *ProviderView
	for i := range view.Providers {
		if view.Providers[i].Name == "mimo-api" {
			providerView = &view.Providers[i]
			break
		}
	}
	if providerView == nil {
		t.Fatal("mimo-api provider view missing")
	}
	if providerView.Added {
		t.Fatalf("mimo-api provider Added = true, want false until provider_access explicitly enables it")
	}
}

func TestSettingsMarksLegacyEquivalentPresetAsInstalled(t *testing.T) {
	isolateDesktopUserDirs(t)
	preset, ok := config.CuratedProviderPreset("mimo-api")
	if !ok || len(preset.Entries) == 0 {
		t.Fatal("missing mimo-api preset")
	}
	legacy := preset.Entries[0]
	legacy.PresetID = ""
	legacy.PresetVersion = 0
	cfg := config.Default()
	if err := cfg.UpsertProvider(legacy); err != nil {
		t.Fatalf("upsert legacy provider: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	view := NewApp().Settings()
	presetView := providerPresetViewByID(t, view, "mimo-api")
	if !presetView.Added || presetView.Status != providerPresetStatusInstalled || !reflect.DeepEqual(presetView.StatusProviderNames, []string{"mimo-api"}) {
		t.Fatalf("mimo-api preset view = %+v, want installed for legacy equivalent config", presetView)
	}
}

func TestSettingsMarksPresetWithChangedCoreConfigAsModified(t *testing.T) {
	isolateDesktopUserDirs(t)
	preset, ok := config.CuratedProviderPreset("mimo-api")
	if !ok || len(preset.Entries) == 0 {
		t.Fatal("missing mimo-api preset")
	}
	modified := preset.Entries[0]
	modified.BaseURL = "https://custom.example/v1"
	cfg := config.Default()
	if err := cfg.UpsertProvider(modified); err != nil {
		t.Fatalf("upsert modified provider: %v", err)
	}
	cfg.Desktop.ProviderAccess = []string{"mimo-api"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	view := NewApp().Settings()
	presetView := providerPresetViewByID(t, view, "mimo-api")
	if !presetView.Added || presetView.Status != providerPresetStatusInstalledModified || !reflect.DeepEqual(presetView.StatusProviderNames, []string{"mimo-api"}) {
		t.Fatalf("mimo-api preset view = %+v, want installed-modified for edited preset provider", presetView)
	}
}

func TestSettingsMigratesLegacyStepFunPresetBaseURLs(t *testing.T) {
	isolateDesktopUserDirs(t)

	cfg := config.Default()
	stepfun, ok := config.CuratedProviderPreset("stepfun")
	if !ok || len(stepfun.Entries) != 1 {
		t.Fatal("missing stepfun preset")
	}
	stepfunEntry := stepfun.Entries[0]
	stepfunEntry.BaseURL = "https://api.stepfun.ai/step_plan/v1"
	stepfunAnthropic, ok := config.CuratedProviderPreset("stepfun-anthropic")
	if !ok || len(stepfunAnthropic.Entries) != 1 {
		t.Fatal("missing stepfun-anthropic preset")
	}
	stepfunAnthropicEntry := stepfunAnthropic.Entries[0]
	stepfunAnthropicEntry.BaseURL = "https://api.stepfun.ai/step_plan"
	cfg.Providers = append(cfg.Providers, stepfunEntry, stepfunAnthropicEntry)
	cfg.Desktop.ProviderAccess = []string{"stepfun", "stepfun-anthropic"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	view := NewApp().Settings()
	for _, id := range []string{"stepfun", "stepfun-anthropic"} {
		presetView := providerPresetViewByID(t, view, id)
		if !presetView.Added || presetView.Status != providerPresetStatusInstalled {
			t.Fatalf("%s preset view = %+v, want installed after migration", id, presetView)
		}
	}

	migrated := config.LoadForEdit(config.UserConfigPath())
	stepfunEntryView, ok := migrated.Provider("stepfun")
	if !ok {
		t.Fatal("stepfun provider missing after migration")
	}
	if got := stepfunEntryView.BaseURL; got != "https://api.stepfun.com/step_plan/v1" {
		t.Fatalf("stepfun base_url = %q, want official URL", got)
	}
	stepfunAnthropicEntryView, ok := migrated.Provider("stepfun-anthropic")
	if !ok {
		t.Fatal("stepfun-anthropic provider missing after migration")
	}
	if got := stepfunAnthropicEntryView.BaseURL; got != "https://api.stepfun.com/step_plan" {
		t.Fatalf("stepfun-anthropic base_url = %q, want official URL", got)
	}
}

func TestSettingsMarksSimilarProviderPresetWithoutBlockingAdd(t *testing.T) {
	isolateDesktopUserDirs(t)
	preset, ok := config.CuratedProviderPreset("mimo-api")
	if !ok || len(preset.Entries) == 0 {
		t.Fatal("missing mimo-api preset")
	}
	similar := preset.Entries[0]
	similar.Name = "my-mimo"
	similar.PresetID = ""
	similar.PresetVersion = 0
	cfg := config.Default()
	if err := cfg.UpsertProvider(similar); err != nil {
		t.Fatalf("upsert similar provider: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	view := NewApp().Settings()
	presetView := providerPresetViewByID(t, view, "mimo-api")
	if presetView.Added || presetView.Status != providerPresetStatusSimilarExisting || !reflect.DeepEqual(presetView.StatusProviderNames, []string{"my-mimo"}) {
		t.Fatalf("mimo-api preset view = %+v, want non-blocking similar-existing status", presetView)
	}
}

func TestAddProviderPresetAccessSavesEditableProviderAndKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("MIMO_API_KEY", "")
	os.Unsetenv("MIMO_API_KEY")

	if warning, err := NewApp().AddProviderPresetAccess("mimo-api", "sk-mimo"); err != nil {
		t.Fatalf("AddProviderPresetAccess: %v", err)
	} else if warning != "" {
		t.Fatalf("AddProviderPresetAccess warning = %q, want none", warning)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	p, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider not saved")
	}
	if p.Kind != "openai" || p.BaseURL != "https://api.xiaomimimo.com/v1" || p.Default != "mimo-v2.5-pro" {
		t.Fatalf("mimo-api provider after preset add = %+v", p)
	}
	if p.PresetID != "mimo-api" || p.PresetVersion != config.ProviderPresetVersion {
		t.Fatalf("mimo-api preset metadata = %q/%d, want mimo-api/%d", p.PresetID, p.PresetVersion, config.ProviderPresetVersion)
	}
	if !p.NoProxy {
		t.Fatal("mimo-api preset should save no_proxy = true")
	}
	if !p.HasVisionModel("mimo-v2.5") || p.HasVisionModel("mimo-v2.5-pro") {
		t.Fatalf("mimo vision_models = %+v, want only vision-capable MiMo models", p.VisionModels)
	}
	if price := p.PriceForModel("mimo-v2.5-pro"); price == nil || price.Currency != "¥" {
		t.Fatalf("mimo-v2.5-pro price = %+v, want RMB pricing", price)
	}
	if !providerAccessSet(cfg.Desktop.ProviderAccess)["mimo-api"] {
		t.Fatalf("provider_access missing mimo-api: %+v", cfg.Desktop.ProviderAccess)
	}
	data, err := os.ReadFile(config.UserCredentialsPath())
	if err != nil {
		t.Fatalf("read saved credentials: %v", err)
	}
	if !strings.Contains(string(data), "MIMO_API_KEY=sk-mimo") {
		t.Fatalf("saved credentials missing MiMo key:\n%s", data)
	}

	view := NewApp().Settings()
	var presetView *ProviderPresetView
	var providerView *ProviderView
	for i := range view.ProviderPresets {
		if view.ProviderPresets[i].ID == "mimo-api" {
			presetView = &view.ProviderPresets[i]
		}
	}
	for i := range view.Providers {
		if view.Providers[i].Name == "mimo-api" {
			providerView = &view.Providers[i]
		}
	}
	if presetView == nil || !presetView.Added || presetView.Status != providerPresetStatusInstalled || !presetView.KeySet {
		t.Fatalf("mimo-api preset view = %+v, want installed/key-set", presetView)
	}
	if providerView == nil || providerView.BuiltIn || !providerView.Added || !providerView.KeySet {
		t.Fatalf("mimo provider view = %+v, want editable added custom provider with key", providerView)
	}
}

func TestAddProviderPresetAccessDoesNotOverwriteExistingProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("MIMO_API_KEY", "")
	os.Unsetenv("MIMO_API_KEY")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-original")

	cfg := config.Default()
	custom := config.ProviderEntry{
		Name:      "mimo-api",
		Kind:      "openai",
		BaseURL:   "https://custom.example/v1",
		Models:    []string{"custom-model"},
		Default:   "custom-model",
		APIKeyEnv: "MIMO_API_KEY",
		Headers:   map[string]string{"X-Custom": "keep-me"},
	}
	if err := cfg.UpsertProvider(custom); err != nil {
		t.Fatalf("upsert custom provider: %v", err)
	}
	cfg.Desktop.ProviderAccess = []string{"mimo-api"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if warning, err := NewApp().AddProviderPresetAccess("mimo-api", "sk-new"); err == nil {
		t.Fatal("AddProviderPresetAccess unexpectedly overwrote an existing provider")
	} else if !strings.Contains(err.Error(), "provider name(s) already exist") {
		t.Fatalf("AddProviderPresetAccess error = %v, want name-exists guard", err)
	} else if warning != "" {
		t.Fatalf("AddProviderPresetAccess warning = %q, want none on rejected add", warning)
	}

	cfg = config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing after rejected add")
	}
	if got.BaseURL != custom.BaseURL || got.DefaultModel() != custom.DefaultModel() || !reflect.DeepEqual(got.ModelList(), custom.ModelList()) || !reflect.DeepEqual(got.Headers, custom.Headers) {
		t.Fatalf("mimo-api provider was overwritten: %+v, want custom %+v", got, custom)
	}
	data, err := os.ReadFile(config.UserCredentialsPath())
	if err != nil {
		t.Fatalf("read saved credentials: %v", err)
	}
	if strings.Contains(string(data), "sk-new") || !strings.Contains(string(data), "MIMO_API_KEY=sk-original") {
		t.Fatalf("credentials changed after rejected add:\n%s", data)
	}
}

func TestResetProviderPresetAccessOverwritesSameNameProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("MIMO_API_KEY", "")
	os.Unsetenv("MIMO_API_KEY")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-original")

	cfg := config.Default()
	custom := config.ProviderEntry{
		Name:      "mimo-api",
		Kind:      "openai",
		BaseURL:   "https://custom.example/v1",
		Models:    []string{"custom-model"},
		Default:   "custom-model",
		APIKeyEnv: "MIMO_API_KEY",
		Headers:   map[string]string{"X-Custom": "remove-me"},
	}
	if err := cfg.UpsertProvider(custom); err != nil {
		t.Fatalf("upsert custom provider: %v", err)
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := NewApp().ResetProviderPresetAccess("mimo-api"); err != nil {
		t.Fatalf("ResetProviderPresetAccess: %v", err)
	}

	cfg = config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api provider missing after reset")
	}
	if got.BaseURL != "https://api.xiaomimimo.com/v1" || got.DefaultModel() != "mimo-v2.5-pro" || got.PresetID != "mimo-api" || got.PresetVersion != config.ProviderPresetVersion {
		t.Fatalf("mimo-api provider after reset = %+v, want preset template", got)
	}
	if len(got.Headers) != 0 {
		t.Fatalf("mimo-api headers after reset = %+v, want preset headers", got.Headers)
	}
	if !providerAccessSet(cfg.Desktop.ProviderAccess)["mimo-api"] {
		t.Fatalf("provider_access missing mimo-api after reset: %+v", cfg.Desktop.ProviderAccess)
	}
	data, err := os.ReadFile(config.UserCredentialsPath())
	if err != nil {
		t.Fatalf("read saved credentials: %v", err)
	}
	if !strings.Contains(string(data), "MIMO_API_KEY=sk-original") {
		t.Fatalf("credentials changed after reset:\n%s", data)
	}

	presetView := providerPresetViewByID(t, NewApp().Settings(), "mimo-api")
	if !presetView.Added || presetView.Status != providerPresetStatusInstalled {
		t.Fatalf("mimo-api preset view = %+v, want installed after reset", presetView)
	}
}

func TestResetProviderPresetAccessRejectsMissingSameNameProvider(t *testing.T) {
	isolateDesktopUserDirs(t)

	if err := NewApp().ResetProviderPresetAccess("mimo-api"); err == nil {
		t.Fatal("ResetProviderPresetAccess unexpectedly reset a missing provider")
	} else if !strings.Contains(err.Error(), "no same-name provider exists") {
		t.Fatalf("ResetProviderPresetAccess error = %v, want missing same-name provider guard", err)
	}
}

func TestAddEveryProviderPresetAccessInstallsTemplate(t *testing.T) {
	for _, preset := range config.CuratedProviderPresets() {
		preset := preset
		t.Run(preset.ID, func(t *testing.T) {
			isolateDesktopUserDirs(t)

			if warning, err := NewApp().AddProviderPresetAccess(preset.ID, "sk-test"); err != nil {
				t.Fatalf("AddProviderPresetAccess(%q): %v", preset.ID, err)
			} else if warning != "" {
				t.Fatalf("AddProviderPresetAccess(%q) warning = %q, want none", preset.ID, warning)
			}

			cfg := config.LoadForEdit(config.UserConfigPath())
			access := providerAccessSet(cfg.Desktop.ProviderAccess)
			for _, entry := range preset.Entries {
				got, ok := cfg.Provider(entry.Name)
				if !ok {
					t.Fatalf("provider %q from preset %q was not saved", entry.Name, preset.ID)
				}
				if !access[entry.Name] {
					t.Fatalf("provider_access for preset %q missing %q: %+v", preset.ID, entry.Name, cfg.Desktop.ProviderAccess)
				}
				if got.Kind != entry.Kind || got.BaseURL != entry.BaseURL || got.DefaultModel() != entry.DefaultModel() || got.APIKeyEnv != entry.APIKeyEnv || got.AuthHeader != entry.AuthHeader || got.NoProxy != entry.NoProxy {
					t.Fatalf("provider %q core fields = %+v, want template %+v", entry.Name, got, entry)
				}
				if got.PresetID != preset.ID || got.PresetVersion != config.ProviderPresetVersion {
					t.Fatalf("provider %q preset metadata = %q/%d, want %q/%d", entry.Name, got.PresetID, got.PresetVersion, preset.ID, config.ProviderPresetVersion)
				}
				if got.ContextWindow != entry.ContextWindow || got.Thinking != entry.Thinking || got.DefaultEffort != entry.DefaultEffort || got.ReasoningProtocol != entry.ReasoningProtocol {
					t.Fatalf("provider %q capability fields = %+v, want template %+v", entry.Name, got, entry)
				}
				if !reflect.DeepEqual(got.ModelList(), entry.ModelList()) || !reflect.DeepEqual(got.VisionModels, entry.VisionModels) || !reflect.DeepEqual(got.SupportedEfforts, entry.SupportedEfforts) {
					t.Fatalf("provider %q models/capabilities = %+v, want template %+v", entry.Name, got, entry)
				}
				if !reflect.DeepEqual(got.Headers, entry.Headers) || !reflect.DeepEqual(got.ExtraBody, entry.ExtraBody) {
					t.Fatalf("provider %q request extras = %+v, want template %+v", entry.Name, got, entry)
				}
			}

			view := NewApp().Settings()
			var presetView *ProviderPresetView
			for i := range view.ProviderPresets {
				if view.ProviderPresets[i].ID == preset.ID {
					presetView = &view.ProviderPresets[i]
					break
				}
			}
			if presetView == nil || !presetView.Added || presetView.Status != providerPresetStatusInstalled || !presetView.KeySet || !presetView.Configured {
				t.Fatalf("preset view for %q = %+v, want installed/key-set/configured", preset.ID, presetView)
			}
		})
	}
}

func TestAddOfficialProviderAccessRejectsBackgroundJobsBeforeSavingKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "")
	os.Unsetenv("DEEPSEEK_API_KEY")

	app := NewApp()
	app.readyHook = func() {}
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

func TestSetProviderKeyLeaseHeldKeepsCurrentController(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "longcat", Kind: "openai", BaseURL: "https://longcat.example/v1", Model: "longcat-chat", APIKeyEnv: "LONGCAT_API_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "externally-leased-provider-key.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer externalLease.Release()

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_provider",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_provider", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	warning, err := app.SetProviderKey("LONGCAT_API_KEY", "sk-longcat")
	if err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	if !strings.Contains(warning, "current session could not refresh yet") || !strings.Contains(warning, "another Reasonix window") {
		t.Fatalf("SetProviderKey warning = %q, want deferred rebuild warning", warning)
	}
	if strings.Contains(warning, sessionPath) || strings.Contains(warning, "held by") {
		t.Fatalf("SetProviderKey surfaced raw lease details: %v", warning)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("tab controller changed after failed provider-key rebuild")
	}
	if tab.StartupErr != "" {
		t.Fatalf("tab startup error = %q, want unchanged current session", tab.StartupErr)
	}
	if got := tab.Ctrl.History(); len(got) < 2 || got[1].Content != "hello" {
		t.Fatalf("history after failed provider-key rebuild = %+v", got)
	}
	if access := providerAccessSet(config.LoadForEditWithoutCredentials(config.UserConfigPath()).Desktop.ProviderAccess); !access["longcat"] {
		t.Fatalf("provider_access should still persist longcat after key save")
	}
}

func TestSetProviderKeyRebuildSupersedesInFlightStartupBuild(t *testing.T) {
	isolateDesktopUserDirs(t)

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "startup-build-in-flight.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	// Model the async startup build still being in flight: no controller yet,
	// a live build generation, and a cancellable build context.
	buildCtx, buildCancel := context.WithCancel(context.Background())
	const startupGeneration = 1
	tab := &WorkspaceTab{
		ID:              "tab_key_rebuild",
		Scope:           "global",
		SessionPath:     sessionPath,
		model:           "old/old-model",
		buildGeneration: startupGeneration,
		buildCancel:     buildCancel,
		disabledMCP:     map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(tab.releaseSessionLease)

	if _, err := app.SetProviderKey("OLD_MODEL_KEY", "sk-new"); err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	if tab.Ctrl == nil {
		t.Fatal("provider-key rebuild did not install a controller")
	}
	defer tab.Ctrl.Close()

	assertTabBuildSuperseded(t, app, tab, startupGeneration, buildCtx)
}

func TestSaveProviderWithKeyLeaseHeldPersistsCustomProvider(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "externally-leased-custom-provider.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer externalLease.Release()

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_custom_provider",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_custom_provider", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	warning, err := app.SaveProviderWithKey(ProviderView{
		Name:      "proxy",
		Kind:      "openai",
		BaseURL:   "https://proxy.example/v1",
		Models:    []string{"model-a", "model-b"},
		Default:   "model-a",
		APIKeyEnv: "PROXY_API_KEY",
	}, "sk-proxy")
	if err != nil {
		t.Fatalf("SaveProviderWithKey: %v", err)
	}
	if !strings.Contains(warning, "current session could not refresh yet") || !strings.Contains(warning, "another Reasonix window") {
		t.Fatalf("SaveProviderWithKey warning = %q, want deferred rebuild warning", warning)
	}
	if strings.Contains(warning, sessionPath) || strings.Contains(warning, "held by") {
		t.Fatalf("SaveProviderWithKey surfaced raw lease details: %v", warning)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("tab controller changed after failed provider rebuild")
	}
	gotCfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	got, ok := gotCfg.Provider("proxy")
	if !ok {
		t.Fatal("custom provider was not saved")
	}
	if want := []string{"model-a", "model-b"}; !reflect.DeepEqual(got.ModelList(), want) {
		t.Fatalf("custom provider models = %v, want %v", got.ModelList(), want)
	}
	if !providerAccessSet(gotCfg.Desktop.ProviderAccess)["proxy"] {
		t.Fatalf("provider_access = %+v, want proxy", gotCfg.Desktop.ProviderAccess)
	}
	data, err := os.ReadFile(config.UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if !strings.Contains(string(data), "PROXY_API_KEY=sk-proxy") {
		t.Fatalf("provider key was not saved:\n%s", data)
	}
}

func TestConfigChangeLeaseHeldPersistsAndDefersRefresh(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "externally-leased-settings.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer externalLease.Release()

	oldExec := agent.New(nil, nil, agent.NewSession("old system prompt"), agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_settings",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_settings", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.SetMaxSubagentDepth(1); err != nil {
		t.Fatalf("SetMaxSubagentDepth should defer lease-held refresh instead of failing: %v", err)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("tab controller changed after deferred settings rebuild")
	}
	got := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if got.Agent.MaxSubagentDepth != 1 {
		t.Fatalf("saved max_subagent_depth = %d, want 1", got.Agent.MaxSubagentDepth)
	}
}

func TestDeferredRebuildRetryAppliesAfterLeaseRelease(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	prevInterval := deferredRebuildRetryInterval
	deferredRebuildRetryInterval = 20 * time.Millisecond
	t.Cleanup(func() { deferredRebuildRetryInterval = prevInterval })

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "deferred-rebuild-retry.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	released := false
	defer func() {
		if !released {
			externalLease.Release()
		}
	}()

	oldExec := agent.New(nil, nil, agent.NewSession("old system prompt"), agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.enableDeferredRebuildRetry()
	t.Cleanup(app.stopDeferredRebuildRetry)
	tab := &WorkspaceTab{
		ID:          "tab_deferred_retry",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_deferred_retry", app: app},
		disabledMCP: map[string]ServerView{},
	}
	installNoopRuntimeEvents(app, tab.sink)
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if c := app.controllerForTab(tab); c != nil && c != oldCtrl {
			c.Close()
		}
		tab.releaseSessionLease()
	})

	if err := app.SetMaxSubagentDepth(1); err != nil {
		t.Fatalf("SetMaxSubagentDepth: %v", err)
	}
	if !app.deferredRebuildPending(tab.ID) {
		t.Fatal("deferred rebuild was not scheduled while the lease is held")
	}
	if app.controllerForTab(tab) != oldCtrl {
		t.Fatal("controller changed while the lease is still held")
	}

	externalLease.Release()
	released = true

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !app.deferredRebuildPending(tab.ID) && app.controllerForTab(tab) != oldCtrl {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if app.deferredRebuildPending(tab.ID) {
		t.Fatal("deferred rebuild is still pending after the lease was released")
	}
	if c := app.controllerForTab(tab); c == nil || c == oldCtrl {
		t.Fatalf("controller was not rebuilt after the lease release: got %p", c)
	}
}

func TestDeferredRebuildScheduleAfterStopIsNoop(t *testing.T) {
	app := NewApp()
	app.stopDeferredRebuildRetry()
	app.scheduleDeferredRebuild("tab_x", "settings")
	if app.deferredRebuildPending("tab_x") {
		t.Fatal("schedule after stop should not register pending work")
	}
}

func TestDeferredRebuildWaitsForTabToBecomeActive(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	prevInterval := deferredRebuildRetryInterval
	deferredRebuildRetryInterval = 20 * time.Millisecond
	t.Cleanup(func() { deferredRebuildRetryInterval = prevInterval })

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "deferred-rebuild-inactive.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	released := false
	defer func() {
		if !released {
			externalLease.Release()
		}
	}()

	oldExec := agent.New(nil, nil, agent.NewSession("old system prompt"), agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	otherCtrl := control.New(control.Options{Label: "other"})
	defer otherCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.enableDeferredRebuildRetry()
	t.Cleanup(app.stopDeferredRebuildRetry)
	tab := &WorkspaceTab{
		ID:          "tab_pending",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_pending", app: app},
		disabledMCP: map[string]ServerView{},
	}
	installNoopRuntimeEvents(app, tab.sink)
	other := &WorkspaceTab{
		ID:          "tab_other",
		Scope:       "global",
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        otherCtrl,
		sink:        &tabEventSink{tabID: "tab_other", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab, other.ID: other}
	app.tabOrder = []string{tab.ID, other.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if c := app.controllerForTab(tab); c != nil && c != oldCtrl {
			c.Close()
		}
		tab.releaseSessionLease()
	})

	if err := app.SetMaxSubagentDepth(1); err != nil {
		t.Fatalf("SetMaxSubagentDepth: %v", err)
	}
	if !app.deferredRebuildPending(tab.ID) {
		t.Fatal("deferred rebuild was not scheduled while the lease is held")
	}

	// Focus another tab, then release the lease: the retry must not rebuild
	// while the pending tab is inactive (rebuildSettingLocked acts on the
	// active tab), and must not touch the focused tab's runtime either.
	app.mu.Lock()
	app.activeTabID = other.ID
	app.mu.Unlock()
	externalLease.Release()
	released = true

	time.Sleep(150 * time.Millisecond)
	if !app.deferredRebuildPending(tab.ID) {
		t.Fatal("pending entry was consumed while its tab was inactive")
	}
	if app.controllerForTab(tab) != oldCtrl {
		t.Fatal("inactive pending tab was rebuilt")
	}
	if app.controllerForTab(other) != otherCtrl {
		t.Fatal("focused tab was rebuilt by another tab's deferred retry")
	}

	// Switch back: the retry should now refresh the pending tab.
	app.mu.Lock()
	app.activeTabID = tab.ID
	app.mu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !app.deferredRebuildPending(tab.ID) && app.controllerForTab(tab) != oldCtrl {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if app.deferredRebuildPending(tab.ID) {
		t.Fatal("deferred rebuild still pending after its tab became active again")
	}
	if c := app.controllerForTab(tab); c == nil || c == oldCtrl {
		t.Fatalf("controller was not rebuilt after tab reactivation: got %p", c)
	}
}

func TestSetEffortForTabLeaseHeldKeepsOldControllerAlive(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:             "old",
		Kind:             "openai",
		BaseURL:          "https://example.invalid/v1",
		Model:            "old-model",
		APIKeyEnv:        "OLD_MODEL_KEY",
		SupportedEfforts: []string{"low", "max"},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "externally-leased-effort-switch.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	released := false
	defer func() {
		if !released {
			externalLease.Release()
		}
	}()

	oldExec := agent.New(nil, nil, agent.NewSession("old system prompt"), agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_effort",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_effort", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if c := app.controllerForTab(tab); c != nil && c != oldCtrl {
			c.Close()
		}
		tab.releaseSessionLease()
	})

	err = app.SetEffortForTab(tab.ID, "max")
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("SetEffortForTab err = %v, want ErrSessionLeaseHeld", err)
	}
	if strings.Contains(err.Error(), sessionPath) || strings.Contains(err.Error(), "held by") {
		t.Fatalf("SetEffortForTab surfaced raw lease details: %v", err)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatal("tab controller changed after failed effort switch")
	}

	// The failed switch must leave the old runtime alive: after the other
	// window releases the lease, retrying from the same tab has to succeed.
	// (The old code closed the old controller before acquiring the lease, so
	// this retry died on a snapshot of a closed session.)
	externalLease.Release()
	released = true
	if err := app.SetEffortForTab(tab.ID, "max"); err != nil {
		t.Fatalf("SetEffortForTab retry after lease release: %v", err)
	}
	if tab.Ctrl == oldCtrl {
		t.Fatal("retry did not rebuild the controller")
	}
}

func TestSetEffortForTabReanchorsDepthCapRecoveryBranch(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:             "old",
		Kind:             "openai",
		BaseURL:          "https://example.invalid/v1",
		Model:            "old-model",
		APIKeyEnv:        "OLD_MODEL_KEY",
		SupportedEfforts: []string{"low", "max"},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	recoveryPath := filepath.Join(dir, "effort-switch-conflict-recovery-deadbeef.jsonl")
	disk := agent.NewSession("old system prompt")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := disk.Save(recoveryPath); err != nil {
		t.Fatalf("save recovery branch: %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(recoveryPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	meta.Recovered = true
	meta.ParentID = "effort-switch-conflict"
	meta.RecoveryReason = "snapshot conflict"
	meta.RecoveryDepth = agent.SessionRecoveryMaxDepth
	if err := agent.SaveBranchMeta(recoveryPath, meta); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	stale := agent.NewSession("old system prompt")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	oldExec := agent.New(nil, nil, stale, agent.Options{}, event.Discard)

	app := NewApp()
	app.ctx = context.Background()
	app.runtimeEvents.emit = func(context.Context, string, ...interface{}) {}
	tab := &WorkspaceTab{
		ID:          "tab_depth_cap_effort",
		Scope:       "global",
		SessionPath: recoveryPath,
		Ready:       true,
		model:       "old/old-model",
		disabledMCP: map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	oldCtrl := control.New(control.Options{
		Executor:            oldExec,
		SessionDir:          dir,
		SessionPath:         recoveryPath,
		Label:               "old",
		Sink:                tab.sink,
		SessionRecoveryMeta: app.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:  app.handleTabSessionRecovered(tab),
	})
	tab.Ctrl = oldCtrl
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	stale.IncrementRewrite()

	if err := app.SetEffortForTab(tab.ID, "max"); err != nil {
		t.Fatalf("SetEffortForTab: %v", err)
	}
	if got := tab.Ctrl.SessionPath(); got != recoveryPath {
		t.Fatalf("session path after effort switch = %q, want current recovery branch %q", got, recoveryPath)
	}
	if got := tab.currentSessionPath(); got != recoveryPath {
		t.Fatalf("tab current session path = %q, want %q", got, recoveryPath)
	}
	if tab.sessionLease == nil || sessionRuntimeKey(tab.sessionLease.Path()) != sessionRuntimeKey(recoveryPath) {
		t.Fatalf("tab lease path = %q, want %q", tab.sessionLeaseRuntimeKey(), recoveryPath)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches: %v", err)
	}
	matches = primarySessionFiles(matches)
	if len(matches) != 1 || matches[0] != recoveryPath {
		t.Fatalf("recovery branches after effort switch = %v, want only %q", matches, recoveryPath)
	}

	lines := readConflictLogLines(t, store.SessionConflictLog(recoveryPath))
	if len(lines) != 1 {
		t.Fatalf("conflict log lines = %v, want one depth-cap diagnostic", lines)
	}
	if !strings.Contains(lines[0], `"outcome":"recovery_depth_cap_force_saved"`) {
		t.Fatalf("conflict diagnostic = %s, want depth-cap outcome", lines[0])
	}
	if strings.Contains(lines[0], dir) || strings.Contains(lines[0], recoveryPath) {
		t.Fatalf("conflict diagnostic leaked local path: %s", lines[0])
	}

	if err := tab.Ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot after effort switch recovery: %v", err)
	}
	afterLines := readConflictLogLines(t, store.SessionConflictLog(recoveryPath))
	if len(afterLines) != len(lines) {
		t.Fatalf("follow-up snapshot appended conflict diagnostics: before=%v after=%v", lines, afterLines)
	}
	matches, err = filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches after snapshot: %v", err)
	}
	matches = primarySessionFiles(matches)
	if len(matches) != 1 || matches[0] != recoveryPath {
		t.Fatalf("recovery branches after follow-up snapshot = %v, want only %q", matches, recoveryPath)
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")

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
	setDesktopTestCredential(t, "LOCAL_API_KEY", "sk-test")
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
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")

	cfg := config.Default()
	preset, ok := config.CuratedProviderPreset("mimo-api")
	if !ok || len(preset.Entries) == 0 {
		t.Fatal("mimo-api preset missing")
	}
	if err := cfg.UpsertProvider(preset.Entries[0]); err != nil {
		t.Fatalf("upsert mimo-api preset: %v", err)
	}
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
	} {
		if !refs[want] {
			t.Fatalf("Models() refs = %+v, missing %s", models, want)
		}
	}
	if len(models) != 2 {
		t.Fatalf("Models() len = %d, want 2: %+v", len(models), models)
	}
}

func TestModelsForTabKeepsUserProvidersWithProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")

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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	setDesktopTestCredential(t, "MIMO_API_KEY", "sk-test")

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

func TestSetModelForTabRefreshesCarriedSystemPrompt(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if err := os.MkdirAll(config.MemoryUserDir(), 0o755); err != nil {
		t.Fatalf("mkdir memory dir: %v", err)
	}
	const freshRule = "Fresh global AGENTS rule for model switch"
	if err := os.WriteFile(filepath.Join(config.MemoryUserDir(), "AGENTS.md"), []byte(freshRule), 0o644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	oldSession := agent.NewSession("old system prompt without memory")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldPath := filepath.Join(dir, "old.jsonl")
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: oldPath, Label: "old", Sink: event.Discard})

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_a",
		Scope:       "global",
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	if err := app.SetModelForTab(tab.ID, "new/new-model"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	history := tab.Ctrl.History()
	if len(history) < 2 {
		t.Fatalf("history length = %d, want system + user", len(history))
	}
	if history[0].Role != provider.RoleSystem {
		t.Fatalf("first message role = %s, want system", history[0].Role)
	}
	if !strings.Contains(history[0].Content, freshRule) {
		t.Fatalf("refreshed system prompt missing global AGENTS rule:\n%s", history[0].Content)
	}
	if history[1].Role != provider.RoleUser || history[1].Content != "hello" {
		t.Fatalf("carried user message changed: %+v", history[1])
	}
}

func TestSetModelForTabContinuesRecoveryPathAfterSnapshotConflict(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	originalPath := filepath.Join(dir, "model-switch-conflict.jsonl")
	current := agent.NewSession("old system prompt")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(originalPath); err != nil {
		t.Fatalf("save current session: %v", err)
	}

	stale := agent.NewSession("old system prompt")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	oldExec := agent.New(nil, nil, stale, agent.Options{}, event.Discard)

	app := NewApp()
	app.ctx = context.Background()
	app.runtimeEvents.emit = func(context.Context, string, ...interface{}) {}
	tab := &WorkspaceTab{
		ID:          "tab_recovery_model",
		Scope:       "global",
		SessionPath: originalPath,
		Ready:       true,
		model:       "old/old-model",
		disabledMCP: map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	oldCtrl := control.New(control.Options{
		Executor:            oldExec,
		SessionDir:          dir,
		SessionPath:         originalPath,
		Label:               "old",
		Sink:                tab.sink,
		SessionRecoveryMeta: app.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:  app.handleTabSessionRecovered(tab),
	})
	tab.Ctrl = oldCtrl
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	if err := app.SetModelForTab(tab.ID, "new/new-model"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	recoveryPath := tab.Ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == originalPath || !strings.Contains(filepath.Base(recoveryPath), "-recovery-") {
		t.Fatalf("model switch session path = %q, want recovery path distinct from %q", recoveryPath, originalPath)
	}
	if got := tab.currentSessionPath(); got != recoveryPath {
		t.Fatalf("tab current session path = %q, want recovery path %q", got, recoveryPath)
	}
	if tab.sessionLease == nil || sessionRuntimeKey(tab.sessionLease.Path()) != sessionRuntimeKey(recoveryPath) {
		t.Fatalf("tab lease path = %q, want recovery path %q", tab.sessionLeaseRuntimeKey(), recoveryPath)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches: %v", err)
	}
	matches = primarySessionFiles(matches)
	if len(matches) != 1 || matches[0] != recoveryPath {
		t.Fatalf("recovery branches after model switch = %v, want only %q", matches, recoveryPath)
	}
	if err := tab.Ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot after model switch recovery: %v", err)
	}
	matches, err = filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches after snapshot: %v", err)
	}
	matches = primarySessionFiles(matches)
	if len(matches) != 1 || matches[0] != recoveryPath {
		t.Fatalf("recovery branches after follow-up snapshot = %v, want only %q", matches, recoveryPath)
	}
}

func TestSetModelForTabReusesCurrentSessionLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldPath := filepath.Join(dir, "leased-model-switch.jsonl")
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: oldPath, Label: "old", Sink: event.Discard})

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_a",
		Scope:       "global",
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	if err := tab.ensureSessionLease(oldPath); err != nil {
		t.Fatalf("ensureSessionLease: %v", err)
	}
	if err := app.SetModelForTab(tab.ID, "new/new-model"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	if tab.Ctrl == nil || tab.Ctrl == oldCtrl {
		t.Fatalf("tab controller was not rebuilt")
	}
	if got := tab.model; got != "new/new-model" {
		t.Fatalf("tab model = %q, want new/new-model", got)
	}
	if tab.sessionLease == nil || sessionRuntimeKey(tab.sessionLease.Path()) != sessionRuntimeKey(oldPath) {
		t.Fatalf("session lease path = %q, want %q", tab.currentSessionPath(), oldPath)
	}
	history := tab.Ctrl.History()
	if len(history) < 2 || history[1].Role != provider.RoleUser || history[1].Content != "hello" {
		t.Fatalf("carried history = %+v, want original user message", history)
	}
}

func TestSetModelForTabWaitsForConcurrentBlankSessionLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(dir, "blank-model-switch-race.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write blank session: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:            "tab_blank_race",
		Scope:         "global",
		WorkspaceRoot: globalTabWorkspaceRoot(),
		SessionPath:   path,
		Ready:         true,
		model:         "old/old-model",
		sink:          &tabEventSink{tabID: "tab_blank_race", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	acquired := make(chan struct{})
	releaseHook := make(chan struct{})
	var once sync.Once
	sessionLeaseAcquireHookForTest = func() {
		once.Do(func() {
			close(acquired)
			<-releaseHook
		})
	}
	t.Cleanup(func() { sessionLeaseAcquireHookForTest = nil })

	buildErr := make(chan error, 1)
	go func() {
		buildErr <- tab.ensureSessionLease(path)
	}()

	select {
	case <-acquired:
	case err := <-buildErr:
		t.Fatalf("background lease acquire returned before hook: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("background lease acquire did not start")
	}

	switchErr := make(chan error, 1)
	go func() {
		switchErr <- app.SetModelForTab(tab.ID, "new/new-model")
	}()

	select {
	case err := <-switchErr:
		t.Fatalf("SetModelForTab returned before concurrent lease was bound: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseHook)
	if err := <-buildErr; err != nil {
		t.Fatalf("background ensureSessionLease: %v", err)
	}
	if err := <-switchErr; err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	if tab.Ctrl == nil {
		t.Fatal("model switch did not build a controller")
	}
	if got := tab.model; got != "new/new-model" {
		t.Fatalf("tab model = %q, want new/new-model", got)
	}
	if tab.sessionLease == nil || sessionRuntimeKey(tab.sessionLease.Path()) != sessionRuntimeKey(path) {
		t.Fatalf("session lease path = %q, want %q", tab.currentSessionPath(), path)
	}
}

func TestSetModelForTabLeaseHeldKeepsCurrentController(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	oldPath := filepath.Join(dir, "externally-leased-model-switch.jsonl")
	if err := os.WriteFile(oldPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(oldPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer externalLease.Release()

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: oldPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_a",
		Scope:       "global",
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	err = app.SetModelForTab(tab.ID, "new/new-model")
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("SetModelForTab err = %v, want ErrSessionLeaseHeld", err)
	}
	if strings.Contains(err.Error(), oldPath) || strings.Contains(err.Error(), "held by") {
		t.Fatalf("SetModelForTab surfaced raw lease details: %v", err)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("tab controller changed after failed switch")
	}
	if got := tab.model; got != "old/old-model" {
		t.Fatalf("tab model = %q, want old/old-model", got)
	}
	info, err := os.Stat(oldPath)
	if err != nil {
		t.Fatalf("stat session: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("session file size = %d, want unchanged empty file", info.Size())
	}
}

func TestSetModelForTabReattachesDetachedRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "NEW_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old", "new"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
		{Name: "new", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "new-model", APIKeyEnv: "NEW_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := desktopSessionDir(globalTabWorkspaceRoot())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "detached-model-switch.jsonl")
	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello from detached"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: path, Label: "old", Sink: event.Discard})
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	key := sessionRuntimeKey(path)
	detached := &WorkspaceTab{
		ID:             detachedRuntimeTabID(key),
		Scope:          "global",
		SessionPath:    path,
		Ctrl:           oldCtrl,
		Ready:          true,
		model:          "old/old-model",
		sessionLease:   lease,
		disabledMCP:    map[string]ServerView{},
		SharedHostKey:  "detached-host",
		ActivityStatus: "",
	}
	tab := &WorkspaceTab{
		ID:          "tab_a",
		Scope:       "global",
		SessionPath: path,
		Ready:       true,
		model:       "old/old-model",
		sink:        &tabEventSink{tabID: "tab_a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.detachedSessions = map[string]*WorkspaceTab{key: detached}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
		if detached.sessionLease != nil {
			detached.releaseSessionLease()
		}
	})

	if err := app.SetModelForTab(tab.ID, "new/new-model"); err != nil {
		t.Fatalf("SetModelForTab: %v", err)
	}
	if _, ok := app.detachedSessions[key]; ok {
		t.Fatal("detached runtime was not consumed")
	}
	if tab.Ctrl == nil || tab.Ctrl == oldCtrl {
		t.Fatalf("tab controller was not rebuilt from detached runtime")
	}
	if got := tab.model; got != "new/new-model" {
		t.Fatalf("tab model = %q, want new/new-model", got)
	}
	if tab.sessionLease == nil || sessionRuntimeKey(tab.sessionLease.Path()) != key {
		t.Fatalf("session lease path = %q, want %q", tab.currentSessionPath(), path)
	}
	history := tab.Ctrl.History()
	if len(history) < 2 || history[1].Content != "hello from detached" {
		t.Fatalf("carried history = %+v, want detached user message", history)
	}
}

type staleWorkspaceBindingFixture struct {
	app          *App
	tab          *WorkspaceTab
	oldCtrl      control.SessionAPI
	projectA     string
	sessionDirA  string
	sessionPathA string
}

func newStaleWorkspaceBindingFixture(t *testing.T, suffix string) staleWorkspaceBindingFixture {
	return newStaleWorkspaceBindingFixtureWithLayout(t, suffix, "")
}

func newStaleWorkspaceBindingFixtureWithLayout(t *testing.T, suffix, layoutStyle string) staleWorkspaceBindingFixture {
	t.Helper()
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "TEST_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "test/test-model"
	cfg.Desktop.ProviderAccess = []string{"test"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "test", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "test-model", APIKeyEnv: "TEST_MODEL_KEY"},
	}
	if strings.TrimSpace(layoutStyle) != "" {
		if err := cfg.SetDesktopLayoutStyle(layoutStyle); err != nil {
			t.Fatalf("set desktop layout style: %v", err)
		}
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}

	topicID := "topic_" + suffix
	topicTitle := "Rebuild workspace " + suffix
	sessionDirA := desktopSessionDir(projectA)
	sessionDirB := desktopSessionDir(projectB)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	if err := os.MkdirAll(sessionDirB, 0o755); err != nil {
		t.Fatalf("mkdir project B sessions: %v", err)
	}
	sessionPathA := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", topicID, topicTitle, projectA, "project A prompt", time.Now())
	sessionPathB := filepath.Join(sessionDirB, "wrong.jsonl")

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "carry me"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{
		Executor:      oldExec,
		SessionDir:    sessionDirB,
		SessionPath:   sessionPathB,
		Label:         "test/test-model",
		ModelRef:      "test/test-model",
		WorkspaceRoot: projectB,
		Sink:          event.Discard,
	})

	app := NewApp()
	app.readyHook = func() {}
	tab := &WorkspaceTab{
		ID:            "tab_stale_workspace_" + suffix,
		Scope:         "project",
		WorkspaceRoot: projectB,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   sessionPathA,
		Ready:         true,
		model:         "test/test-model",
		Ctrl:          oldCtrl,
		sink:          &tabEventSink{tabID: "tab_stale_workspace_" + suffix, app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	return staleWorkspaceBindingFixture{
		app:          app,
		tab:          tab,
		oldCtrl:      oldCtrl,
		projectA:     projectA,
		sessionDirA:  sessionDirA,
		sessionPathA: sessionPathA,
	}
}

func assertTabRebuiltToPinnedWorkspace(t *testing.T, f staleWorkspaceBindingFixture) {
	t.Helper()
	if f.tab.Ctrl == nil {
		t.Fatal("controller was not rebuilt")
	}
	if f.tab.Ctrl == f.oldCtrl {
		t.Fatal("stale controller was reused")
	}
	if got := normalizeProjectRoot(f.tab.WorkspaceRoot); got != normalizeProjectRoot(f.projectA) {
		t.Fatalf("tab workspace root = %q, want project A %q", got, normalizeProjectRoot(f.projectA))
	}
	if got := normalizeProjectRoot(f.tab.Ctrl.WorkspaceRoot()); got != normalizeProjectRoot(f.projectA) {
		t.Fatalf("controller workspace root = %q, want project A %q", got, normalizeProjectRoot(f.projectA))
	}
	if !sameDesktopPath(f.tab.Ctrl.SessionDir(), f.sessionDirA) {
		t.Fatalf("controller session dir = %q, want %q", f.tab.Ctrl.SessionDir(), f.sessionDirA)
	}
	if !sameDesktopPath(f.tab.Ctrl.SessionPath(), f.sessionPathA) {
		t.Fatalf("controller session path = %q, want %q", f.tab.Ctrl.SessionPath(), f.sessionPathA)
	}
}

type blockingSnapshotCtrl struct {
	control.SessionAPI

	firstSnapshotStarted  chan struct{}
	secondSnapshotStarted chan struct{}
	releaseSnapshot       chan struct{}
	firstOnce             sync.Once
	secondOnce            sync.Once
	snapshotCount         atomic.Int32
	closeCount            atomic.Int32
}

func newBlockingSnapshotCtrl(ctrl control.SessionAPI) *blockingSnapshotCtrl {
	return &blockingSnapshotCtrl{
		SessionAPI:            ctrl,
		firstSnapshotStarted:  make(chan struct{}),
		secondSnapshotStarted: make(chan struct{}),
		releaseSnapshot:       make(chan struct{}),
	}
}

func (c *blockingSnapshotCtrl) Snapshot() error {
	count := c.snapshotCount.Add(1)
	switch count {
	case 1:
		c.firstOnce.Do(func() { close(c.firstSnapshotStarted) })
	case 2:
		c.secondOnce.Do(func() { close(c.secondSnapshotStarted) })
	}
	<-c.releaseSnapshot
	if c.SessionAPI == nil {
		return nil
	}
	return c.SessionAPI.Snapshot()
}

func (c *blockingSnapshotCtrl) Close() {
	c.closeCount.Add(1)
	if c.SessionAPI != nil {
		c.SessionAPI.Close()
	}
}

func (f *staleWorkspaceBindingFixture) installBlockingSnapshotController() *blockingSnapshotCtrl {
	ctrl := newBlockingSnapshotCtrl(f.tab.Ctrl)
	f.tab.Ctrl = ctrl
	f.oldCtrl = ctrl
	return ctrl
}

func TestEnsureTabControllerWorkspaceRebuildsStaleWorkspace(t *testing.T) {
	f := newStaleWorkspaceBindingFixture(t, "rebuild_workspace")

	if err := f.app.ensureTabControllerWorkspace(f.tab); err != nil {
		t.Fatalf("ensureTabControllerWorkspace: %v", err)
	}
	assertTabRebuiltToPinnedWorkspace(t, f)
}

func TestEnsureTabControllerWorkspaceWarnsWhenPinnedSessionSwitchesWorkspace(t *testing.T) {
	f := newStaleWorkspaceBindingFixture(t, "warn_workspace_switch")
	events := make(chan event.Event, 8)
	f.tab.sink.SetBotSink(event.FuncSink(func(e event.Event) {
		events <- e
	}))

	if err := f.app.ensureTabControllerWorkspace(f.tab); err != nil {
		t.Fatalf("ensureTabControllerWorkspace: %v", err)
	}
	assertTabRebuiltToPinnedWorkspace(t, f)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == event.Notice &&
				e.Level == event.LevelWarn &&
				strings.Contains(e.Text, f.projectA) &&
				strings.Contains(e.Text, "switched tab") {
				return
			}
		case <-deadline:
			t.Fatal("did not receive workspace switch warning notice")
		}
	}
}

func TestSteerForTabReconcilesStaleWorkspaceBeforeIdleFallback(t *testing.T) {
	f := newStaleWorkspaceBindingFixture(t, "steer_idle_fallback")

	if err := f.app.SteerForTab(f.tab.ID, "/unknown-command"); err != nil {
		t.Fatalf("SteerForTab: %v", err)
	}
	waitNotRunning(t, f.tab.Ctrl)
	assertTabRebuiltToPinnedWorkspace(t, f)
}

func TestCompactReconcilesStaleWorkspaceBeforeCompaction(t *testing.T) {
	f := newStaleWorkspaceBindingFixture(t, "compact")

	if err := f.app.Compact(); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	assertTabRebuiltToPinnedWorkspace(t, f)
}

func TestEffortCommandUsesPinnedSessionOwnerBeforeStaleWorkspaceRoot(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OWNER_MODEL_KEY", "sk-test")
	setDesktopTestCredential(t, "STALE_MODEL_KEY", "sk-test")

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}
	ownerConfig := `default_model = "owner/owner-model"
[[providers]]
name = "owner"
kind = "openai"
base_url = "https://owner.example.invalid/v1"
model = "owner-model"
api_key_env = "OWNER_MODEL_KEY"
supported_efforts = ["max"]
default_effort = "max"
`
	if err := os.WriteFile(filepath.Join(projectA, "reasonix.toml"), []byte(ownerConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	staleConfig := `default_model = "stale/stale-model"
[[providers]]
name = "stale"
kind = "openai"
base_url = "https://stale.example.invalid/v1"
model = "stale-model"
api_key_env = "STALE_MODEL_KEY"
reasoning_protocol = "none"
`
	if err := os.WriteFile(filepath.Join(projectB, "reasonix.toml"), []byte(staleConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	topicID := "topic_effort_owner"
	topicTitle := "Effort owner"
	sessionDirA := desktopSessionDir(projectA)
	sessionDirB := desktopSessionDir(projectB)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	if err := os.MkdirAll(sessionDirB, 0o755); err != nil {
		t.Fatalf("mkdir project B sessions: %v", err)
	}
	sessionPathA := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", topicID, topicTitle, projectA, "project A prompt", time.Now())
	oldCtrl := control.New(control.Options{
		SessionDir:    sessionDirB,
		SessionPath:   filepath.Join(sessionDirB, "wrong.jsonl"),
		WorkspaceRoot: projectB,
		Sink:          event.Discard,
	})

	app := NewApp()
	app.readyHook = func() {}
	tab := &WorkspaceTab{
		ID:            "tab_stale_effort",
		Scope:         "project",
		WorkspaceRoot: projectB,
		TopicID:       topicID,
		TopicTitle:    topicTitle,
		SessionPath:   sessionPathA,
		Ready:         true,
		Ctrl:          oldCtrl,
		sink:          &tabEventSink{tabID: "tab_stale_effort", app: app},
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
	})

	if err := app.SubmitToTab(tab.ID, "/effort max"); err != nil {
		t.Fatalf("SubmitToTab(/effort max): %v", err)
	}
	waitNotRunning(t, tab.Ctrl)
	if tab.effort == nil || *tab.effort != "max" {
		t.Fatalf("tab effort = %#v, want max from pinned project A provider", tab.effort)
	}
	if got := normalizeProjectRoot(tab.WorkspaceRoot); got != normalizeProjectRoot(projectA) {
		t.Fatalf("tab workspace root = %q, want project A %q", got, normalizeProjectRoot(projectA))
	}
	if got := normalizeProjectRoot(tab.Ctrl.WorkspaceRoot()); got != normalizeProjectRoot(projectA) {
		t.Fatalf("controller workspace root = %q, want project A %q", got, normalizeProjectRoot(projectA))
	}
}

func TestClassicLayoutQuickClicksSerializeWorkspaceRebuild(t *testing.T) {
	runQuickClickWorkspaceReconcileTest(t, "classic")
}

func TestWorkbenchLayoutQuickClicksSerializeWorkspaceRebuild(t *testing.T) {
	runQuickClickWorkspaceReconcileTest(t, "workbench")
}

func TestCreationLayoutQuickClicksSerializeWorkspaceRebuild(t *testing.T) {
	runQuickClickWorkspaceReconcileTest(t, "creation")
}

func runQuickClickWorkspaceReconcileTest(t *testing.T, layoutStyle string) {
	t.Helper()
	f := newStaleWorkspaceBindingFixtureWithLayout(t, "quick_click_"+layoutStyle, layoutStyle)
	if got, want := f.app.singleSurfaceLayoutEnabled(), singleSurfaceLayoutStyle(layoutStyle); got != want {
		t.Fatalf("singleSurfaceLayoutEnabled(%q) = %v, want %v", layoutStyle, got, want)
	}
	blockingCtrl := f.installBlockingSnapshotController()

	type quickAction struct {
		name string
		run  func() error
	}
	actions := []quickAction{
		{name: "submit", run: func() error { return f.app.SubmitToTab(f.tab.ID, "/unknown-command") }},
		{name: "steer", run: func() error { return f.app.SteerForTab(f.tab.ID, "/unknown-command") }},
		{name: "compact", run: func() error { return f.app.Compact() }},
		{name: "submit-display", run: func() error { return f.app.SubmitDisplayToTab(f.tab.ID, "/unknown display", "/unknown-command") }},
	}

	start := make(chan struct{})
	ready := make(chan struct{}, len(actions))
	errs := make(chan error, len(actions))
	var wg sync.WaitGroup
	for _, action := range actions {
		action := action
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			if err := action.run(); err != nil {
				errs <- fmt.Errorf("%s: %w", action.name, err)
			}
		}()
	}
	for range actions {
		<-ready
	}
	close(start)

	select {
	case <-blockingCtrl.firstSnapshotStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first stale controller snapshot")
	}
	select {
	case <-blockingCtrl.secondSnapshotStarted:
		t.Fatal("workspace rebuild was not serialized: second stale snapshot started before the first rebuild finished")
	case <-time.After(75 * time.Millisecond):
	}
	close(blockingCtrl.releaseSnapshot)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if got := blockingCtrl.snapshotCount.Load(); got != 1 {
		t.Fatalf("stale snapshot count = %d, want 1", got)
	}
	if got := blockingCtrl.closeCount.Load(); got != 1 {
		t.Fatalf("stale close count = %d, want 1", got)
	}
	waitNotRunning(t, f.tab.Ctrl)
	assertTabRebuiltToPinnedWorkspace(t, f)
}

func TestListSessionsUsesPinnedSessionOwnerBeforeStaleRuntimeDir(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatalf("add project A: %v", err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatalf("add project B: %v", err)
	}
	sessionDirA := desktopSessionDir(projectA)
	sessionDirB := desktopSessionDir(projectB)
	if err := os.MkdirAll(sessionDirA, 0o755); err != nil {
		t.Fatalf("mkdir project A sessions: %v", err)
	}
	if err := os.MkdirAll(sessionDirB, 0o755); err != nil {
		t.Fatalf("mkdir project B sessions: %v", err)
	}
	sessionPathA := writeTopicSessionWithPrompt(t, sessionDirA, "project-a.jsonl", "topic_project_a", "Project A topic", projectA, "project A prompt", time.Now())
	sessionPathB := writeTopicSessionWithPrompt(t, sessionDirB, "project-b.jsonl", "topic_project_b", "Project B topic", projectB, "project B prompt", time.Now().Add(time.Minute))

	app := NewApp()
	oldCtrl := control.New(control.Options{
		SessionDir:    sessionDirB,
		SessionPath:   sessionPathB,
		WorkspaceRoot: projectB,
		Sink:          event.Discard,
	})
	tab := &WorkspaceTab{
		ID:            "tab_stale_runtime_dir",
		Scope:         "project",
		WorkspaceRoot: projectB,
		TopicID:       "topic_project_a",
		TopicTitle:    "Project A topic",
		SessionPath:   sessionPathA,
		Ready:         true,
		Ctrl:          oldCtrl,
		disabledMCP:   map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(oldCtrl.Close)

	sessions := app.ListSessions()
	if len(sessions) == 0 {
		t.Fatal("ListSessions() returned no sessions")
	}
	if filepath.Clean(sessions[0].Path) != filepath.Clean(sessionPathA) {
		t.Fatalf("ListSessions()[0].Path = %q, want pinned project A session %q", sessions[0].Path, sessionPathA)
	}
	for _, item := range sessions {
		if filepath.Clean(item.Path) == filepath.Clean(sessionPathB) {
			t.Fatalf("ListSessions() included stale project B runtime session: %+v", sessions)
		}
	}
	if got := normalizeProjectRoot(tab.WorkspaceRoot); got != normalizeProjectRoot(projectA) {
		t.Fatalf("tab workspace root = %q, want project A %q", got, normalizeProjectRoot(projectA))
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
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

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

// assertTabBuildSuperseded checks that the startup build registered before the
// mutation (generation) can no longer install its controller and that its
// build context was cancelled.
func assertTabBuildSuperseded(t *testing.T, app *App, tab *WorkspaceTab, generation uint64, buildCtx context.Context) {
	t.Helper()
	app.mu.Lock()
	superseded := app.tabBuildSupersededLocked(tab, generation)
	app.mu.Unlock()
	if !superseded {
		t.Fatal("in-flight startup build was not superseded; finishing it would reinstall a stale controller")
	}
	select {
	case <-buildCtx.Done():
	default:
		t.Fatal("in-flight startup build context was not cancelled")
	}
	if tab.buildCancel != nil {
		t.Fatal("build cancel was not cleared")
	}
}

func TestDeleteProviderSupersedesInFlightStartupBuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "prov-b/model-b1"
	cfg.Providers = []config.ProviderEntry{
		{Name: "prov-a", Kind: "openai", BaseURL: "https://a.example.com", Model: "model-a1", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	cfg.Desktop.ProviderAccess = []string{"prov-a", "prov-b"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	// Model the async startup build still being in flight for the affected
	// tab: no controller yet, a live generation, a cancellable build context.
	buildCtx, buildCancel := context.WithCancel(context.Background())
	tab := &WorkspaceTab{
		ID:              "tab_a",
		Scope:           "global",
		model:           "prov-a/model-a1",
		buildGeneration: 1,
		buildCancel:     buildCancel,
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.DeleteProvider("prov-a"); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	assertTabBuildSuperseded(t, app, tab, 1, buildCtx)
	if tab.model != "prov-b/model-b1" {
		t.Fatalf("tab model after delete = %q, want prov-b/model-b1", tab.model)
	}
}

func TestRemoveBuiltInProviderAccessSupersedesInFlightStartupBuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "prov-b/model-b1"
	cfg.Providers = []config.ProviderEntry{
		{Name: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "prov-b", Kind: "openai", BaseURL: "https://b.example.com", Model: "model-b1", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	cfg.Desktop.ProviderAccess = []string{"deepseek", "prov-b"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	buildCtx, buildCancel := context.WithCancel(context.Background())
	tab := &WorkspaceTab{
		ID:              "tab_ds",
		Scope:           "global",
		model:           "deepseek/deepseek-chat",
		buildGeneration: 1,
		buildCancel:     buildCancel,
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	if err := app.RemoveProviderAccess("deepseek"); err != nil {
		t.Fatalf("RemoveProviderAccess: %v", err)
	}
	assertTabBuildSuperseded(t, app, tab, 1, buildCtx)
	if tab.model != "prov-b/model-b1" {
		t.Fatalf("tab model after access removal = %q, want prov-b/model-b1", tab.model)
	}
}

func TestClearActiveSessionRuntimeSupersedesInFlightStartupBuild(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "clear-runtime-in-flight.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}

	oldSession := agent.NewSession("old system prompt")
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})

	app := NewApp()
	// A runtime is attached while an older async build is still in flight
	// (e.g. attached via topic activation); destroying the session must
	// invalidate that build so it cannot resurrect the destroyed session.
	buildCtx, buildCancel := context.WithCancel(context.Background())
	tab := &WorkspaceTab{
		ID:              "tab_clear",
		Scope:           "global",
		SessionPath:     sessionPath,
		model:           "old/old-model",
		Ready:           true,
		Ctrl:            oldCtrl,
		buildGeneration: 1,
		buildCancel:     buildCancel,
		disabledMCP:     map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(tab.releaseSessionLease)

	if err := app.clearActiveSessionRuntime(tab, oldCtrl); err != nil {
		t.Fatalf("clearActiveSessionRuntime: %v", err)
	}
	if tab.Ctrl == nil || tab.Ctrl == oldCtrl {
		t.Fatalf("clear did not install a fresh controller (ctrl=%v)", tab.Ctrl)
	}
	defer tab.Ctrl.Close()
	assertTabBuildSuperseded(t, app, tab, 1, buildCtx)
}

func TestClearActiveSessionRuntimeReleasesResourcesWhenTabReplaced(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{{
		Name:      "old",
		Kind:      "openai",
		BaseURL:   "https://example.invalid/v1",
		Model:     "old-model",
		APIKeyEnv: "OLD_MODEL_KEY",
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "clear-runtime-replaced-tab.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}

	oldSession := agent.NewSession("old system prompt")
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})

	app := NewApp()
	tab := &WorkspaceTab{
		ID:          "tab_replaced",
		Scope:       "global",
		SessionPath: sessionPath,
		model:       "old/old-model",
		Ready:       true,
		Ctrl:        oldCtrl,
		disabledMCP: map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	// The tab entry now points at a replacement struct (the tab was closed and
	// reopened while the clear ran off-lock), so the swap must not apply.
	replacement := &WorkspaceTab{ID: tab.ID, Scope: "global"}
	app.tabs = map[string]*WorkspaceTab{tab.ID: replacement}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(tab.releaseSessionLease)

	err := app.clearActiveSessionRuntime(tab, oldCtrl)
	if err == nil || !strings.Contains(err.Error(), "changed while clearing") {
		t.Fatalf("clearActiveSessionRuntime error = %v, want tab-changed error", err)
	}
	if replacement.Ctrl != nil {
		t.Fatalf("replacement tab controller = %v, want untouched nil", replacement.Ctrl)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("replaced tab controller = %v, want left on the destroyed runtime", tab.Ctrl)
	}
	if key := tab.sessionLeaseRuntimeKey(); key != "" {
		t.Fatalf("replaced tab still holds a session lease for %q; the fresh lease leaked", key)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("old session artifacts were not destroyed (stat err=%v)", err)
	}
}

func TestDeleteProviderRejectsRunningAffectedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

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
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

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
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

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

func TestConnectKeyRebuildLeaseHeldKeepsCurrentController(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv(onboardingKeyEnv, "")
	os.Unsetenv(onboardingKeyEnv)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	oldFetch := connectKeyBalanceFetch
	connectKeyBalanceFetch = func(context.Context, *http.Client, string, string) (*billing.Balance, error) {
		return &billing.Balance{Available: true}, nil
	}
	t.Cleanup(func() { connectKeyBalanceFetch = oldFetch })

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	sessionPath := filepath.Join(dir, "externally-leased-connect-key.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(sessionPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer externalLease.Release()

	oldSession := agent.NewSession("old system prompt")
	oldSession.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	oldExec := agent.New(nil, nil, oldSession, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: oldExec, SessionDir: dir, SessionPath: sessionPath, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_connect",
		Scope:       "global",
		SessionPath: sessionPath,
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_connect", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	warning, err := app.ConnectKey("sk-test")
	if err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
	if !strings.Contains(warning, "another Reasonix window") {
		t.Fatalf("ConnectKey warning = %q, want user-facing lease warning", warning)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("tab controller changed after failed connect-key rebuild")
	}
	if tab.StartupErr != "" {
		t.Fatalf("tab startup error = %q, want unchanged current session", tab.StartupErr)
	}
	if !config.CredentialStored(onboardingKeyEnv) {
		t.Fatal("onboarding key should be persisted even when hot rebuild is deferred")
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
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")

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

func TestSetTokenModeReusesCurrentSessionLease(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	session := agent.NewSession("old system prompt")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "leased-token-mode-switch.jsonl")
	oldCtrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "old", Sink: event.Discard})

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_a",
		Scope:       "global",
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	t.Cleanup(func() {
		if tab.Ctrl != nil {
			tab.Ctrl.Close()
		}
		tab.releaseSessionLease()
	})

	if err := tab.ensureSessionLease(path); err != nil {
		t.Fatalf("ensureSessionLease: %v", err)
	}
	if err := app.SetTokenModeForTab(tab.ID, "economy"); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	if tab.Ctrl == nil || tab.Ctrl == oldCtrl {
		t.Fatalf("tab controller was not rebuilt")
	}
	if got := currentTabTokenMode(tab); got != "economy" {
		t.Fatalf("token mode = %q, want economy", got)
	}
	if tab.sessionLease == nil || sessionRuntimeKey(tab.sessionLease.Path()) != sessionRuntimeKey(path) {
		t.Fatalf("session lease path = %q, want %q", tab.currentSessionPath(), path)
	}
	history := tab.Ctrl.History()
	if len(history) < 2 || history[1].Role != provider.RoleUser || history[1].Content != "hello" {
		t.Fatalf("carried history = %+v, want original user message", history)
	}
}

func TestSetTokenModeLeaseHeldKeepsCurrentController(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "OLD_MODEL_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "old/old-model"
	cfg.Desktop.ProviderAccess = []string{"old"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "old", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "old-model", APIKeyEnv: "OLD_MODEL_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "externally-leased-token-mode-switch.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write placeholder session: %v", err)
	}
	externalLease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	defer externalLease.Release()

	session := agent.NewSession("old system prompt")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "old", Sink: event.Discard})
	defer oldCtrl.Close()

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{
		ID:          "tab_a",
		Scope:       "global",
		Ready:       true,
		model:       "old/old-model",
		Ctrl:        oldCtrl,
		sink:        &tabEventSink{tabID: "tab_a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	err = app.SetTokenModeForTab(tab.ID, "economy")
	if !errors.Is(err, agent.ErrSessionLeaseHeld) {
		t.Fatalf("SetTokenModeForTab err = %v, want ErrSessionLeaseHeld", err)
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "held by") {
		t.Fatalf("SetTokenModeForTab surfaced raw lease details: %v", err)
	}
	if tab.Ctrl != oldCtrl {
		t.Fatalf("tab controller changed after failed switch")
	}
	if got := currentTabTokenMode(tab); got != "full" {
		t.Fatalf("token mode = %q, want full", got)
	}
}

func TestSetTokenModeMigratesStaleOfficialDeepSeekTabModel(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")

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

func TestMetaForTabReportsImageInputCapability(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "CUSTOM_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "custom/text-only"
	cfg.Desktop.ProviderAccess = []string{"custom"}
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		APIKeyEnv:    "CUSTOM_KEY",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.setTestCtrl(control.New(control.Options{Label: "custom/text-only"}), "custom/text-only")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if got := app.Meta().ImageInputEnabled; got {
		t.Fatal("text-only meta should disable image input")
	}
	if err := app.SetModel("custom/vision-pro"); err != nil {
		t.Fatalf("SetModel(custom/vision-pro): %v", err)
	}
	if got := app.Meta().ImageInputEnabled; !got {
		t.Fatal("vision model meta should enable image input")
	}
}

func TestMetaForTabImageInputCapabilityUsesCurrentRef(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "CUSTOM_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "custom/vision-pro"
	cfg.Desktop.ProviderAccess = []string{"custom"}
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://example.invalid/v1",
		APIKeyEnv:    "CUSTOM_KEY",
		Models:       []string{"text-only", "vision-pro"},
		VisionModels: []string{"vision-pro"},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.setTestCtrl(control.New(control.Options{Label: "deleted/model"}), "deleted/model")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if got := app.Meta().ImageInputEnabled; got {
		t.Fatal("unknown model ref should not inherit image input from the default fallback model")
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

func TestFileRefsForTabIgnoreActiveParentWorkspace(t *testing.T) {
	parentRoot := robustTempDir(t)
	childRoot := filepath.Join(parentRoot, "child")
	if err := os.MkdirAll(childRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentRoot, "parent-only.txt"), []byte("parent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentRoot, "shared.txt"), []byte("parent shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childRoot, "child-only.txt"), []byte("child"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childRoot, "shared.txt"), []byte("child shared"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"parent": {ID: "parent", Scope: "project", WorkspaceRoot: parentRoot},
			"child":  {ID: "child", Scope: "project", WorkspaceRoot: childRoot},
		},
		activeTabID: "parent",
	}

	listed := app.ListDirForTab("child", "")
	if !hasDirEntry(listed, "child-only.txt") || hasDirEntry(listed, "parent-only.txt") {
		t.Fatalf("ListDirForTab(child) = %+v, want only child workspace entries", listed)
	}
	found := app.SearchFileRefsForTab("child", "child-only")
	if !hasDirEntry(found, "child-only.txt") {
		t.Fatalf("SearchFileRefsForTab(child) = %+v, want child-only.txt", found)
	}
	preview := app.ReadFileForTab("child", "shared.txt")
	if preview.Err != "" || preview.Body != "child shared" {
		t.Fatalf("ReadFileForTab(child) = %+v, want child workspace file", preview)
	}
	path, ok, err := app.workspaceOrExternalPathForTab("child", "shared.txt")
	if err != nil || !ok || path != filepath.Join(childRoot, "shared.txt") {
		t.Fatalf("workspaceOrExternalPathForTab(child) = (%q, %v, %v)", path, ok, err)
	}

	legacy := app.ReadFile("shared.txt")
	if legacy.Err != "" || legacy.Body != "parent shared" {
		t.Fatalf("ReadFile legacy active-tab behavior = %+v, want parent workspace file", legacy)
	}
}

func TestFileRefsIncludeRegisteredExternalFolderChildren(t *testing.T) {
	workspace := robustTempDir(t)
	external := filepath.Join(robustTempDir(t), "Folder With Spaces")
	if err := os.MkdirAll(filepath.Join(external, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "src", "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedExternal := external
	if resolved, err := filepath.EvalSymlinks(external); err == nil {
		expectedExternal = resolved
	}
	expectedDisplayPath := filepath.ToSlash(expectedExternal)

	ctrl := &control.Controller{}
	token, _, err := ctrl.RegisterExternalFolderRef(external)
	if err != nil {
		t.Fatalf("RegisterExternalFolderRef: %v", err)
	}
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"project": {ID: "project", WorkspaceRoot: workspace, Ctrl: ctrl},
			"other":   {ID: "other", WorkspaceRoot: robustTempDir(t)},
		},
		activeTabID: "other",
	}

	listed := app.ListDirForTab("project", token+"/src/")
	if len(listed) != 1 ||
		listed[0].Name != "outside.txt" ||
		listed[0].Path != token+"/src/outside.txt" ||
		listed[0].DisplayPath != expectedDisplayPath+"/src/outside.txt" {
		t.Fatalf("ListDir external src = %+v, want outside token/display path", listed)
	}

	found := app.SearchFileRefsForTab("project", "outside")
	var externalHit *DirEntry
	for i := range found {
		if found[i].Path == token+"/src/outside.txt" {
			externalHit = &found[i]
			break
		}
	}
	if externalHit == nil || externalHit.DisplayName != "Folder With Spaces/src/outside.txt" || externalHit.DisplayPath != expectedDisplayPath+"/src/outside.txt" {
		t.Fatalf("SearchFileRefs external hit = %+v, all results %+v", externalHit, found)
	}

	preview := app.ReadFileForTab("project", token+"/src/outside.txt")
	if preview.Err != "" || preview.Body != "outside" {
		t.Fatalf("ReadFile external token preview = %+v, want outside file body", preview)
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

func TestDeleteSessionCancelsPreReadyBlankBuild(t *testing.T) {
	isolateDesktopUserDirs(t)

	globalRoot := globalTabWorkspaceRoot()
	dir := desktopSessionDir(globalRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "pre-ready-blank.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write blank session: %v", err)
	}
	cancelled := false
	blank := &WorkspaceTab{
		ID:            "blank",
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		SessionPath:   path,
		buildCancel:   func() { cancelled = true },
		disabledMCP:   map[string]ServerView{},
	}
	keep := &WorkspaceTab{
		ID:            "keep",
		Scope:         "global",
		WorkspaceRoot: globalRoot,
		Ready:         true,
		disabledMCP:   map[string]ServerView{},
	}
	app := &App{
		tabs:        map[string]*WorkspaceTab{"blank": blank, "keep": keep},
		tabOrder:    []string{"blank", "keep"},
		activeTabID: "blank",
	}

	if err := app.DeleteSession(filepath.Base(path)); err != nil {
		t.Fatalf("DeleteSession(pre-ready blank): %v", err)
	}
	if !cancelled {
		t.Fatal("pre-ready blank build was not cancelled")
	}
	if !blank.removed {
		t.Fatal("pre-ready blank tab was not marked removed")
	}
	if _, ok := app.tabs["blank"]; ok {
		t.Fatal("pre-ready blank tab should be removed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("blank session should be moved out of active history, stat err = %v", err)
	}
}

func TestDeleteLastTopicSessionFallbackDoesNotReuseDeletedTopic(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_delete_last"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Delete last"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := writeTopicSession(t, dir, "delete-last.jsonl", topicID, "Delete last", projectRoot)
	ctrl := controllerWithContent(t, path)
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"only": {
				ID:            "only",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Delete last",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"only"},
		activeTabID: "only",
	}

	if err := app.DeleteSession(path); err != nil {
		t.Fatalf("DeleteSession(last topic session): %v", err)
	}

	if _, ok := app.tabs["only"]; ok {
		t.Fatalf("deleted topic session tab should be removed")
	}
	for id, tab := range app.tabs {
		if tab.TopicID == topicID {
			t.Fatalf("fallback tab %q reused deleted topic %q", id, topicID)
		}
		if strings.TrimSpace(tab.TopicID) != "" {
			t.Fatalf("fallback tab %q topic ID = %q, want transient unindexed blank", id, tab.TopicID)
		}
	}
	trashPath := filepath.Join(dir, sessionTrashDir, "delete-last.jsonl", "delete-last.jsonl")
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("deleted session should be moved to trash: %v", err)
	}
}

func TestDeleteSessionFallbackKeepsTopicWithRemainingHistory(t *testing.T) {
	isolateDesktopUserDirs(t)

	projectRoot := t.TempDir()
	topicID := "topic_delete_keep_history"
	if err := addProject(projectRoot, ""); err != nil {
		t.Fatalf("add project: %v", err)
	}
	if err := setTopicTitle(projectRoot, topicID, "Keep history"); err != nil {
		t.Fatalf("set topic title: %v", err)
	}
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := writeTopicSession(t, dir, "delete-one.jsonl", topicID, "Keep history", projectRoot)
	remainingPath := writeTopicSessionWithPrompt(t, dir, "remaining.jsonl", topicID, "Keep history", projectRoot, "remaining turn", time.Now().Add(-time.Minute))
	ctrl := controllerWithContent(t, path)
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"only": {
				ID:            "only",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       topicID,
				TopicTitle:    "Keep history",
				Ctrl:          ctrl,
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"only"},
		activeTabID: "only",
	}

	if err := app.DeleteSession(path); err != nil {
		t.Fatalf("DeleteSession(topic with remaining history): %v", err)
	}

	found := false
	for _, tab := range app.tabs {
		if tab.TopicID == topicID {
			found = true
			if got := filepath.Clean(tab.currentSessionPath()); got != filepath.Clean(remainingPath) {
				t.Fatalf("fallback session path = %q, want remaining history %q", got, remainingPath)
			}
		}
	}
	if !found {
		t.Fatalf("fallback should keep topic %q when another session remains", topicID)
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
	if err != nil {
		t.Fatalf("DeleteSession should succeed after cleaning empty trash dir: %v", err)
	}
	if _, ok := app.tabs["test"]; ok {
		t.Fatalf("deleted session runtime should be removed from tabs")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session file should be moved out of active history, stat err = %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(path), filepath.Base(path))
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("session should be moved to trash: %v", err)
	}

	close(runner.release)
	waitNotRunning(t, ctrl)
}

func TestDeleteSessionValidTrashRemovesEmptyLiveStub(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "stale-live.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write live stub: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(path), filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o755); err != nil {
		t.Fatalf("create trash dir: %v", err)
	}
	if err := os.WriteFile(trashPath, []byte(`{"role":"user","content":"trashed"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write trash session: %v", err)
	}

	activePath := filepath.Join(dir, "active.jsonl")
	if err := os.WriteFile(activePath, []byte(`{"role":"user","content":"active"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write active session: %v", err)
	}
	activeCtrl := control.New(control.Options{SessionDir: dir, SessionPath: activePath, Label: "active"})
	defer activeCtrl.Close()
	app := &App{
		tabs:        map[string]*WorkspaceTab{"active": {ID: "active", Scope: "global", Ctrl: activeCtrl, Ready: true}},
		activeTabID: "active",
		tabOrder:    []string{"active"},
	}

	if err := app.DeleteSession(filepath.Base(path)); err != nil {
		t.Fatalf("DeleteSession should remove stale live stub: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("live stub should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("existing trash should remain authoritative: %v", err)
	}
}

func TestDeleteSessionValidTrashRemovesDuplicateLiveSession(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "duplicate-recovery.jsonl")
	content := []byte(`{"role":"user","content":"same recovery"}` + "\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write live session: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(path), filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o755); err != nil {
		t.Fatalf("create trash dir: %v", err)
	}
	if err := os.WriteFile(trashPath, content, 0o644); err != nil {
		t.Fatalf("write trash session: %v", err)
	}

	activePath := filepath.Join(dir, "active.jsonl")
	if err := os.WriteFile(activePath, []byte(`{"role":"user","content":"active"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write active session: %v", err)
	}
	activeCtrl := control.New(control.Options{SessionDir: dir, SessionPath: activePath, Label: "active"})
	defer activeCtrl.Close()
	app := &App{
		tabs:        map[string]*WorkspaceTab{"active": {ID: "active", Scope: "global", Ctrl: activeCtrl, Ready: true}},
		activeTabID: "active",
		tabOrder:    []string{"active"},
	}

	if err := app.DeleteSession(filepath.Base(path)); err != nil {
		t.Fatalf("DeleteSession should remove duplicate live session: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("duplicate live session should be removed, stat err = %v", err)
	}
	if got, err := os.ReadFile(trashPath); err != nil || string(got) != string(content) {
		t.Fatalf("existing trash should remain authoritative, got %q err=%v", string(got), err)
	}
}

func TestRestoreSessionRejectsOpenEmptyLiveStub(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "restore-open.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"trashed"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write trash source: %v", err)
	}
	if err := deleteSessionFile(dir, path); err != nil {
		t.Fatalf("trash source: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(path), filepath.Base(path))
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write live stub: %v", err)
	}
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "open"})
	defer ctrl.Close()
	app := &App{
		tabs:        map[string]*WorkspaceTab{"open": {ID: "open", Scope: "global", Ctrl: ctrl, Ready: true}},
		tabOrder:    []string{"open"},
		activeTabID: "open",
	}

	err := app.RestoreSession(trashPath)
	if err == nil || !strings.Contains(err.Error(), "session is open") {
		t.Fatalf("RestoreSession error = %v, want open-session rejection", err)
	}
	if info, statErr := os.Stat(path); statErr != nil || info.Size() != 0 {
		t.Fatalf("open live stub should remain empty, info=%v err=%v", info, statErr)
	}
	if _, err := os.Stat(trashPath); err != nil {
		t.Fatalf("trash session should remain after rejected restore: %v", err)
	}
}

func TestDeleteSessionValidTrashRenamesDifferentLiveConflict(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "real-live.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"new work"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write live session: %v", err)
	}
	trashPath := filepath.Join(dir, sessionTrashDir, filepath.Base(path), filepath.Base(path))
	if err := os.MkdirAll(filepath.Dir(trashPath), 0o755); err != nil {
		t.Fatalf("create trash dir: %v", err)
	}
	if err := os.WriteFile(trashPath, []byte(`{"role":"user","content":"trashed"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write trash session: %v", err)
	}

	activeCtrl := control.New(control.Options{SessionDir: dir, SessionPath: path, Label: "active"})
	defer activeCtrl.Close()
	app := NewApp()
	app.setTestCtrl(activeCtrl, "")

	if err := app.DeleteSession(filepath.Base(path)); err != nil {
		t.Fatalf("DeleteSession should move different live session to a unique trash item: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("live session should be moved out of active history, stat err = %v", err)
	}
	if got, err := os.ReadFile(trashPath); err != nil || !strings.Contains(string(got), "trashed") {
		t.Fatalf("original trash session should remain, got %q err=%v", string(got), err)
	}
	trashed, err := listTrashedSessionFiles(dir)
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	var renamedPath string
	for _, candidate := range trashed {
		if candidate != trashPath && filepath.Base(candidate) == filepath.Base(path) {
			renamedPath = candidate
			break
		}
	}
	if renamedPath == "" {
		t.Fatalf("renamed trash copy not found in %#v", trashed)
	}
	if filepath.Base(filepath.Dir(renamedPath)) == filepath.Base(path) {
		t.Fatalf("renamed trash copy reused fixed trash item dir: %s", renamedPath)
	}
	if got, err := os.ReadFile(renamedPath); err != nil || !strings.Contains(string(got), "new work") {
		t.Fatalf("renamed trash session = %q err=%v, want live content", string(got), err)
	}
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
	meta, ok, err := agent.LoadBranchMeta(pathA)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta after RenameSession ok=%v err=%v", ok, err)
	}
	if meta.CustomTitle != "A title" {
		t.Fatalf("custom title should be written to branch meta, got %q", meta.CustomTitle)
	}
	sessions = app.ListSessions()
	if len(sessions) != 1 || sessions[0].Title != "A title" {
		t.Fatalf("ListSessions should return custom title from branch meta, got %+v", sessions)
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

func TestDeleteSessionClearsAutoBotSessionMapping(t *testing.T) {
	isolateDesktopUserDirs(t)

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "bot-channel.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"from channel"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	other := filepath.Join(dir, "other-channel.jsonl")
	cfg := config.Default()
	cfg.Bot.Connections = []config.BotConnectionConfig{{
		ID: "weixin-weixin", Provider: "weixin", Domain: "weixin", Label: "微信", Enabled: true, Status: "connected",
		SessionMappings: []config.BotConnectionSessionMapping{
			{RemoteID: "remove-auto", SessionID: "path:" + path, SessionSource: "auto"},
			{RemoteID: "keep-explicit", SessionID: "path:" + path},
			{RemoteID: "keep-other-auto", SessionID: "path:" + other, SessionSource: "auto"},
		},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dir, SessionPath: filepath.Join(dir, "active.jsonl"), Label: "test"})
	app.setTestCtrl(ctrl, "")
	defer app.activeCtrl().Close()

	if err := app.DeleteSession(path); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	mappings := got.Bot.Connections[0].SessionMappings
	if len(mappings) != 2 {
		t.Fatalf("session mappings = %+v, want explicit and other auto mappings preserved", mappings)
	}
	for _, mapping := range mappings {
		if mapping.RemoteID == "remove-auto" {
			t.Fatalf("deleted session auto mapping was preserved: %+v", mappings)
		}
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

func TestUserTriggeredCommandsReturnErrorsWhenUnavailable(t *testing.T) {
	tests := []struct {
		name string
		app  *App
		call func(*App) error
		want string
	}{
		{
			name: "submit read-only",
			app: &App{
				tabs:        map[string]*WorkspaceTab{"test": {ID: "test", Scope: "global", ReadOnly: true}},
				activeTabID: "test",
			},
			call: func(app *App) error { return app.SubmitToTab("test", "hello") },
			want: "read-only",
		},
		{
			name: "submit workspace unavailable",
			app: &App{
				tabs:        map[string]*WorkspaceTab{"test": {ID: "test", Scope: "global", StartupErr: "boom"}},
				activeTabID: "test",
			},
			call: func(app *App) error { return app.SubmitToTab("test", "hello") },
			want: "workspace failed to start: boom",
		},
		{
			name: "run shell workspace unavailable",
			app: &App{
				tabs:        map[string]*WorkspaceTab{"test": {ID: "test", Scope: "global"}},
				activeTabID: "test",
			},
			call: func(app *App) error { return app.RunShellForTab("test", "echo hi") },
			want: "workspace is still starting",
		},
		{
			name: "steer workspace unavailable",
			app: &App{
				tabs:        map[string]*WorkspaceTab{"test": {ID: "test", Scope: "global"}},
				activeTabID: "test",
			},
			call: func(app *App) error { return app.SteerForTab("test", "please continue") },
			want: "workspace is still starting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(tt.app)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err, tt.want)
			}
		})
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

func TestCapabilitiesIncludesInstalledPlugins(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	root := filepath.Join(reasonixHome, "plugins", "superpowers")
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "plan", "SKILL.md"), []byte("---\ndescription: Plan work\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".codex-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex-plugin", "plugin.json"), []byte(`{
  "name": "superpowers",
  "version": "6.1.0",
  "description": "Planning workflows",
  "skills": "./skills/"
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "6.1.0",
		Description:  "Planning workflows",
		ManifestKind: "codex",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	plugins := app.Capabilities().Plugins
	if len(plugins) != 1 || plugins[0].Name != "superpowers" || plugins[0].Skills != 1 {
		t.Fatalf("Capabilities().Plugins = %+v", plugins)
	}
	if len(plugins[0].SkillDetails) != 1 || plugins[0].SkillDetails[0].Invocation != "/plan" {
		t.Fatalf("Capabilities().Plugins skill details = %+v", plugins[0].SkillDetails)
	}
}

func TestDesktopSharedHostBackgroundMCPAutoConnectsOnBoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping background MCP boot integration test in short mode")
	}

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

func TestRemoveMCPServerClearsRecordedStartupFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "broken"
command = "reasonix-missing-mcp-binary"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()
	recordMCPFailure(app.activeCtrl(), config.PluginEntry{
		Name:    "broken",
		Command: "reasonix-missing-mcp-binary",
	}, errors.New("connect: missing binary"))

	view := app.Capabilities()
	if len(view.Servers) != 1 || view.Servers[0].Name != "broken" || view.Servers[0].Status != "failed" {
		t.Fatalf("Capabilities before remove = %+v, want broken failed", view.Servers)
	}

	if err := app.RemoveMCPServer("broken"); err != nil {
		t.Fatalf("RemoveMCPServer(broken): %v", err)
	}
	if mcpFailed(app.activeCtrl(), "broken") {
		t.Fatalf("Host.Failures() still contains broken after remove: %+v", app.activeCtrl().Host().Failures())
	}
	view = app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "broken" {
			t.Fatalf("Capabilities after remove still contains broken: %+v", view.Servers)
		}
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

func TestRemoveMCPServerRejectsPluginManagedServerWithoutDisconnecting(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	srv := desktopMCPHTTPServer(t)
	defer srv.Close()
	reasonixHome := filepath.Join(home, ".reasonix")
	root := filepath.Join(reasonixHome, "plugins", "superpowers")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(fmt.Sprintf(`{
  "name": "superpowers",
  "version": "1.0.0",
  "mcpServers": {
    "helper": { "type": "http", "url": %q }
  }
}`, srv.URL)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "1.0.0",
		ManifestKind: "reasonix",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := findPluginEntry(cfg.Plugins, "helper")
	if !ok {
		t.Fatalf("plugin-managed MCP missing from config: %+v", cfg.Plugins)
	}
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	defer ctrl.Close()
	if _, err := ctrl.ConnectMCPServer(entry); err != nil {
		t.Fatalf("connect plugin-managed MCP: %v", err)
	}

	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.activeTab().WorkspaceRoot = dir
	err = app.RemoveMCPServer("helper")
	if err == nil || !strings.Contains(err.Error(), "managed by plugin") || !strings.Contains(err.Error(), "superpowers") {
		t.Fatalf("RemoveMCPServer(plugin-managed) error = %v", err)
	}
	if !mcpConnected(ctrl, "helper") {
		t.Fatal("plugin-managed MCP was disconnected despite rejected removal")
	}
	for action, actionErr := range map[string]error{
		"trust tool": app.TrustMCPServerTool("helper", "echo"),
		"clear auth": app.ClearMCPServerAuthentication("helper"),
		"update":     app.UpdateMCPServer("helper", MCPServerInput{Name: "helper", Transport: "http", URL: srv.URL}),
	} {
		if actionErr == nil || !strings.Contains(actionErr.Error(), "managed by plugin") {
			t.Fatalf("%s plugin-managed MCP error = %v", action, actionErr)
		}
	}
	if _, found := findPluginEntry(config.LoadForEdit(config.UserConfigPath()).Plugins, "helper"); found {
		t.Fatal("plugin-managed MCP mutation created a user-config shadow")
	}
	servers := app.MCPServers()
	if len(servers) != 1 || servers[0].Name != "helper" || servers[0].ManagedByPlugin != "superpowers" {
		t.Fatalf("MCPServers() = %+v, want helper managed by superpowers", servers)
	}
}

func TestRemoveMCPServerRejectsRuntimeOnlyServerWithoutDisconnecting(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	srv := desktopMCPHTTPServer(t)
	defer srv.Close()
	ctrl := control.New(control.Options{Host: plugin.NewHost()})
	defer ctrl.Close()
	if _, err := ctrl.ConnectMCPServer(config.PluginEntry{Name: "runtime-only", Type: "http", URL: srv.URL}); err != nil {
		t.Fatalf("connect runtime-only MCP: %v", err)
	}

	app := NewApp()
	app.setTestCtrl(ctrl, "")
	app.activeTab().WorkspaceRoot = dir
	err := app.RemoveMCPServer("runtime-only")
	if err == nil || !strings.Contains(err.Error(), "no removable MCP server") {
		t.Fatalf("RemoveMCPServer(runtime-only) error = %v", err)
	}
	if !mcpConnected(ctrl, "runtime-only") {
		t.Fatal("runtime-only MCP was disconnected despite failed persistence removal")
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

func TestTrustMCPServerToolPersistsTrustedReadOnlyTools(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[[plugins]]
name = "github"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-github"]
trusted_read_only_tools = ["pull_request_read"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.TrustMCPServerTool("github", " issue_read "); err != nil {
		t.Fatalf("TrustMCPServerTool(github, issue_read): %v", err)
	}
	if err := app.TrustMCPServerTool("github", "issue_read"); err != nil {
		t.Fatalf("TrustMCPServerTool duplicate: %v", err)
	}
	if err := app.TrustMCPServerTools("github", []string{" search_issues ", "issue_read", ""}); err != nil {
		t.Fatalf("TrustMCPServerTools(github): %v", err)
	}
	if err := app.UntrustMCPServerTool("github", " pull_request_read "); err != nil {
		t.Fatalf("UntrustMCPServerTool(github, pull_request_read): %v", err)
	}
	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	updated, ok := findPluginEntry(cfg.Plugins, "github")
	if !ok {
		t.Fatalf("github plugin missing: %+v", cfg.Plugins)
	}
	if !reflect.DeepEqual(updated.TrustedReadOnlyTools, []string{"issue_read", "search_issues"}) {
		t.Fatalf("trusted read-only tools = %+v", updated.TrustedReadOnlyTools)
	}
	for _, s := range app.MCPServers() {
		if s.Name == "github" {
			if !reflect.DeepEqual(s.TrustedReadOnlyTools, []string{"issue_read", "search_issues"}) {
				t.Fatalf("view trusted read-only tools = %+v", s.TrustedReadOnlyTools)
			}
			return
		}
	}
	t.Fatalf("github MCP missing from view")
}

func TestTrustMCPServerToolPersistsProjectMCPJSONEntry(t *testing.T) {
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

	if err := app.TrustMCPServerTool("codegraph", "codegraph_context"); err != nil {
		t.Fatalf("TrustMCPServerTool(.mcp.json codegraph): %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		MCPServers map[string]struct {
			TrustedReadOnlyTools []string `json:"trusted_read_only_tools"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.MCPServers["codegraph"].TrustedReadOnlyTools, []string{"codegraph_context"}) {
		t.Fatalf(".mcp.json trusted_read_only_tools = %+v", doc.MCPServers["codegraph"].TrustedReadOnlyTools)
	}
	cfg, err := config.LoadForRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	updated, ok := findPluginEntry(cfg.Plugins, "codegraph")
	if !ok || !reflect.DeepEqual(updated.TrustedReadOnlyTools, []string{"codegraph_context"}) {
		t.Fatalf("merged codegraph trusted_read_only_tools = %+v, found=%v", updated.TrustedReadOnlyTools, ok)
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
	app.activeTab().disabledMCP["playwright"] = ServerView{}

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

func TestRunShellForTabStaysBoundDuringRapidProjectTabSwitching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shell cancellation integration test in short mode")
	}

	isolateDesktopUserDirs(t)

	projectA := t.TempDir()
	projectB := t.TempDir()
	globalRoot := t.TempDir()
	shellEvents := make(chan event.Event, 64)
	projectEvents := make(chan event.Event, 64)
	globalEvents := make(chan event.Event, 64)
	shellCtrl := control.New(control.Options{
		Sink:          event.FuncSink(func(e event.Event) { shellEvents <- e }),
		WorkspaceRoot: projectA,
	})
	projectCtrl := control.New(control.Options{
		Sink:          event.FuncSink(func(e event.Event) { projectEvents <- e }),
		WorkspaceRoot: projectB,
	})
	globalCtrl := control.New(control.Options{
		Sink:          event.FuncSink(func(e event.Event) { globalEvents <- e }),
		WorkspaceRoot: globalRoot,
	})
	defer shellCtrl.Close()
	defer projectCtrl.Close()
	defer globalCtrl.Close()

	app := &App{
		tabs: map[string]*WorkspaceTab{
			"shell":     {ID: "shell", Scope: "project", WorkspaceRoot: projectA, Ctrl: shellCtrl, Ready: true},
			"project-b": {ID: "project-b", Scope: "project", WorkspaceRoot: projectB, Ctrl: projectCtrl, Ready: true},
			"global":    {ID: "global", Scope: "global", WorkspaceRoot: globalRoot, Ctrl: globalCtrl, Ready: true},
		},
		tabOrder:    []string{"shell", "project-b", "global"},
		activeTabID: "shell",
	}

	marker := "shell-route-marker.txt"
	if err := app.RunShellForTab("shell", longRunningMarkerCommand(marker)); err != nil {
		t.Fatalf("RunShellForTab: %v", err)
	}
	waitForShellDispatch(t, shellEvents, marker)
	waitForFile(t, filepath.Join(projectA, marker), "shell")

	for i := 0; i < 8; i++ {
		if err := app.SetActiveTab("project-b"); err != nil {
			t.Fatalf("SetActiveTab(project-b): %v", err)
		}
		if err := app.SetActiveTab("global"); err != nil {
			t.Fatalf("SetActiveTab(global): %v", err)
		}
		if err := app.SetActiveTab("shell"); err != nil {
			t.Fatalf("SetActiveTab(shell): %v", err)
		}
	}
	if err := app.SetActiveTab("project-b"); err != nil {
		t.Fatalf("SetActiveTab(project-b final): %v", err)
	}
	app.CancelTab("shell")

	cancelled := false
	deadline := time.After(15 * time.Second)
	for {
		select {
		case e := <-shellEvents:
			if e.Kind == event.ToolResult && e.Tool.Name == "bash" {
				cancelled = e.Tool.Err != ""
			}
			if e.Kind == event.TurnDone {
				if !cancelled {
					t.Fatal("shell tab finished without a cancelled shell result")
				}
				if _, err := os.Stat(filepath.Join(projectB, marker)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("shell marker appeared in project-b workspace: %v", err)
				}
				if got := activeTabIDForTest(app); got != "project-b" {
					t.Fatalf("active tab = %q, want project-b after background shell cancel", got)
				}
				assertNoEvents(t, projectEvents, "project-b")
				assertNoEvents(t, globalEvents, "global")
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for shell tab cancellation")
		}
	}
}

func longRunningMarkerCommand(marker string) string {
	if sandbox.ResolveShell("", "", nil).Kind == sandbox.ShellPowerShell {
		return fmt.Sprintf("Set-Content -LiteralPath %s -Value shell; Start-Sleep -Seconds 30", marker)
	}
	return fmt.Sprintf("printf shell > %s; sleep 30", marker)
}

func waitForShellDispatch(t *testing.T, ch <-chan event.Event, marker string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Kind == event.ToolDispatch && strings.Contains(e.Tool.Args, marker) {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for shell dispatch")
		}
	}
}

func activeTabIDForTest(app *App) string {
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.activeTabID
}

func assertNoEvents(t *testing.T, ch <-chan event.Event, name string) {
	t.Helper()
	select {
	case e := <-ch:
		t.Fatalf("%s received event while shell ran in another tab: %+v", name, e)
	default:
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
