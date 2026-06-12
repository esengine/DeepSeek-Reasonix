package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
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

	project := robustTempDir(t)
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

	project := robustTempDir(t)
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
	if !official["deepseek"] || !official["mimo-api"] || !official["mimo-token-plan"] {
		t.Fatalf("official providers = %+v, want deepseek, mimo-api, and mimo-token-plan", got.OfficialProviders)
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
	if !providers["mimo-token-plan"].Added || !providers["mimo-token-plan"].KeySet {
		t.Fatalf("mimo-token-plan provider = %+v, want inferred added key-set provider", providers["mimo-token-plan"])
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
	if !providers["mimo-token-plan"].Added || !providers["mimo-token-plan"].KeySet {
		t.Fatalf("mimo-token-plan provider = %+v, want inferred added provider from configured key", providers["mimo-token-plan"])
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

	if err := NewApp().AddOfficialProviderAccess("deepseek", "test-key"); err != nil {
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
	if access["deepseek"] || !access["mimo-token-plan"] {
		t.Fatalf("provider_access = %+v, want only mimo-token-plan", cfg.Desktop.ProviderAccess)
	}
	if cfg.DefaultModel != "mimo-token-plan/mimo-v2.5-pro" {
		t.Fatalf("default_model = %q, want mimo-token-plan/mimo-v2.5-pro", cfg.DefaultModel)
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
		"mimo-token-plan/mimo-v2.5-pro",
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
	if len(models) != 3 {
		t.Fatalf("Models() len = %d, want 3: %+v", len(models), models)
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
	if !refs["mimo-api/mimo-v2.5-pro"] {
		t.Fatalf("Models() refs = %+v, missing mimo-api/mimo-v2.5-pro", models)
	}
	if len(models) != 1 {
		t.Fatalf("Models() len = %d, want 1: %+v", len(models), models)
	}
}

func TestSetModelForTabRejectsProviderOutsideAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-test")
	t.Setenv("MIMO_API_KEY", "sk-test")

	cfg := config.Default()
	cfg.DefaultModel = "deepseek-flash/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek-flash"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	tab := &WorkspaceTab{ID: "tab_a", Scope: "global", Ready: true, model: "deepseek-flash/deepseek-v4-flash"}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID

	err := app.SetModelForTab(tab.ID, "mimo-flash/mimo-v2.5")
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
	if err := os.MkdirAll(filepath.Join(dir, ".codegraph", "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".codegraph", "cache", "runtime.js"), []byte("index"), 0o644); err != nil {
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
	for _, hidden := range []string{".codex", ".codegraph", ".npm", ".pnpm-store", "bin", "dist", "stage", "tmp"} {
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
		".codegraph/cache/runtime.js",
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

func TestCapabilitiesShowsDefaultMCPAsInitializingNotDisabled(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
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
			if s.Status != "initializing" {
				t.Fatalf("default MCP status = %q, want initializing; server = %+v", s.Status, s)
			}
			return
		}
	}
	t.Fatalf("playwright MCP missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesShowsDefaultCodegraphDisabled(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
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
			if s.Tier != "background" {
				t.Fatalf("codegraph tier = %q, want background; server = %+v", s.Tier, s)
			}
			return
		}
	}
	t.Fatalf("codegraph missing from Capabilities: %+v", view.Servers)
}

func TestCapabilitiesShowsBuiltInMCPDefaults(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	want := map[string][]string{
		"time": []string{"builtin-mcp", "time"},
	}
	found := map[string]bool{}
	for _, s := range view.Servers {
		if s.Name != "time" && s.Name != "context7" {
			continue
		}
		found[s.Name] = true
		wantStatus := map[string]string{"time": "deferred", "context7": "disabled"}[s.Name]
		wantAutoStart := s.Name == "time"
		if s.Status != wantStatus {
			t.Fatalf("%s status = %q, want %s; server = %+v", s.Name, s.Status, wantStatus, s)
		}
		if !s.BuiltIn || !s.Configured || s.AutoStart != wantAutoStart {
			t.Fatalf("%s builtIn/configured/autoStart = %v/%v/%v, want true/true/%v; server = %+v", s.Name, s.BuiltIn, s.Configured, s.AutoStart, wantAutoStart, s)
		}
		if s.Tier != "lazy" || s.Transport != "stdio" || strings.TrimSpace(s.Command) == "" {
			t.Fatalf("%s transport/tier/command = %q/%q/%q, want stdio/lazy/non-empty; server = %+v", s.Name, s.Transport, s.Tier, s.Command, s)
		}
		if s.Name == "time" && !reflect.DeepEqual(s.Args, want["time"]) {
			t.Fatalf("time args = %+v, want %+v", s.Args, want["time"])
		}
		if s.Name == "context7" && !validContext7Runner(s.Command, s.Args) {
			t.Fatalf("context7 runner = %q %+v, want npx/pnpm/bunx for @upstash/context7-mcp", s.Command, s.Args)
		}
	}
	for _, name := range []string{"time", "context7"} {
		if !found[name] {
			t.Fatalf("built-in MCP %s missing from Capabilities: %+v", name, view.Servers)
		}
	}
}

func TestCapabilitiesShowsManuallyEnabledContext7Deferred(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

[builtin_mcp]
context7_enabled = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "context7" {
			if s.Status != "deferred" || !s.AutoStart || !s.BuiltIn || !s.Configured {
				t.Fatalf("enabled context7 view = %+v, want deferred built-in configured autoStart", s)
			}
			return
		}
	}
	t.Fatalf("context7 missing from Capabilities: %+v", view.Servers)
}

func validContext7Runner(command string, args []string) bool {
	switch command {
	case "npx":
		return reflect.DeepEqual(args, []string{"-y", "@upstash/context7-mcp"})
	case "pnpm":
		return reflect.DeepEqual(args, []string{"dlx", "@upstash/context7-mcp"})
	case "bunx":
		return reflect.DeepEqual(args, []string{"@upstash/context7-mcp"})
	default:
		return false
	}
}

func TestConfiguredMCPWithBuiltInNameTakesPrecedence(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

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
			t.Fatalf("configured time view = %+v, want user config to take precedence over built-in", s)
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

func TestEditAndRemoveConfiguredMCPWithBuiltInName(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(`
[codegraph]
enabled = false

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

func TestSetBuiltInMCPDisabledWritesBuiltInConfigOnly(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{Host: plugin.NewHost()}), "")
	defer app.activeCtrl().Close()

	if err := app.SetMCPServerEnabled("time", false); err != nil {
		t.Fatalf("SetMCPServerEnabled(time,false): %v", err)
	}
	view := app.Capabilities()
	for _, s := range view.Servers {
		if s.Name == "time" {
			if s.Status != "disabled" || !s.BuiltIn || !s.Configured {
				t.Fatalf("time disabled view = %+v, want disabled built-in configured", s)
			}
			cfg := config.LoadForEdit(config.UserConfigPath())
			if _, ok := findPluginEntry(cfg.Plugins, "time"); ok {
				t.Fatalf("time built-in disable wrote a user plugin: %+v", cfg.Plugins)
			}
			if cfg.BuiltInMCP.TimeEnabled {
				t.Fatalf("time built-in disable left time_enabled true: %+v", cfg.BuiltInMCP)
			}
			return
		}
	}
	t.Fatalf("time missing from Capabilities after disable: %+v", view.Servers)
}

func TestCapabilitiesMarksBackgroundRemoteMCPAuthPossible(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
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
			if s.Status != "initializing" || s.AuthStatus != "possible" || s.AuthURL != "https://mcp.dida365.com" {
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
	dir := robustTempDir(t)
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
	dir := robustTempDir(t)
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
			if s.Status != "initializing" || s.AuthStatus != "possible" {
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
[codegraph]
enabled = false

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
[codegraph]
enabled = false

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

func TestSetMCPServerTierRecordsConnectFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
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

func TestSetMCPServerTierEnablesCodegraphAndIgnoresLegacyTier(t *testing.T) {
	t.Setenv("HOME", robustTempDir(t))
	t.Setenv("USERPROFILE", robustTempDir(t))
	t.Setenv("XDG_CONFIG_HOME", robustTempDir(t))
	t.Setenv("AppData", robustTempDir(t))
	t.Setenv("PATH", robustTempDir(t))
	t.Setenv("REASONIX_CACHE_DIR", robustTempDir(t)) // isolate the codegraph bundle cache so Resolve fails deterministically
	dir := robustTempDir(t)
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

	if err := app.SetMCPServerTier("codegraph", "eager"); err != nil {
		t.Fatalf("SetMCPServerTier(codegraph): %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Codegraph.Enabled {
		t.Fatal("codegraph enabled = false, want true after legacy tier update")
	}
	if got := cfg.Codegraph.Tier; got != "" {
		t.Fatalf("codegraph tier = %q, want ignored legacy tier", got)
	}
	userCfg := config.LoadForEdit(config.UserConfigPath())
	if !userCfg.Codegraph.Enabled {
		t.Fatal("user codegraph enabled = false, want true after legacy tier update")
	}
	if got := userCfg.Codegraph.Tier; got != "" {
		t.Fatalf("user codegraph tier = %q, want ignored legacy tier", got)
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
	dir := robustTempDir(t)
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

func TestCapabilitiesMigratesFailedMCPConfiguredTierAfterRestart(t *testing.T) {
	isolateDesktopUserDirs(t)
	dir := robustTempDir(t)
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
