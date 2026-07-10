package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
)

type captureTurnRunner struct {
	inputs []string
}

func (r *captureTurnRunner) Run(_ context.Context, input string) error {
	r.inputs = append(r.inputs, input)
	return nil
}

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
		VisionModels: []string{"mimo-v2", "mimo-v2-asr", "mimo-v2-omni"},
	}
	view := providerViewFromEntry(p, true, false)
	want := []string{"mimo-v2", "mimo-v2-pro"}
	if !reflect.DeepEqual(view.Models, want) {
		t.Errorf("ProviderView.Models = %v, want %v", view.Models, want)
	}
	if got, want := view.VisionModels, []string{"mimo-v2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ProviderView.VisionModels = %v, want %v", got, want)
	}
	if !view.VisionModelsSet {
		t.Fatal("ProviderView.VisionModelsSet = false, want true for configured vision_models")
	}
}

func TestProviderViewFromEntry_MigratesProviderWideVision(t *testing.T) {
	p := config.ProviderEntry{
		Name:   "custom",
		Models: []string{"text-only", "qwen-vl-plus"},
		Vision: true,
	}
	view := providerViewFromEntry(p, false, true)
	if got, want := view.VisionModels, []string{"text-only", "qwen-vl-plus"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ProviderView.VisionModels = %v, want %v", got, want)
	}
	if !view.VisionModelsSet {
		t.Fatal("ProviderView.VisionModelsSet = false, want true for provider-wide vision")
	}
}

func TestProviderViewFromEntryIncludesThinking(t *testing.T) {
	view := providerViewFromEntry(config.ProviderEntry{
		Name:     "anthropic",
		Thinking: "ADAPTIVE",
	}, false, true)
	if view.Thinking != "adaptive" {
		t.Fatalf("ProviderView.Thinking = %q, want adaptive", view.Thinking)
	}
}

func TestProviderViewFromEntryShowsKeySource(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("TEST_PROVIDER_KEY_SOURCE", "")
	os.Unsetenv("TEST_PROVIDER_KEY_SOURCE")
	if _, err := config.SetCredential("TEST_PROVIDER_KEY_SOURCE", "sk-test"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}

	view := providerViewFromEntry(config.ProviderEntry{
		Name:      "custom",
		APIKeyEnv: "TEST_PROVIDER_KEY_SOURCE",
	}, false, true)
	if !view.KeySet {
		t.Fatal("KeySet = false, want true")
	}
	if !view.Configured {
		t.Fatal("Configured = false, want true from resolved credentials")
	}
	if view.KeySource == "" || !strings.Contains(view.KeySource, "credentials") {
		t.Fatalf("KeySource = %q, want credentials source", view.KeySource)
	}
}

func TestSettingsExposesEffectiveSandboxWriteRoots(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	project := robustTempDir(t)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Sandbox.AllowWrite = []string{
		"${HOME}/.m2",
		"${HOME}/.m2/repository",
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"project": {ID: "project", Scope: "project", WorkspaceRoot: project, Ready: true},
	}
	app.activeTabID = "project"

	got := app.Settings().Sandbox
	if got.EffectiveWorkspaceRoot != project {
		t.Fatalf("EffectiveWorkspaceRoot = %q, want %q", got.EffectiveWorkspaceRoot, project)
	}
	// Settings expose expanded configured roots; the writer confiner normalizes
	// separators later when enforcing them.
	want := []string{
		project,
		home + "/.m2",
		home + "/.m2/repository",
	}
	if !reflect.DeepEqual(got.EffectiveWriteRoots, want) {
		t.Fatalf("EffectiveWriteRoots = %v, want %v", got.EffectiveWriteRoots, want)
	}
	if !reflect.DeepEqual(got.AllowWrite, cfg.Sandbox.AllowWrite) {
		t.Fatalf("AllowWrite = %v, want raw configured paths %v", got.AllowWrite, cfg.Sandbox.AllowWrite)
	}
	if got.EffectiveShell == "" {
		t.Fatal("EffectiveShell is empty")
	}
}

