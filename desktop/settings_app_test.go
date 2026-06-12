package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
)

func TestWithFreshSystemPromptReplacesExistingSystemMessage(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "old", ReasoningContent: "stale", ReasoningSignature: "sig", ToolCalls: []provider.ToolCall{{ID: "call", Name: "noop"}}, ToolCallID: "tool", Name: "name"},
		{Role: provider.RoleUser, Content: "hello"},
	}

	got := withFreshSystemPrompt(msgs, "new")
	if got[0].Content != "new" {
		t.Fatalf("system prompt = %q, want new", got[0].Content)
	}
	if got[0].ReasoningContent != "" || got[0].ReasoningSignature != "" || len(got[0].ToolCalls) != 0 || got[0].ToolCallID != "" || got[0].Name != "" {
		t.Fatalf("system metadata should be cleared, got %+v", got[0])
	}
	if got[1].Content != "hello" {
		t.Fatalf("non-system message changed: %+v", got[1])
	}
	if msgs[0].Content != "old" {
		t.Fatalf("input slice was mutated: %+v", msgs[0])
	}
}

func TestWithFreshSystemPromptPrependsMissingSystemMessage(t *testing.T) {
	msgs := []provider.Message{{Role: provider.RoleUser, Content: "hello"}}

	got := withFreshSystemPrompt(msgs, "new")
	if len(got) != 2 || got[0].Role != provider.RoleSystem || got[0].Content != "new" {
		t.Fatalf("expected prepended system prompt, got %+v", got)
	}
	if got[1].Content != "hello" {
		t.Fatalf("existing user message changed: %+v", got[1])
	}
}

func TestProviderViewFromEntry_FiltersNonChatModels(t *testing.T) {
	p := config.ProviderEntry{
		Name: "mimo-api",
		Models: []string{
			"mimo-v2", "mimo-v2-pro",
			"mimo-v2-asr", "mimo-v2-tts",
			"mimo-v2-tts-voiceclone", "mimo-v2-tts-voicedesign",
		},
	}
	view := providerViewFromEntry(p, true, false)
	want := []string{"mimo-v2", "mimo-v2-pro"}
	if !reflect.DeepEqual(view.Models, want) {
		t.Errorf("ProviderView.Models = %v, want %v", view.Models, want)
	}
}

func TestFetchProviderModelsFiltersNonChatModels(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "mimo-v2.5-pro", "object": "model"},
				{"id": "mimo-v2.5-asr", "object": "model"},
				{"id": "mimo-v2.5-tts", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	got, err := NewApp().FetchProviderModels(ProviderView{
		Name:      "mimo-api",
		BaseURL:   srv.URL,
		APIKeyEnv: "TEST_PROVIDER_KEY",
	})
	if err != nil {
		t.Fatalf("FetchProviderModels: %v", err)
	}
	want := []string{"mimo-v2.5-pro"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FetchProviderModels = %v, want %v", got, want)
	}
}

func TestSaveProviderFiltersNonChatModels(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:      "mimo-api",
		Kind:      "openai",
		BaseURL:   "https://api.xiaomimimo.com/v1",
		Models:    []string{"mimo-v2.5-asr", "mimo-v2.5-pro", "mimo-v2.5-tts"},
		Default:   "mimo-v2.5-asr",
		APIKeyEnv: "MIMO_API_KEY",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("saved provider not found")
	}
	want := []string{"mimo-v2.5-pro"}
	if !reflect.DeepEqual(got.ModelList(), want) {
		t.Errorf("saved provider models = %v, want %v", got.ModelList(), want)
	}
	if got.DefaultModel() != "mimo-v2.5-pro" {
		t.Errorf("saved provider default = %q, want mimo-v2.5-pro", got.DefaultModel())
	}
}

func TestSetAgentParamsPersistsStepLimitsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetAgentParams(0.35, 37, 9, "custom system"); err != nil {
		t.Fatalf("SetAgentParams: %v", err)
	}

	view := app.Settings()
	if view.Agent.MaxSteps != 37 || view.Agent.PlannerMaxSteps != 9 {
		t.Fatalf("Settings().Agent = %+v, want maxSteps=37 plannerMaxSteps=9", view.Agent)
	}
	if view.Agent.Temperature != 0.35 || view.Agent.SystemPrompt != "custom system" {
		t.Fatalf("Settings().Agent did not preserve other agent params: %+v", view.Agent)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.MaxSteps != 37 || cfg.Agent.PlannerMaxSteps != 9 {
		t.Fatalf("saved config agent steps = max:%d planner:%d, want 37/9", cfg.Agent.MaxSteps, cfg.Agent.PlannerMaxSteps)
	}
	if cfg.Agent.Temperature != 0.35 || cfg.Agent.SystemPrompt != "custom system" {
		t.Fatalf("saved config did not preserve other agent params: %+v", cfg.Agent)
	}
}

func TestSetDesktopCheckUpdatesPersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if !app.Settings().CheckUpdates {
		t.Fatal("Settings().CheckUpdates default = false, want true")
	}
	if err := app.SetDesktopCheckUpdates(false); err != nil {
		t.Fatalf("SetDesktopCheckUpdates: %v", err)
	}
	view := app.Settings()
	if view.CheckUpdates {
		t.Fatal("Settings().CheckUpdates = true, want false")
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Desktop.CheckUpdates == nil || *cfg.Desktop.CheckUpdates {
		t.Fatalf("desktop.check_updates = %+v, want false", cfg.Desktop.CheckUpdates)
	}
	if cfg.DesktopCheckUpdates() {
		t.Fatal("DesktopCheckUpdates() = true, want false")
	}
}

func TestSaveHooksSettingsPreservesUnknownSettingsKeys(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := hook.GlobalSettingsPath("")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark","hooks":{"Stop":[{"command":"old"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	if err := app.SaveHooksSettings("global", []HookConfigView{{
		Event:   string(hook.PreToolUse),
		Match:   "bash",
		Command: "echo guard",
	}}); err != nil {
		t.Fatalf("SaveHooksSettings: %v", err)
	}

	var raw map[string]json.RawMessage
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["theme"]) != `"dark"` {
		t.Fatalf("theme key was not preserved: %s", raw["theme"])
	}
	view := app.HooksSettings("global")
	if len(view.Hooks) != 1 || view.Hooks[0].Event != string(hook.PreToolUse) || view.Hooks[0].Command != "echo guard" {
		t.Fatalf("HooksSettings = %+v, want saved PreToolUse hook", view)
	}
}

func TestProjectHooksSettingsUseActiveWorkspaceRootAndTrust(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	project := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"project": {ID: "project", Scope: "project", WorkspaceRoot: project, Ready: true},
	}
	app.activeTabID = "project"

	if err := app.SaveHooksSettings("project", []HookConfigView{{
		Event:       string(hook.Stop),
		Command:     "echo done",
		Description: "Turn done",
	}}); err != nil {
		t.Fatalf("SaveHooksSettings(project): %v", err)
	}
	if err := app.TrustProjectHooks(); err != nil {
		t.Fatalf("TrustProjectHooks: %v", err)
	}
	if !hook.IsTrusted(project, home) {
		t.Fatal("project hooks were not trusted")
	}
	view := app.HooksSettings("project")
	if view.Scope != "project" || view.ProjectRoot != project || !view.Trusted {
		t.Fatalf("project hook view metadata = %+v", view)
	}
	if len(view.Hooks) != 1 || view.Hooks[0].Event != string(hook.Stop) || view.Hooks[0].Description != "Turn done" {
		t.Fatalf("project hooks = %+v", view.Hooks)
	}
	if _, err := os.Stat(filepath.Join(project, ".reasonix", "settings.json")); err != nil {
		t.Fatalf("project hooks settings file missing: %v", err)
	}
}

func TestTrustProjectHooksForRootUsesDisplayedProjectRoot(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	projectA := t.TempDir()
	projectB := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"a": {ID: "a", Scope: "project", WorkspaceRoot: projectA, Ready: true},
		"b": {ID: "b", Scope: "project", WorkspaceRoot: projectB, Ready: true},
	}
	app.activeTabID = "b"

	if err := app.TrustProjectHooksForRoot(projectA); err != nil {
		t.Fatalf("TrustProjectHooksForRoot: %v", err)
	}
	if !hook.IsTrusted(projectA, home) {
		t.Fatal("displayed project root was not trusted")
	}
	if hook.IsTrusted(projectB, home) {
		t.Fatal("active project root was trusted instead of displayed project root")
	}
}

func TestSaveHooksSettingsForRootUsesDisplayedProjectRoot(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectA := t.TempDir()
	projectB := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"a": {ID: "a", Scope: "project", WorkspaceRoot: projectA, Ready: true},
		"b": {ID: "b", Scope: "project", WorkspaceRoot: projectB, Ready: true},
	}
	app.activeTabID = "b"

	if err := app.SaveHooksSettingsForRoot("project", projectA, []HookConfigView{{
		Event:   string(hook.Stop),
		Command: "echo done",
	}}); err != nil {
		t.Fatalf("SaveHooksSettingsForRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectA, ".reasonix", "settings.json")); err != nil {
		t.Fatalf("displayed project root settings missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectB, ".reasonix", "settings.json")); err == nil {
		t.Fatal("active project root was written instead of displayed project root")
	}
}