func TestSandboxEffectiveShellViewLabels(t *testing.T) {
	cases := []struct {
		name  string
		shell sandbox.Shell
		want  string
	}{
		{"bash", sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}, "bash"},
		{"git bash", sandbox.Shell{Kind: sandbox.ShellBash, Path: `C:\Program Files\Git\bin\bash.exe`}, "git-bash"},
		{"windows powershell", sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"}, "powershell"},
		{"pwsh", sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "pwsh"}, "pwsh"},
	}
	for _, tc := range cases {
		if got := sandboxEffectiveShellView(tc.shell); got != tc.want {
			t.Errorf("%s: sandboxEffectiveShellView() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestProviderViewFromEntryExposesNoAuthAvailability(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("LOCAL_API_KEY", "")
	os.Unsetenv("LOCAL_API_KEY")

	noAuth := providerViewFromEntry(config.ProviderEntry{
		Name:    "local",
		Kind:    "openai",
		BaseURL: "http://127.0.0.1:23333/v1",
		Models:  []string{"model-a"},
	}, false, true)
	if noAuth.RequiresKey {
		t.Fatal("no-auth provider RequiresKey = true, want false")
	}
	if !noAuth.Configured {
		t.Fatal("no-auth provider Configured = false, want true")
	}
	if noAuth.KeySet {
		t.Fatal("no-auth provider KeySet = true, want false")
	}

	legacyLoopback := providerViewFromEntry(config.ProviderEntry{
		Name:      "local",
		Kind:      "openai",
		BaseURL:   "http://127.0.0.1:23333/v1",
		Models:    []string{"model-a"},
		APIKeyEnv: "LOCAL_API_KEY",
	}, false, true)
	if legacyLoopback.RequiresKey {
		t.Fatal("loopback provider with missing legacy key env RequiresKey = true, want false")
	}
	if !legacyLoopback.Configured {
		t.Fatal("loopback provider with missing legacy key env Configured = false, want true")
	}

	official := providerViewFromEntry(config.ProviderEntry{
		Name:    "deepseek",
		Kind:    "openai",
		BaseURL: "https://api.deepseek.com",
		Models:  []string{"deepseek-v4-flash"},
	}, true, true)
	if !official.RequiresKey {
		t.Fatal("official provider RequiresKey = false, want true")
	}
	if official.Configured {
		t.Fatal("official provider without key Configured = true, want false")
	}
}

func TestSetProviderKeyDoesNotWarnWhenProjectEnvAlsoDefinesSavedKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("TEST_PROVIDER_SHADOW=old-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_PROVIDER_SHADOW", "")
	os.Unsetenv("TEST_PROVIDER_SHADOW")

	app := &App{
		tabs:        map[string]*WorkspaceTab{"project": {ID: "project", WorkspaceRoot: project}},
		activeTabID: "project",
	}
	warning, err := app.SetProviderKey("TEST_PROVIDER_SHADOW", "new-key")
	if err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	if warning != "" {
		t.Fatalf("SetProviderKey warning = %q, want no warning because provider keys use global credentials only", warning)
	}
	data, readErr := os.ReadFile(config.UserCredentialsPath())
	if readErr != nil {
		t.Fatalf("read credentials: %v", readErr)
	}
	if !strings.Contains(string(data), "TEST_PROVIDER_SHADOW=new-key") {
		t.Fatalf("saved credentials missing new key:\n%s", data)
	}
}

func TestSetProviderKeyDoesNotWarnWhenEnvironmentAlsoDefinesSavedKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("TEST_PROVIDER_EMPTY_ENV", "")

	app := &App{}
	warning, err := app.SetProviderKey("TEST_PROVIDER_EMPTY_ENV", "new-key")
	if err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	if warning != "" {
		t.Fatalf("SetProviderKey warning = %q, want no warning because provider keys use global credentials only", warning)
	}
	data, readErr := os.ReadFile(config.UserCredentialsPath())
	if readErr != nil {
		t.Fatalf("read credentials: %v", readErr)
	}
	if !strings.Contains(string(data), "TEST_PROVIDER_EMPTY_ENV=new-key") {
		t.Fatalf("saved credentials missing new key:\n%s", data)
	}
}

func TestSetProviderKeyDoesNotWarnWhenEmptyProjectEnvAlsoDefinesSavedKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("TEST_PROVIDER_EMPTY_PROJECT=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_PROVIDER_EMPTY_PROJECT", "")
	os.Unsetenv("TEST_PROVIDER_EMPTY_PROJECT")

	app := &App{
		tabs:        map[string]*WorkspaceTab{"project": {ID: "project", WorkspaceRoot: project}},
		activeTabID: "project",
	}
	warning, err := app.SetProviderKey("TEST_PROVIDER_EMPTY_PROJECT", "new-key")
	if err != nil {
		t.Fatalf("SetProviderKey: %v", err)
	}
	if warning != "" {
		t.Fatalf("SetProviderKey warning = %q, want no warning because provider keys use global credentials only", warning)
	}
}

func TestFetchProviderModelsFiltersNonChatModels(t *testing.T) {
	isolateDesktopUserDirs(t)
	if _, err := config.SetCredential("TEST_PROVIDER_KEY", "test-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
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

func TestFetchProviderModelsUsesSavedCredentialBeforeEnvironment(t *testing.T) {
	isolateDesktopUserDirs(t)
	const keyEnv = "TEST_PROVIDER_FETCH_KEY"
	if _, err := config.SetCredential(keyEnv, "saved-key"); err != nil {
		t.Fatalf("SetCredential: %v", err)
	}
	t.Setenv(keyEnv, "stale-env-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer saved-key" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "model-a", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	got, err := NewApp().FetchProviderModels(ProviderView{
		Name:      "custom",
		BaseURL:   srv.URL,
		APIKeyEnv: keyEnv,
	})
	if err != nil {
		t.Fatalf("FetchProviderModels: %v", err)
	}
	if want := []string{"model-a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("FetchProviderModels = %v, want %v", got, want)
	}
	if got := os.Getenv(keyEnv); got != "stale-env-key" {
		t.Fatalf("process env = %q, want stale env left untouched", got)
	}
}

func TestSaveProviderFiltersNonChatModels(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:    "mimo-api",
		Kind:    "openai",
		BaseURL: "https://api.xiaomimimo.com/v1",
		Models:  []string{"mimo-v2.5-asr", "mimo-v2.5-pro", "mimo-v2.5-tts"},
		VisionModels: []string{
			"mimo-v2.5-asr",
			"mimo-v2.5-pro",
			"mimo-v2.5-tts",
		},
		VisionModelsSet: true,
		Default:         "mimo-v2.5-asr",
		APIKeyEnv:       "MIMO_API_KEY",
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
	if got, want := got.VisionModels, []string{"mimo-v2.5-pro"}; !reflect.DeepEqual(got, want) {
		t.Errorf("saved provider vision_models = %v, want %v", got, want)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	saved := string(raw)
	blockStart := strings.Index(saved, "\n[[providers]]\nname        = \"mimo-api\"")
	if blockStart < 0 {
		t.Fatalf("saved config missing mimo-api provider block:\n%s", raw)
	}
	block := saved[blockStart:]
	if next := strings.Index(block[len("\n[[providers]]"):], "\n[[providers]]"); next >= 0 {
		block = block[:len("\n[[providers]]")+next]
	}
	if !strings.Contains(block, `models      = ["mimo-v2.5-pro"]`) {
		t.Fatalf("saved provider block did not persist single selection as models array:\n%s", block)
	}
	if strings.Contains(block, `model       = "mimo-v2.5-pro"`) {
		t.Fatalf("saved provider block should not persist explicit single selection as legacy model:\n%s", block)
	}
	if !strings.Contains(block, `vision_models = ["mimo-v2.5-pro"]`) {
		t.Fatalf("saved provider block did not persist filtered vision_models:\n%s", block)
	}
}

func TestSaveProviderPersistsThinkingOverride(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:      "glm-proxy",
		Kind:      "openai",
		BaseURL:   "https://proxy.example.com/v1",
		Models:    []string{"glm-4.5-air"},
		APIKeyEnv: "GLM_PROXY_API_KEY",
		Thinking:  "DISABLED",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("glm-proxy")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.Thinking != "disabled" {
		t.Fatalf("saved provider thinking = %q, want disabled", got.Thinking)
	}
}

func TestSaveProviderPersistsAuthHeader(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:       "minimax-global-anthropic",
		Kind:       "anthropic",
		BaseURL:    "https://api.minimax.io/anthropic",
		Models:     []string{"MiniMax-M3"},
		APIKeyEnv:  "MINIMAX_API_KEY",
		AuthHeader: true,
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("minimax-global-anthropic")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if !got.AuthHeader {
		t.Fatal("saved provider auth_header = false, want true")
	}
	view := providerViewFromEntry(*got, false, true)
	if !view.AuthHeader {
		t.Fatal("provider view authHeader = false, want true")
	}
}

func TestSaveProviderPersistsCustomEndpointURLs(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:      "sub2api",
		Kind:      "openai",
		BaseURL:   "https://proxy.example.com/v1",
		ChatURL:   " https://proxy.example.com/custom/chat/completions ",
		ModelsURL: " https://proxy.example.com/v1/models ",
		Models:    []string{"model-a"},
		Default:   "model-a",
		APIKeyEnv: "SUB2API_KEY",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("sub2api")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.ChatURL != "https://proxy.example.com/custom/chat/completions" {
		t.Fatalf("saved chat_url = %q", got.ChatURL)
	}
	if got.ModelsURL != "https://proxy.example.com/v1/models" {
		t.Fatalf("saved models_url = %q", got.ModelsURL)
	}

	view := app.Settings()
	for _, provider := range view.Providers {
		if provider.Name != "sub2api" {
			continue
		}
		if provider.ChatURL != "https://proxy.example.com/custom/chat/completions" {
			t.Fatalf("Settings chatUrl = %q", provider.ChatURL)
		}
		if provider.ModelsURL != "https://proxy.example.com/v1/models" {
			t.Fatalf("Settings modelsUrl = %q", provider.ModelsURL)
		}
		return
	}
	t.Fatalf("Settings providers missing sub2api: %+v", view.Providers)
}

func TestSaveProviderPreservesHiddenProviderFields(t *testing.T) {
	isolateDesktopUserDirs(t)

	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{
		Name:         "custom",
		Kind:         "openai",
		BaseURL:      "https://proxy.example.com/v1",
		Models:       []string{"model-a", "model-b"},
		Default:      "model-a",
		APIKeyEnv:    "CUSTOM_API_KEY",
		Price:        &provider.Pricing{Input: 1, Output: 2, Currency: "$"},
		Prices:       map[string]*provider.Pricing{"model-b": {Input: 3, Output: 4, Currency: "$"}},
		Thinking:     "adaptive",
		Effort:       "high",
		VisionDetail: "low",
		ExtraBody:    map[string]any{"enable_thinking": true},
		NoProxy:      true,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	app := NewApp()
	settings := app.Settings()
	var view ProviderView
	found := false
	for _, p := range settings.Providers {
		if p.Name == "custom" {
			view = p
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Settings providers missing custom: %+v", settings.Providers)
	}
	if view.ExtraBody["enable_thinking"] != true {
		t.Fatalf("settings extra_body = %+v, want enable_thinking=true", view.ExtraBody)
	}

	if err := app.SaveProvider(view); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	gotCfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := gotCfg.Provider("custom")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.Price == nil || got.Price.Input != 1 || got.Price.Output != 2 || got.Price.Currency != "$" {
		t.Fatalf("provider-wide price = %+v, want preserved", got.Price)
	}
	if got.Prices["model-b"] == nil || got.Prices["model-b"].Input != 3 || got.Prices["model-b"].Output != 4 || got.Prices["model-b"].Currency != "$" {
		t.Fatalf("per-model prices = %+v, want model-b price preserved", got.Prices)
	}
	if got.Thinking != "adaptive" || got.Effort != "high" {
		t.Fatalf("thinking/effort = %q/%q, want adaptive/high", got.Thinking, got.Effort)
	}
	if got.VisionDetail != "low" {
		t.Fatalf("vision_detail = %q, want low", got.VisionDetail)
	}
	if got.ExtraBody["enable_thinking"] != true {
		t.Fatalf("extra_body = %+v, want enable_thinking=true", got.ExtraBody)
	}
	if !got.NoProxy {
		t.Fatal("no_proxy = false, want preserved true")
	}
}

func TestSaveProviderClearsProviderWideVisionForPerModelSelection(t *testing.T) {
	isolateDesktopUserDirs(t)

	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://proxy.example.com/v1",
		Models:  []string{"text-only", "qwen-vl-plus"},
		Default: "text-only",
		Vision:  true,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	if err := NewApp().SaveProvider(ProviderView{
		Name:            "custom",
		Kind:            "openai",
		BaseURL:         "https://proxy.example.com/v1",
		Models:          []string{"text-only", "qwen-vl-plus"},
		VisionModels:    []string{"qwen-vl-plus"},
		VisionModelsSet: true,
		Default:         "text-only",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	gotCfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := gotCfg.Provider("custom")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.Vision {
		t.Fatal("saved provider kept provider-wide vision=true")
	}
	if got, want := got.VisionModels, []string{"qwen-vl-plus"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("saved provider vision_models = %v, want %v", got, want)
	}
	textOnly := *got
	textOnly.Model = "text-only"
	if config.EffectiveVision(&textOnly) {
		t.Fatal("unchecked text-only model should not inherit image input")
	}
	vision := *got
	vision.Model = "qwen-vl-plus"
	if !config.EffectiveVision(&vision) {
		t.Fatal("checked vision model should keep image input")
	}
}

func TestSaveProviderPreservesExplicitEmptyVisionModels(t *testing.T) {
	isolateDesktopUserDirs(t)

	if err := NewApp().SaveProvider(ProviderView{
		Name:            "custom",
		Kind:            "openai",
		BaseURL:         "https://proxy.example.com/v1",
		Models:          []string{"text-only", "qwen-vl-plus"},
		VisionModels:    []string{},
		VisionModelsSet: true,
		Default:         "text-only",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("custom")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.VisionModels == nil || len(got.VisionModels) != 0 {
		t.Fatalf("saved provider vision_models = %#v, want explicit empty list", got.VisionModels)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(raw), `vision_models = []`) {
		t.Fatalf("saved config did not persist explicit empty vision_models:\n%s", raw)
	}
}

func TestOfficialMimoAPITemplateRemoved(t *testing.T) {
	if entries, keyEnv, err := officialProviderTemplate("mimo-api", "en"); err == nil {
		t.Fatalf("officialProviderTemplate(mimo-api) = entries=%v key=%q nil error, want unknown template", entries, keyEnv)
	}
}

func TestOfficialDeepSeekTemplateDefaultsToRMBPricing(t *testing.T) {
	entries, keyEnv, err := officialProviderTemplate("deepseek", "en")
	if err != nil {
		t.Fatalf("officialProviderTemplate: %v", err)
	}
	if keyEnv != "DEEPSEEK_API_KEY" || len(entries) != 1 {
		t.Fatalf("template = %v/%q, want one DEEPSEEK_API_KEY entry", entries, keyEnv)
	}
	got := entries[0]
	if got.Prices["deepseek-v4-flash"] == nil || got.Prices["deepseek-v4-flash"].Currency != "¥" || got.Prices["deepseek-v4-flash"].Output != 2 {
		t.Fatalf("deepseek-v4-flash price = %+v, want RMB pricing", got.Prices["deepseek-v4-flash"])
	}
	if got.Prices["deepseek-v4-pro"] == nil || got.Prices["deepseek-v4-pro"].Currency != "¥" || got.Prices["deepseek-v4-pro"].Output != 6 {
		t.Fatalf("deepseek-v4-pro price = %+v, want RMB pricing", got.Prices["deepseek-v4-pro"])
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

func TestSetReasoningLanguagePersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetReasoningLanguage("zh"); err != nil {
		t.Fatalf("SetReasoningLanguage: %v", err)
	}

	view := app.Settings()
	if view.Agent.ReasoningLanguage != "zh" {
		t.Fatalf("Settings().Agent.ReasoningLanguage = %q, want zh", view.Agent.ReasoningLanguage)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.ReasoningLanguage != "zh" || cfg.ReasoningLanguage() != "zh" {
		t.Fatalf("saved reasoning language = %q/%q, want zh", cfg.Agent.ReasoningLanguage, cfg.ReasoningLanguage())
	}
}

func TestSetDesktopLanguagePersistsResponseLanguageAndUpdatesLiveTabs(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte("language = \"zh\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	userCtrl := control.New(control.Options{})
	projectCtrl := control.New(control.Options{})
	app.tabs = map[string]*WorkspaceTab{
		"user": {
			ID:          "user",
			Scope:       "global",
			Ctrl:        userCtrl,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		},
		"project": {
			ID:            "project",
			Scope:         "project",
			WorkspaceRoot: projectRoot,
			Ctrl:          projectCtrl,
			Ready:         true,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "user"

	if err := app.SetDesktopLanguage("en"); err != nil {
		t.Fatalf("SetDesktopLanguage: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.DesktopLanguage() != "en" || cfg.Language != "en" {
		t.Fatalf("saved language prefs = desktop:%q response:%q, want en/en", cfg.DesktopLanguage(), cfg.Language)
	}
	got := userCtrl.Compose("解释这个函数")
	if !strings.Contains(got, "<response-language>") || !strings.Contains(got, "use English") {
		t.Fatalf("live controller Compose = %q, want English response language", got)
	}
	projectComposed := projectCtrl.Compose("explain this function")
	if !strings.Contains(projectComposed, "use Simplified Chinese") {
		t.Fatalf("project controller Compose = %q, want project zh response language", projectComposed)
	}
}

func TestSetReasoningLanguageUpdatesLiveTabControllers(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte("[agent]\nreasoning_language = \"en\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	userCtrl := control.New(control.Options{ReasoningLanguage: "auto"})
	projectCtrl := control.New(control.Options{ReasoningLanguage: "auto"})
	app.tabs = map[string]*WorkspaceTab{
		"user": {
			ID:          "user",
			Scope:       "global",
			Ctrl:        userCtrl,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		},
		"project": {
			ID:            "project",
			Scope:         "project",
			WorkspaceRoot: projectRoot,
			Ctrl:          projectCtrl,
			Ready:         true,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "user"

	if err := app.SetReasoningLanguage("zh"); err != nil {
		t.Fatalf("SetReasoningLanguage: %v", err)
	}

	userComposed := userCtrl.Compose("hi")
	if !strings.Contains(userComposed, "简体中文") {
		t.Fatalf("user-level tab Compose = %q, want zh reasoning language", userComposed)
	}
	projectComposed := projectCtrl.Compose("hi")
	if !strings.Contains(projectComposed, "use English") {
		t.Fatalf("project override tab Compose = %q, want en reasoning language", projectComposed)
	}
}

func TestSetAutoPlanUpdatesLiveTabControllers(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	userRunner := &captureTurnRunner{}
	projectRunner := &captureTurnRunner{}
	userCtrl := control.New(control.Options{AutoPlan: "on", Runner: userRunner})
	projectCtrl := control.New(control.Options{AutoPlan: "on", Runner: projectRunner})
	app.tabs = map[string]*WorkspaceTab{
		"user": {
			ID:          "user",
			Scope:       "global",
			Ctrl:        userCtrl,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		},
		"project": {
			ID:            "project",
			Scope:         "project",
			WorkspaceRoot: t.TempDir(),
			Ctrl:          projectCtrl,
			Ready:         true,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "user"

	if err := app.SetAutoPlan("off"); err != nil {
		t.Fatalf("SetAutoPlan: %v", err)
	}

	input := "实现 GitHub issue #2395：\n- 新增配置项\n- 自动判断复杂任务\n- 补测试和文档"
	if err := userCtrl.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := projectCtrl.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(userRunner.inputs) != 1 || strings.HasPrefix(userRunner.inputs[0], control.PlanModeMarker) {
		t.Fatalf("user tab should use updated auto_plan=off, inputs=%q", userRunner.inputs)
	}
	if len(projectRunner.inputs) != 1 || strings.HasPrefix(projectRunner.inputs[0], control.PlanModeMarker) {
		t.Fatalf("project tab without override should use updated auto_plan=off, inputs=%q", projectRunner.inputs)
	}
}

func TestSetAutoPlanIgnoresProjectOverrideForLiveTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "reasonix.toml"), []byte("[agent]\nauto_plan = \"on\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	userRunner := &captureTurnRunner{}
	projectRunner := &captureTurnRunner{}
	userCtrl := control.New(control.Options{AutoPlan: "on", Runner: userRunner})
	projectCtrl := control.New(control.Options{AutoPlan: "on", Runner: projectRunner})
	app.tabs = map[string]*WorkspaceTab{
		"user": {
			ID:          "user",
			Scope:       "global",
			Ctrl:        userCtrl,
			Ready:       true,
			disabledMCP: map[string]ServerView{},
		},
		"project": {
			ID:            "project",
			Scope:         "project",
			WorkspaceRoot: projectRoot,
			Ctrl:          projectCtrl,
			Ready:         true,
			disabledMCP:   map[string]ServerView{},
		},
	}
	app.activeTabID = "user"

	if err := app.SetAutoPlan("off"); err != nil {
		t.Fatalf("SetAutoPlan: %v", err)
	}

	input := "实现 GitHub issue #2395：\n- 新增配置项\n- 自动判断复杂任务\n- 补测试和文档"
	if err := userCtrl.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := projectCtrl.RunTurn(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if len(userRunner.inputs) != 1 || strings.HasPrefix(userRunner.inputs[0], control.PlanModeMarker) {
		t.Fatalf("user tab should use updated auto_plan=off, inputs=%q", userRunner.inputs)
	}
	if len(projectRunner.inputs) != 1 || strings.HasPrefix(projectRunner.inputs[0], control.PlanModeMarker) {
		t.Fatalf("project auto_plan should be ignored, inputs=%q", projectRunner.inputs)
	}
}

func TestSetAutoPlanEnablingClassifierRebuildsActiveController(t *testing.T) {
	isolateDesktopUserDirs(t)

	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Agent.AutoPlan = "off"
	cfg.Agent.AutoPlanClassifier = "deepseek-flash"
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	old := control.New(control.Options{AutoPlan: "off", Label: "old-controller"})
	app.setTestCtrl(old, "deepseek-flash/deepseek-v4-flash")
	defer func() {
		if c := app.activeCtrl(); c != nil {
			c.Close()
		}
	}()

	if err := app.SetAutoPlan("on"); err != nil {
		t.Fatalf("SetAutoPlan(on): %v", err)
	}
	if c := app.activeCtrl(); c == nil {
		t.Fatal("SetAutoPlan should leave a rebuilt controller")
	}
	if c := app.activeCtrl(); c == old {
		t.Fatal("SetAutoPlan should rebuild when enabling a configured classifier")
	}

	got := config.LoadForEdit(config.UserConfigPath())
	if got.Agent.AutoPlan != "on" {
		t.Fatalf("saved auto_plan = %q, want on", got.Agent.AutoPlan)
	}
}

func TestSetReasoningLanguageRejectsBackgroundJobsBeforeSavingConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	app.setTestCtrl(newBackgroundJobController(t, "reasoning-language-job"), "")

	err := app.SetReasoningLanguage("zh")
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("SetReasoningLanguage with background job error = %v, want active-work guard", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.ReasoningLanguage() != "auto" {
		t.Fatalf("reasoning language changed after rejected update: %q", cfg.ReasoningLanguage())
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

func TestSetDefaultToolApprovalModePersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if app.Settings().DefaultToolApprovalMode != control.ToolApprovalAsk {
		t.Fatalf("Settings().DefaultToolApprovalMode = %q, want ask", app.Settings().DefaultToolApprovalMode)
	}
	if err := app.SetDefaultToolApprovalMode(control.ToolApprovalAuto); err != nil {
		t.Fatalf("SetDefaultToolApprovalMode: %v", err)
	}
	view := app.Settings()
	if view.DefaultToolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("Settings().DefaultToolApprovalMode = %q, want auto", view.DefaultToolApprovalMode)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Desktop.DefaultToolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("desktop.default_tool_approval_mode = %q, want auto", cfg.Desktop.DefaultToolApprovalMode)
	}
	if cfg.DesktopDefaultToolApprovalMode() != control.ToolApprovalAuto {
		t.Fatalf("DesktopDefaultToolApprovalMode() = %q, want auto", cfg.DesktopDefaultToolApprovalMode())
	}
}

func TestSetDesktopMetricsDefaultsOnAndPersistsOff(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if !app.Settings().Metrics {
		t.Fatal("Settings().Metrics default = false, want true")
	}
	if err := app.SetDesktopMetrics(false); err != nil {
		t.Fatalf("SetDesktopMetrics: %v", err)
	}
	view := app.Settings()
	if view.Metrics {
		t.Fatal("Settings().Metrics = true, want false")
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Desktop.Metrics == nil || *cfg.Desktop.Metrics {
		t.Fatalf("desktop.metrics = %+v, want false", cfg.Desktop.Metrics)
	}
	if cfg.DesktopMetrics() {
		t.Fatal("DesktopMetrics() = true, want false")
	}
}

func TestSetMemoryCompilerDefaultsOnAndPersistsOff(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if !app.Settings().MemoryCompiler {
		t.Fatal("Settings().MemoryCompiler default = false, want true")
	}
	if err := app.SetMemoryCompilerEnabled(false); err != nil {
		t.Fatalf("SetMemoryCompilerEnabled: %v", err)
	}
	view := app.Settings()
	if view.MemoryCompiler {
		t.Fatal("Settings().MemoryCompiler = true, want false")
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.MemoryCompiler.Enabled == nil || *cfg.Agent.MemoryCompiler.Enabled {
		t.Fatalf("agent.memory_compiler.enabled = %+v, want false", cfg.Agent.MemoryCompiler.Enabled)
	}
	if cfg.MemoryCompilerEnabled() {
		t.Fatal("MemoryCompilerEnabled() = true, want false")
	}
}

type memoryCompilerTargetFake struct {
	calls []bool
}

func (f *memoryCompilerTargetFake) SetMemoryCompilerEnabled(enabled bool) {
	f.calls = append(f.calls, enabled)
}

func TestApplyMemoryCompilerToControllersBroadcastsToAllTargets(t *testing.T) {
	first := &memoryCompilerTargetFake{}
	second := &memoryCompilerTargetFake{}

	applyMemoryCompilerToControllers(false, []memoryCompilerTarget{first, nil, second})

	if !reflect.DeepEqual(first.calls, []bool{false}) {
		t.Fatalf("first calls = %v, want [false]", first.calls)
	}
	if !reflect.DeepEqual(second.calls, []bool{false}) {
		t.Fatalf("second calls = %v, want [false]", second.calls)
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

func TestSaveHooksSettingsDecodesLegacyEncodedGlobalSettings(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := hook.GlobalSettingsPath("")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"label":"中文","hooks":{"Stop":[{"command":"echo 旧"}]}}`
	if err := os.WriteFile(path, fileencoding.Encode(legacy, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	before := app.HooksSettings("global")
	if len(before.Hooks) != 1 || before.Hooks[0].Command != "echo 旧" {
		t.Fatalf("HooksSettings before save = %+v, want decoded legacy hook", before.Hooks)
	}
	if err := app.SaveHooksSettings("global", []HookConfigView{{
		Event:   string(hook.PreToolUse),
		Command: "echo 新",
	}}); err != nil {
		t.Fatalf("SaveHooksSettings: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("saved settings should be valid UTF-8 JSON: %v", err)
	}
	if string(raw["label"]) != `"中文"` {
		t.Fatalf("label key was not preserved after decoding legacy settings: %s", raw["label"])
	}
	view := app.HooksSettings("global")
	if len(view.Hooks) != 1 || view.Hooks[0].Command != "echo 新" {
		t.Fatalf("HooksSettings after save = %+v, want new decoded hook", view.Hooks)
	}
}

func TestSaveHooksSettingsNormalizesQuotedNodeEvalHookCommand(t *testing.T) {
	isolateDesktopUserDirs(t)
	script := "const payload = JSON.parse(require('fs').readFileSync(0, 'utf8')); console.log(payload.toolName)"
	bad := `node -e "\"` + script + `\""`
	want := hook.NormalizeCommand(bad)
	if want == bad {
		t.Fatal("test command did not normalize")
	}

	app := NewApp()
	if err := app.SaveHooksSettings("global", []HookConfigView{{
		Event:   string(hook.PreToolUse),
		Match:   "bash",
		Command: bad,
	}}); err != nil {
		t.Fatalf("SaveHooksSettings: %v", err)
	}

	view := app.HooksSettings("global")
	if len(view.Hooks) != 1 || view.Hooks[0].Command != want {
		t.Fatalf("HooksSettings = %+v, want normalized command %q", view.Hooks, want)
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

// TestLoadDesktopUserConfigForViewDoesNotPersistLegacyProviderAccess locks the
// read-path contract: loading a legacy-form config (configured providers but
// no declared desktop.provider_access) through the View helpers returns a
// normalized in-memory view while leaving the file bytes untouched. The
// on-disk migration only happens once a locked write path runs.
func TestLoadDesktopUserConfigForViewDoesNotPersistLegacyProviderAccess(t *testing.T) {
	isolateDesktopUserDirs(t)
	userPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "default_model = \"local/m1\"\n\n[[providers]]\nname = \"local\"\nbase_url = \"http://127.0.0.1:9999/v1\"\nmodels = [\"m1\"]\n"
	if err := os.WriteFile(userPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	for name, load := range map[string]func() (*config.Config, string, error){
		"view":                  app.loadDesktopUserConfigForView,
		"view-with-credentials": app.loadDesktopUserConfigForViewWithCredentials,
	} {
		cfg, _, err := load()
		if err != nil {
			t.Fatalf("%s load: %v", name, err)
		}
		if len(cfg.Desktop.ProviderAccess) == 0 {
			t.Fatalf("%s load should normalize legacy provider access in memory", name)
		}
		raw, err := os.ReadFile(userPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != legacy {
			t.Fatalf("%s load must not rewrite the user config, got:\n%s", name, raw)
		}
	}

	// The first locked write path persists the pending migration.
	if err := app.applyConfigOnly(func(*config.Config) error { return nil }); err != nil {
		t.Fatalf("applyConfigOnly: %v", err)
	}
	if !configDeclaresProviderAccess(userPath) {
		t.Fatal("locked write path should persist the provider access migration to disk")
	}
	migrated := config.LoadForEditWithoutCredentials(userPath)
	if len(migrated.Desktop.ProviderAccess) == 0 {
		t.Fatalf("migrated config lost provider access: %v", migrated.Desktop.ProviderAccess)
	}
}

// TestLoadDesktopUserConfigViewKeepsLegacyBotConfigMigrationInMemory locks the
// same contract for the legacy bot-config migration: read paths (including the
// bot runtime's credential-loading view) see the merged bot config in memory
// without any file being written; the locked write path performs the on-disk
// migration.
func TestLoadDesktopUserConfigViewKeepsLegacyBotConfigMigrationInMemory(t *testing.T) {
	isolateDesktopUserDirs(t)
	userPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	userBody := "default_model = \"local/m1\"\n"
	if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyRoot := t.TempDir()
	legacyPath := filepath.Join(legacyRoot, "reasonix.toml")
	legacyBody := "[bot]\nenabled = true\nmodel = \"local/m1\"\n"
	if err := os.WriteFile(legacyPath, []byte(legacyBody), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"t": {ID: "t", Scope: "project", WorkspaceRoot: legacyRoot, Ready: true},
	}
	app.activeTabID = "t"

	assertFilesUntouched := func(step string) {
		t.Helper()
		rawUser, err := os.ReadFile(userPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(rawUser) != userBody {
			t.Fatalf("%s must not rewrite the user config, got:\n%s", step, rawUser)
		}
		rawLegacy, err := os.ReadFile(legacyPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(rawLegacy) != legacyBody {
			t.Fatalf("%s must not rewrite the legacy config, got:\n%s", step, rawLegacy)
		}
	}

	cfg, _, err := app.loadDesktopUserConfigForView()
	if err != nil {
		t.Fatalf("loadDesktopUserConfigForView: %v", err)
	}
	if !cfg.Bot.Enabled {
		t.Fatal("view load should merge the legacy bot config in memory")
	}
	assertFilesUntouched("loadDesktopUserConfigForView")

	botCfg, err := app.loadDesktopBotConfig()
	if err != nil {
		t.Fatalf("loadDesktopBotConfig: %v", err)
	}
	if !botCfg.Bot.Enabled {
		t.Fatal("bot runtime load should see the merged legacy bot config")
	}
	assertFilesUntouched("loadDesktopBotConfig")

	// The first locked write path migrates the bot config into the user file.
	if err := app.applyConfigOnly(func(*config.Config) error { return nil }); err != nil {
		t.Fatalf("applyConfigOnly: %v", err)
	}
	migrated := config.LoadForEditWithoutCredentials(userPath)
	if !migrated.Bot.Enabled {
		t.Fatal("locked write path should persist the legacy bot config migration")
	}
	rawLegacy, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawLegacy) != legacyBody {
		t.Fatalf("migration must not rewrite the legacy config, got:\n%s", rawLegacy)
	}
}

func TestSetBotSettingsPreservesFeishuOutboundMediaRoots(t *testing.T) {
	isolateDesktopUserDirs(t)
	root := t.TempDir()
	cfg := config.Default()
	cfg.Bot.Feishu.OutboundMediaRoots = []string{root}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save initial config: %v", err)
	}

	app := NewApp()
	view := botSettingsView(cfg.Bot)
	view.QueueCap++
	if err := app.SetBotSettings(view); err != nil {
		t.Fatalf("SetBotSettings: %v", err)
	}

	got := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if !reflect.DeepEqual(got.Bot.Feishu.OutboundMediaRoots, []string{root}) {
		t.Fatalf("outbound media roots = %v, want preserved %q", got.Bot.Feishu.OutboundMediaRoots, root)
	}
}
