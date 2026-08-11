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
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
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

func TestProviderModelOverridesPreservePerModelContextWindow(t *testing.T) {
	overrides := map[string]config.ProviderModelOverride{
		"short-model": {ContextWindow: 32_768},
		"long-model":  {ContextWindow: 1_000_000},
		"removed":     {ContextWindow: 8_192},
	}
	models := []string{"short-model", "long-model"}

	view := providerModelOverridesForView(overrides, models)
	if len(view) != 2 || view[0].Model != "long-model" || view[0].ContextWindow != 1_000_000 || view[1].Model != "short-model" || view[1].ContextWindow != 32_768 {
		t.Fatalf("provider model override view = %+v", view)
	}

	view[0].ContextWindow = -1
	saved := providerModelOverridesForSave(view, models)
	if _, ok := saved["long-model"]; ok {
		t.Fatalf("non-positive context-only override should be removed: %+v", saved)
	}
	if got := saved["short-model"].ContextWindow; got != 32_768 {
		t.Fatalf("saved short-model context window = %d, want 32768", got)
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

func TestProviderViewFromEntryReportsOfficialDeepSeekVisionUnsupported(t *testing.T) {
	p := config.ProviderEntry{
		Name:         "deepseek",
		Kind:         "openai",
		BaseURL:      "https://api.deepseek.com",
		Models:       []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		VisionModels: []string{"deepseek-v4-flash", "deepseek-v4-pro"},
	}
	view := providerViewFromEntry(p, true, true)
	if view.VisionCapability != "unsupported" {
		t.Fatalf("VisionCapability = %q, want unsupported", view.VisionCapability)
	}
	if !reflect.DeepEqual(view.VisionModels, p.VisionModels) {
		t.Fatalf("ProviderView should preserve stale metadata for a lossless settings round trip: got %v want %v", view.VisionModels, p.VisionModels)
	}
	resolved := p
	resolved.Model = "deepseek-v4-pro"
	if config.EffectiveVision(&resolved) {
		t.Fatal("preserved stale settings metadata must not enable runtime image input")
	}
}

func TestProviderViewFromEntryOffersOnlySafeDeepSeekProtocolUpgrade(t *testing.T) {
	legacy := config.ProviderEntry{
		Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com",
		Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY",
	}
	if view := providerViewFromEntry(legacy, true, true); !view.RecommendedUpgradeAvailable {
		t.Fatal("standard official OpenAI entry did not offer the recommended protocol upgrade")
	}

	proxy := legacy
	proxy.BaseURL = "https://deepseek-proxy.example/v1"
	if view := providerViewFromEntry(proxy, false, true); view.RecommendedUpgradeAvailable {
		t.Fatal("proxy entry unexpectedly offered the official protocol upgrade")
	}

	anthropic := legacy
	anthropic.Kind = "anthropic"
	anthropic.BaseURL = "https://api.deepseek.com/anthropic"
	if view := providerViewFromEntry(anthropic, true, true); view.RecommendedUpgradeAvailable {
		t.Fatal("already-upgraded entry still offered the protocol upgrade")
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

func TestProviderViewFromEntryUsesEffectiveWebSearch(t *testing.T) {
	view := providerViewFromEntry(config.ProviderEntry{
		Name:    "deepseek-responses",
		Kind:    "responses",
		BaseURL: "https://api.deepseek.com",
	}, false, true)
	if !view.WebSearch {
		t.Fatal("official DeepSeek Responses omission did not default web search on")
	}

	disabled := false
	explicitOff := providerViewFromEntry(config.ProviderEntry{
		Name:      "deepseek-responses",
		Kind:      "responses",
		BaseURL:   "https://api.deepseek.com",
		WebSearch: &disabled,
	}, false, true)
	if explicitOff.WebSearch {
		t.Fatal("explicit web_search=false was not preserved")
	}

	custom := providerViewFromEntry(config.ProviderEntry{
		Name:    "custom-responses",
		Kind:    "responses",
		BaseURL: "https://gateway.example/v1",
	}, false, true)
	if custom.WebSearch {
		t.Fatal("custom provider unexpectedly enabled web search")
	}
	if custom.ServerWebSearchCapability {
		t.Fatal("unverified custom Responses provider unexpectedly exposed server web search in Settings")
	}

	openAI := providerViewFromEntry(config.ProviderEntry{
		Name:      "custom-openai",
		Kind:      "openai",
		BaseURL:   "https://gateway.example/v1",
		WebSearch: func() *bool { enabled := true; return &enabled }(),
	}, false, true)
	if openAI.ServerWebSearchCapability || openAI.WebSearch {
		t.Fatal("OpenAI Chat Completions unexpectedly reported server web-search support")
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

func TestFetchAllProviderModelsOmitsFailuresWithoutJSONNulls(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   []map[string]string{{"id": "model-a", "object": "model"}},
		})
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"temporary"}`, http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	got := NewApp().FetchAllProviderModels([]ProviderView{
		{Name: "good", Kind: "openai", BaseURL: good.URL},
		{Name: "bad", Kind: "openai", BaseURL: bad.URL},
	})
	if want := []string{"model-a"}; !reflect.DeepEqual(got["good"], want) {
		t.Fatalf("good provider models = %v, want %v", got["good"], want)
	}
	if _, ok := got["bad"]; ok {
		t.Fatalf("failed provider unexpectedly present: %#v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal batch result: %v", err)
	}
	if strings.Contains(string(raw), "null") {
		t.Fatalf("batch result contains JSON null: %s", raw)
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

func TestSaveProviderModelCatalogsPersistsFreshBatchAtomically(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	providers := []ProviderView{
		{Name: "batch-a", Kind: "openai", BaseURL: "https://a.example.com/v1", Models: []string{"model-a"}, APIKeyEnv: "BATCH_A_API_KEY", Headers: map[string]string{"X-Tenant": "a"}},
		{Name: "batch-b", Kind: "openai", BaseURL: "https://b.example.com/v1", Models: []string{"model-b"}, APIKeyEnv: "BATCH_B_API_KEY"},
	}
	for _, provider := range providers {
		if err := app.SaveProvider(provider); err != nil {
			t.Fatalf("SaveProvider(%s): %v", provider.Name, err)
		}
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	a, _ := cfg.Provider("batch-a")
	b, _ := cfg.Provider("batch-b")
	updates := []ProviderModelCatalogUpdate{
		{Name: "batch-a", ExpectedFingerprint: providerModelCatalogFingerprint(*a), Models: []string{"model-a", "model-a-new"}, Default: "model-a-new"},
		{Name: "batch-b", ExpectedFingerprint: providerModelCatalogFingerprint(*b), Models: []string{"model-b", "model-b-new"}, Default: "model-b-new"},
	}
	applied, err := app.SaveProviderModelCatalogs(updates)
	if err != nil {
		t.Fatalf("SaveProviderModelCatalogs: %v", err)
	}
	if !reflect.DeepEqual(applied, []string{"batch-a", "batch-b"}) {
		t.Fatalf("applied = %v, want both providers", applied)
	}

	cfg = config.LoadForEdit(config.UserConfigPath())
	a, _ = cfg.Provider("batch-a")
	b, _ = cfg.Provider("batch-b")
	if a.DefaultModel() != "model-a-new" || b.DefaultModel() != "model-b-new" {
		t.Fatalf("catalog defaults = %q/%q, want model-a-new/model-b-new", a.DefaultModel(), b.DefaultModel())
	}
	if a.BaseURL != providers[0].BaseURL || a.APIKeyEnv != providers[0].APIKeyEnv || a.Headers["X-Tenant"] != "a" {
		t.Fatalf("narrow catalog update changed provider identity: %+v", *a)
	}

	aFingerprint := providerModelCatalogFingerprint(*a)
	bFingerprint := providerModelCatalogFingerprint(*b)
	if _, err := app.SaveProviderModelCatalogs([]ProviderModelCatalogUpdate{
		{Name: "batch-a", ExpectedFingerprint: aFingerprint, Models: []string{"must-not-persist"}},
		{Name: "batch-b", ExpectedFingerprint: bFingerprint, Models: []string{"text-embedding-3-small"}},
	}); err == nil {
		t.Fatal("SaveProviderModelCatalogs invalid batch returned nil error")
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	a, _ = cfg.Provider("batch-a")
	if a.DefaultModel() == "must-not-persist" {
		t.Fatal("SaveProviderModelCatalogs persisted a partial invalid batch")
	}
}

func TestSaveProviderModelCatalogsRejectsStaleCompletion(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name: "race-provider", Kind: "openai", BaseURL: "https://old.example.com/v1",
		Models: []string{"old-model"}, Default: "old-model", APIKeyEnv: "OLD_API_KEY",
		Headers: map[string]string{"X-Version": "old"},
	}); err != nil {
		t.Fatalf("SaveProvider(old): %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	old, _ := cfg.Provider("race-provider")
	oldFingerprint := providerModelCatalogFingerprint(*old)

	started := make(chan struct{})
	release := make(chan struct{})
	type result struct {
		applied []string
		err     error
	}
	done := make(chan result, 1)
	go func() {
		close(started)
		<-release
		applied, err := app.SaveProviderModelCatalogs([]ProviderModelCatalogUpdate{{
			Name: "race-provider", ExpectedFingerprint: oldFingerprint,
			Models: []string{"old-model", "stale-fetched-model"}, Default: "stale-fetched-model",
		}})
		done <- result{applied: applied, err: err}
	}()
	<-started

	if err := app.SaveProvider(ProviderView{
		Name: "race-provider", Kind: "openai", BaseURL: "https://new.example.com/v1",
		Models: []string{"new-model"}, Default: "new-model", APIKeyEnv: "NEW_API_KEY",
		Headers: map[string]string{"X-Version": "new"},
	}); err != nil {
		t.Fatalf("SaveProvider(new): %v", err)
	}
	close(release)
	gotResult := <-done
	if gotResult.err != nil {
		t.Fatalf("stale SaveProviderModelCatalogs: %v", gotResult.err)
	}
	if len(gotResult.applied) != 0 {
		t.Fatalf("stale update applied providers %v, want none", gotResult.applied)
	}

	cfg = config.LoadForEdit(config.UserConfigPath())
	got, _ := cfg.Provider("race-provider")
	if got.BaseURL != "https://new.example.com/v1" || got.APIKeyEnv != "NEW_API_KEY" || got.Headers["X-Version"] != "new" {
		t.Fatalf("stale completion overwrote provider identity: %+v", *got)
	}
	if !reflect.DeepEqual(got.ChatModelList(), []string{"new-model"}) || got.DefaultModel() != "new-model" {
		t.Fatalf("stale completion overwrote model selection: models=%v default=%q", got.ChatModelList(), got.DefaultModel())
	}
}

func TestSaveProviderModelCatalogsRejectsStaleCredentialSnapshot(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name: "credential-race", Kind: "openai", BaseURL: "https://credential.example.com/v1",
		Models: []string{"current-model"}, APIKeyEnv: "CREDENTIAL_RACE_API_KEY",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}
	if _, err := app.SaveProviderKey("CREDENTIAL_RACE_API_KEY", "old-key"); err != nil {
		t.Fatalf("SaveProviderKey(old): %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	provider, _ := cfg.Provider("credential-race")
	oldFingerprint := providerModelCatalogFingerprint(*provider)

	if _, err := app.SaveProviderKey("CREDENTIAL_RACE_API_KEY", "new-key-with-different-length"); err != nil {
		t.Fatalf("SaveProviderKey(new): %v", err)
	}
	applied, err := app.SaveProviderModelCatalogs([]ProviderModelCatalogUpdate{{
		Name: "credential-race", ExpectedFingerprint: oldFingerprint,
		Models: []string{"current-model", "stale-key-model"}, Default: "stale-key-model",
	}})
	if err != nil {
		t.Fatalf("SaveProviderModelCatalogs: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("credential-stale update applied providers %v, want none", applied)
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	provider, _ = cfg.Provider("credential-race")
	if !reflect.DeepEqual(provider.ChatModelList(), []string{"current-model"}) {
		t.Fatalf("credential-stale update overwrote models: %v", provider.ChatModelList())
	}
}

func TestSaveProviderModelCatalogsRejectsOverlappingCredentialRotation(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	const keyEnv = "CREDENTIAL_OVERLAP_API_KEY"
	if err := app.SaveProvider(ProviderView{
		Name: "credential-overlap", Kind: "openai", BaseURL: "https://credential.example.com/v1",
		Models: []string{"current-model"}, APIKeyEnv: keyEnv,
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}
	if _, err := app.SaveProviderKey(keyEnv, "old-key"); err != nil {
		t.Fatalf("SaveProviderKey(old): %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	provider, _ := cfg.Provider("credential-overlap")
	oldFingerprint := providerModelCatalogFingerprint(*provider)

	snapshotRead := make(chan struct{})
	releaseApply := make(chan struct{})
	app.providerCatalogBeforeCredentialLockHook = func(string) {
		close(snapshotRead)
		<-releaseApply
	}
	type result struct {
		applied []string
		err     error
	}
	catalogDone := make(chan result, 1)
	go func() {
		applied, err := app.SaveProviderModelCatalogs([]ProviderModelCatalogUpdate{{
			Name: "credential-overlap", ExpectedFingerprint: oldFingerprint,
			Models: []string{"current-model", "stale-key-model"}, Default: "stale-key-model",
		}})
		catalogDone <- result{applied: applied, err: err}
	}()
	<-snapshotRead

	// Keep the replacement the same length as the old value: revision safety
	// must come from credential contents and locking, not size or mtime luck.
	if _, err := app.SaveProviderKey(keyEnv, "new-key"); err != nil {
		t.Fatalf("SaveProviderKey(new): %v", err)
	}
	close(releaseApply)
	gotResult := <-catalogDone
	if gotResult.err != nil {
		t.Fatalf("SaveProviderModelCatalogs: %v", gotResult.err)
	}
	if len(gotResult.applied) != 0 {
		t.Fatalf("credential-stale update applied providers %v, want none", gotResult.applied)
	}

	cfg = config.LoadForEdit(config.UserConfigPath())
	provider, _ = cfg.Provider("credential-overlap")
	if !reflect.DeepEqual(provider.ChatModelList(), []string{"current-model"}) {
		t.Fatalf("overlapping credential rotation persisted stale models: %v", provider.ChatModelList())
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

func TestSaveProviderPersistsAndMirrorsCustomEndpointURLs(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SaveProvider(ProviderView{
		Name:       "sub2api",
		Kind:       "openai",
		BaseURL:    "https://proxy.example.com/v1",
		ChatURL:    " https://legacy.example.com/chat/completions/ ",
		RequestURL: " https://proxy.example.com/custom/chat/completions/?token=1 ",
		ModelsURL:  " https://proxy.example.com/v1/models ",
		Models:     []string{"model-a"},
		Default:    "model-a",
		APIKeyEnv:  "SUB2API_KEY",
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("sub2api")
	if !ok {
		t.Fatal("saved provider not found")
	}
	if got.ChatURL != "https://proxy.example.com/custom/chat/completions/?token=1" {
		t.Fatalf("saved chat_url = %q", got.ChatURL)
	}
	if got.RequestURL != "https://proxy.example.com/custom/chat/completions/?token=1" {
		t.Fatalf("saved request_url = %q", got.RequestURL)
	}
	if got.ModelsURL != "https://proxy.example.com/v1/models" {
		t.Fatalf("saved models_url = %q", got.ModelsURL)
	}

	view := app.Settings()
	for _, provider := range view.Providers {
		if provider.Name != "sub2api" {
			continue
		}
		if provider.ChatURL != "https://proxy.example.com/custom/chat/completions/?token=1" {
			t.Fatalf("Settings chatUrl = %q", provider.ChatURL)
		}
		if provider.RequestURL != "https://proxy.example.com/custom/chat/completions/?token=1" {
			t.Fatalf("Settings requestUrl = %q", provider.RequestURL)
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

func TestSaveProviderPersistsWebSearchOn(t *testing.T) {
	isolateDesktopUserDirs(t)

	if err := NewApp().SaveProvider(ProviderView{
		Name:      "deepseek-responses",
		Kind:      "responses",
		BaseURL:   "https://api.deepseek.com",
		Models:    []string{"deepseek-v4-flash"},
		Default:   "deepseek-v4-flash",
		WebSearch: true,
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("deepseek-responses")
	if !ok || got.WebSearch == nil || !*got.WebSearch {
		t.Fatalf("saved provider = %+v, found=%v; want web_search=true", got, ok)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(raw), "web_search  = true") {
		t.Fatalf("saved config did not persist web_search:\n%s", raw)
	}
}

func TestSaveProviderPersistsExplicitWebSearchOff(t *testing.T) {
	isolateDesktopUserDirs(t)

	if err := NewApp().SaveProvider(ProviderView{
		Name:      "deepseek-responses",
		Kind:      "responses",
		BaseURL:   "https://api.deepseek.com",
		Models:    []string{"deepseek-v4-flash"},
		Default:   "deepseek-v4-flash",
		WebSearch: false,
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := cfg.Provider("deepseek-responses")
	if !ok || got.WebSearch == nil || *got.WebSearch || config.EffectiveWebSearch(got) {
		t.Fatalf("saved provider = %+v, found=%v; want explicit web_search=false", got, ok)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(raw), "web_search  = false") {
		t.Fatalf("saved config did not persist web_search=false:\n%s", raw)
	}
}

func TestSetProviderWebSearchUpdatesGroupedDeepSeekAliasesAtomically(t *testing.T) {
	isolateDesktopUserDirs(t)

	enabled := true
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Desktop.ProviderAccess = []string{"deepseek-flash", "deepseek-pro"}
	cfg.Providers = []config.ProviderEntry{
		{
			Name: "deepseek-flash", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
			Models: []string{"deepseek-v4-flash"}, Headers: map[string]string{"X-Route": "flash"}, WebSearch: &enabled,
		},
		{
			Name: "deepseek-pro", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
			Models: []string{"deepseek-v4-pro"}, Headers: map[string]string{"X-Route": "pro"}, WebSearch: &enabled,
		},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	if err := NewApp().SetProviderWebSearch([]string{"deepseek-flash", "deepseek-pro", "deepseek-flash"}, false); err != nil {
		t.Fatalf("SetProviderWebSearch: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	for _, name := range []string{"deepseek-flash", "deepseek-pro"} {
		entry, ok := got.Provider(name)
		if !ok || entry.WebSearch == nil || *entry.WebSearch {
			t.Fatalf("provider %q = %+v, found=%v; want explicit web_search=false", name, entry, ok)
		}
	}
	if flash, _ := got.Provider("deepseek-flash"); flash.Headers["X-Route"] != "flash" {
		t.Fatalf("Flash custom transport fields changed: %+v", flash)
	}
	if pro, _ := got.Provider("deepseek-pro"); pro.Headers["X-Route"] != "pro" {
		t.Fatalf("Pro custom transport fields changed: %+v", pro)
	}
}

func TestSetProviderWebSearchRejectsWholeGroupBeforeWriting(t *testing.T) {
	isolateDesktopUserDirs(t)

	enabled := true
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{
		{Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", Models: []string{"deepseek-v4-flash"}, WebSearch: &enabled},
		{Name: "proxy", Kind: "anthropic", BaseURL: "https://gateway.example/anthropic", Models: []string{"custom-model"}, WebSearch: &enabled},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	if err := NewApp().SetProviderWebSearch([]string{"deepseek", "proxy"}, false); err == nil {
		t.Fatal("SetProviderWebSearch accepted an unverified endpoint")
	}

	got := config.LoadForEdit(config.UserConfigPath())
	entry, ok := got.Provider("deepseek")
	if !ok || entry.WebSearch == nil || !*entry.WebSearch {
		t.Fatalf("official provider was partially updated after group rejection: %+v, found=%v", entry, ok)
	}
}

func TestSetProviderWebSearchRebuildsEveryVisibleRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	enabled := true
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = []config.ProviderEntry{{
		Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
		Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash",
		APIKeyEnv: "DEEPSEEK_API_KEY", WebSearch: &enabled,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	newTab := func(id string) (*WorkspaceTab, *blockingSnapshotCtrl) {
		old := newBlockingSnapshotCtrl(control.New(control.Options{Label: cfg.DefaultModel, Sink: event.Discard}))
		close(old.releaseSnapshot)
		tab := &WorkspaceTab{
			ID: id, Scope: "global", Ready: true, Ctrl: old,
			model: cfg.DefaultModel, Label: cfg.DefaultModel,
			sink: &tabEventSink{tabID: id, app: app}, disabledMCP: map[string]ServerView{},
		}
		return tab, old
	}
	first, oldFirst := newTab("first")
	second, oldSecond := newTab("second")
	app.tabs = map[string]*WorkspaceTab{first.ID: first, second.ID: second}
	app.tabOrder = []string{first.ID, second.ID}
	app.activeTabID = first.ID
	t.Cleanup(func() {
		for _, tab := range []*WorkspaceTab{first, second} {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
			tab.releaseSessionLease()
		}
	})

	if err := app.SetProviderWebSearch([]string{"deepseek"}, false); err != nil {
		t.Fatalf("SetProviderWebSearch: %v", err)
	}
	if first.Ctrl == oldFirst || second.Ctrl == oldSecond || oldFirst.closeCount.Load() != 1 || oldSecond.closeCount.Load() != 1 {
		t.Fatalf("web-search refresh: first=%T/%d second=%T/%d; want both replaced once", first.Ctrl, oldFirst.closeCount.Load(), second.Ctrl, oldSecond.closeCount.Load())
	}
	got := config.LoadForEdit(config.UserConfigPath())
	provider, ok := got.Provider("deepseek")
	if !ok || provider.WebSearch == nil || *provider.WebSearch {
		t.Fatalf("persisted DeepSeek web_search = %+v, want false", provider)
	}
}

func TestSetProviderWebSearchRejectsDetachedRuntimeBeforeMutation(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	enabled := true
	cfg := config.Default()
	cfg.DefaultModel = "deepseek/deepseek-v4-flash"
	cfg.Desktop.ProviderAccess = []string{"deepseek"}
	cfg.Providers = []config.ProviderEntry{{
		Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic",
		Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY", WebSearch: &enabled,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.ctx = context.Background()
	detachedCtrl := control.New(control.Options{Label: cfg.DefaultModel, Sink: event.Discard})
	detached := &WorkspaceTab{ID: "detached", Scope: "global", Ctrl: detachedCtrl, model: cfg.DefaultModel}
	app.detachedSessions = map[string]*WorkspaceTab{detached.ID: detached}
	t.Cleanup(detachedCtrl.Close)

	err := app.SetProviderWebSearch([]string{"deepseek"}, false)
	if err == nil || !strings.Contains(err.Error(), "background session is still open") {
		t.Fatalf("SetProviderWebSearch error = %v, want detached-runtime guard", err)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	provider, ok := got.Provider("deepseek")
	if !ok || provider.WebSearch == nil || !*provider.WebSearch {
		t.Fatalf("rejected web-search mutation changed provider: %+v", provider)
	}
}

func TestSaveProviderPreservesHiddenCustomWebSearchOverride(t *testing.T) {
	isolateDesktopUserDirs(t)

	enabled := true
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{
		Name:      "custom-anthropic",
		Kind:      "anthropic",
		BaseURL:   "https://gateway.example/anthropic",
		Models:    []string{"custom-model"},
		WebSearch: &enabled,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	if err := NewApp().SaveProvider(ProviderView{
		Name:          "custom-anthropic",
		Kind:          "anthropic",
		BaseURL:       "https://gateway.example/anthropic",
		Models:        []string{"custom-model"},
		Default:       "custom-model",
		ContextWindow: 200_000,
		WebSearch:     false, // The hidden Settings control must not overwrite advanced TOML.
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	gotCfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := gotCfg.Provider("custom-anthropic")
	if !ok || got.WebSearch == nil || !*got.WebSearch || !config.EffectiveWebSearch(got) {
		t.Fatalf("saved provider = %+v, found=%v; want preserved advanced web_search=true", got, ok)
	}
}

func TestSaveProviderDoesNotCarryOfficialWebSearchToCustomEndpoint(t *testing.T) {
	isolateDesktopUserDirs(t)

	enabled := true
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = []config.ProviderEntry{{
		Name:      "deepseek-customized",
		Kind:      "anthropic",
		BaseURL:   "https://api.deepseek.com/anthropic",
		Models:    []string{"deepseek-v4-flash"},
		WebSearch: &enabled,
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	if err := NewApp().SaveProvider(ProviderView{
		Name:      "deepseek-customized",
		Kind:      "anthropic",
		BaseURL:   "https://gateway.example/anthropic",
		Models:    []string{"deepseek-v4-flash"},
		Default:   "deepseek-v4-flash",
		WebSearch: false,
	}); err != nil {
		t.Fatalf("SaveProvider: %v", err)
	}

	gotCfg := config.LoadForEdit(config.UserConfigPath())
	got, ok := gotCfg.Provider("deepseek-customized")
	if !ok || got.WebSearch != nil || config.EffectiveWebSearch(got) {
		t.Fatalf("saved provider = %+v, found=%v; want official web search cleared after endpoint change", got, ok)
	}
}

func TestUpgradeDeepSeekProviderAccessPreservesCustomizedFields(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `default_model = "deepseek-flash/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
vision = true
chat_url = "https://api.deepseek.com/anthropic/v1/messages"
models_url = "https://api.deepseek.com/models"
headers = { X-Trace = "keep" }
extra_body = { route = "keep" }
auth_header = true
thinking = "enabled"
web_search = true
no_proxy = true
cache_ttl_minutes = 17
context_window = 900000
max_output_tokens = 111111
supported_efforts = ["disabled", "low", "high"]
default_effort = "low"
price = { cache_hit = 0.1, input = 1.25, output = 2.25, currency = "T" }
future_capability = "keep"

[[providers]]
name = "deepseek-pro"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-pro"
api_key_env = "DEEPSEEK_API_KEY"
chat_url = "https://api.deepseek.com/anthropic/v1/messages"
models_url = "https://api.deepseek.com/models"
headers = { X-Trace = "keep" }
extra_body = { route = "keep" }
auth_header = true
thinking = "enabled"
web_search = true
no_proxy = true
cache_ttl_minutes = 17
context_window = 800000
max_output_tokens = 222222
reasoning_protocol = "none"
supported_efforts = ["disabled", "high", "max"]
default_effort = "max"
price = { cache_hit = 0.2, input = 3.75, output = 6.75, currency = "T" }
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewApp().UpgradeDeepSeekProviderAccess("deepseek"); err != nil {
		t.Fatalf("UpgradeDeepSeekProviderAccess: %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	if strings.Count(text, `kind = "anthropic"`) != 2 ||
		strings.Count(text, `base_url = "https://api.deepseek.com/anthropic"`) != 2 {
		t.Fatalf("provider family was not upgraded:\n%s", text)
	}
	for _, preserved := range []string{`vision = true`, `headers = { X-Trace = "keep" }`, `extra_body = { route = "keep" }`, `future_capability = "keep"`, `reasoning_protocol = "none"`} {
		if !strings.Contains(text, preserved) {
			t.Errorf("upgrade dropped %q:\n%s", preserved, text)
		}
	}

	cfg, err := config.LoadForRootReadOnly(t.TempDir())
	if err != nil {
		t.Fatalf("load upgraded config: %v", err)
	}
	if got := cfg.Desktop.ProviderAccess; len(got) != 1 || got[0] != "deepseek" {
		t.Fatalf("provider_access = %v, want one canonical DeepSeek entry", got)
	}
	canonical, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("effective canonical DeepSeek provider missing after upgrade")
	}
	if canonical.ChatURL != "https://api.deepseek.com/anthropic/v1/messages" ||
		canonical.ModelsURL != "https://api.deepseek.com/models" ||
		canonical.Headers["X-Trace"] != "keep" || canonical.ExtraBody["route"] != "keep" ||
		!canonical.AuthHeader || !canonical.NoProxy || canonical.CacheTTLMinutes != 17 {
		t.Fatalf("canonical transport fields were not preserved: %+v", canonical)
	}
	flash, ok := cfg.ResolveModel("deepseek/deepseek-v4-flash")
	if !ok {
		t.Fatal("canonical DeepSeek Flash model did not resolve")
	}
	if flash.ContextWindow != 900000 || flash.MaxOutputTokens != 111111 || flash.DefaultEffort != "low" ||
		flash.Price == nil || flash.Price.Output != 2.25 {
		t.Fatalf("Flash model fields were not preserved: %+v", flash)
	}
	if config.EffectiveVision(flash) {
		t.Fatal("preserved stale Flash vision metadata must not enable images on the official DeepSeek endpoint")
	}
	flashOverride := canonical.ModelOverrides["deepseek-v4-flash"]
	if flashOverride.Vision == nil || !*flashOverride.Vision {
		t.Fatalf("Flash vision metadata was dropped instead of being safely ignored: %+v", flashOverride)
	}
	pro, ok := cfg.ResolveModel("deepseek/deepseek-v4-pro")
	if !ok {
		t.Fatal("canonical DeepSeek Pro model did not resolve")
	}
	if pro.ContextWindow != 800000 || pro.MaxOutputTokens != 222222 || pro.ReasoningProtocol != "none" ||
		pro.DefaultEffort != "max" || pro.Price == nil || pro.Price.Output != 6.75 {
		t.Fatalf("Pro model fields were not preserved: %+v", pro)
	}
}

func TestUpgradeDeepSeekProviderAccessSerializesRuntimeMutation(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.runtimeRebuildMu.Lock()
	rebuildLocked := true
	defer func() {
		if rebuildLocked {
			app.runtimeRebuildMu.Unlock()
		}
	}()
	entered := make(chan struct{})
	app.runtimeMutationBeforeLockHook = func(operation string) {
		if operation == "upgrade-deepseek-provider" {
			close(entered)
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := app.UpgradeDeepSeekProviderAccess("deepseek")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("protocol upgrade did not reach the runtime mutation barrier")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), `kind = "openai"`) {
		t.Fatalf("protocol changed before acquiring the runtime mutation lock:\n%s", before)
	}

	app.runtimeRebuildMu.Unlock()
	rebuildLocked = false
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("UpgradeDeepSeekProviderAccess: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("protocol upgrade did not complete after releasing the runtime mutation lock")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `kind = "anthropic"`) {
		t.Fatalf("protocol was not upgraded after acquiring the runtime mutation lock:\n%s", after)
	}
}

func TestUpgradeDeepSeekProviderAccessRebuildsEveryVisibleRuntime(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `default_model = "deepseek-flash/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	sessionDir := config.SessionDir()
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	newOldController := func(id string) *blockingSnapshotCtrl {
		session := agent.NewSession("old system prompt")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "history " + id})
		exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
		ctrl := control.New(control.Options{
			Executor: exec, SessionDir: sessionDir,
			SessionPath: filepath.Join(sessionDir, id+".jsonl"), Label: id, Sink: event.Discard,
		})
		wrapped := newBlockingSnapshotCtrl(ctrl)
		close(wrapped.releaseSnapshot)
		return wrapped
	}
	oldA := newOldController("tab-a")
	oldB := newOldController("tab-b")
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tabA := &WorkspaceTab{
		ID: "tab-a", Scope: "global", WorkspaceRoot: workspace, Ready: true,
		Ctrl: oldA, model: "deepseek/deepseek-v4-flash", sink: &tabEventSink{tabID: "tab-a", app: app},
		disabledMCP: map[string]ServerView{},
	}
	tabB := &WorkspaceTab{
		ID: "tab-b", Scope: "global", WorkspaceRoot: workspace, Ready: true,
		Ctrl: oldB, model: "deepseek/deepseek-v4-flash", sink: &tabEventSink{tabID: "tab-b", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{tabA.ID: tabA, tabB.ID: tabB}
	app.tabOrder = []string{tabA.ID, tabB.ID}
	app.activeTabID = tabA.ID
	t.Cleanup(func() {
		for _, tab := range []*WorkspaceTab{tabA, tabB} {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
			tab.releaseSessionLease()
		}
	})

	if _, err := app.UpgradeDeepSeekProviderAccess("deepseek"); err != nil {
		t.Fatalf("UpgradeDeepSeekProviderAccess: %v", err)
	}
	if tabA.Ctrl == oldA || tabB.Ctrl == oldB {
		t.Fatalf("visible runtimes were not both rebuilt: active=%T inactive=%T", tabA.Ctrl, tabB.Ctrl)
	}
	if oldA.closeCount.Load() != 1 || oldB.closeCount.Load() != 1 {
		t.Fatalf("old controller close counts = %d, %d; want one each", oldA.closeCount.Load(), oldB.closeCount.Load())
	}
	for _, tab := range []*WorkspaceTab{tabA, tabB} {
		history := tab.Ctrl.History()
		if len(history) < 2 || !strings.HasPrefix(history[1].Content, "history ") {
			t.Fatalf("rebuilt tab %q lost history: %+v", tab.ID, history)
		}
	}
}

func TestUpgradeDeepSeekProviderAccessContinuesAfterWorkspaceBuildFailure(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `default_model = "deepseek-flash/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	brokenRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(brokenRoot, "reasonix.toml"), []byte(`[agent]
system_prompt_file = "/outside-workspace/system.md"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	workingRoot := t.TempDir()
	sessionDir := config.SessionDir()
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	newOldController := func(id string) *blockingSnapshotCtrl {
		session := agent.NewSession("old system prompt")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "history " + id})
		exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
		ctrl := control.New(control.Options{
			Executor: exec, SessionDir: sessionDir,
			SessionPath: filepath.Join(sessionDir, id+".jsonl"), Label: id, Sink: event.Discard,
		})
		wrapped := newBlockingSnapshotCtrl(ctrl)
		close(wrapped.releaseSnapshot)
		return wrapped
	}
	oldBroken := newOldController("upgrade-broken")
	oldWorking := newOldController("upgrade-working")
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	broken := &WorkspaceTab{
		ID: "a-broken", Scope: "project", WorkspaceRoot: brokenRoot, Ready: true,
		Ctrl: oldBroken, model: "deepseek/deepseek-v4-flash", sink: &tabEventSink{tabID: "a-broken", app: app},
		disabledMCP: map[string]ServerView{},
	}
	working := &WorkspaceTab{
		ID: "b-working", Scope: "project", WorkspaceRoot: workingRoot, Ready: true,
		Ctrl: oldWorking, model: "deepseek/deepseek-v4-flash", sink: &tabEventSink{tabID: "b-working", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{broken.ID: broken, working.ID: working}
	app.tabOrder = []string{broken.ID, working.ID}
	app.activeTabID = broken.ID
	t.Cleanup(func() {
		for _, tab := range []*WorkspaceTab{broken, working} {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
			tab.releaseSessionLease()
		}
	})

	warning, err := app.UpgradeDeepSeekProviderAccess("deepseek")
	if err == nil || !strings.Contains(err.Error(), "relative path within the workspace") {
		t.Fatalf("UpgradeDeepSeekProviderAccess error = %v, want first workspace build failure", err)
	}
	if warning != "" {
		t.Fatalf("UpgradeDeepSeekProviderAccess warning = %q, want no lease warning", warning)
	}
	updated, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(updated), `kind = "anthropic"`) {
		t.Fatalf("protocol was not persisted before the runtime error:\n%s", updated)
	}
	if broken.Ctrl != oldBroken || oldBroken.closeCount.Load() != 0 {
		t.Fatalf("failed tab changed controller: ctrl=%T closes=%d", broken.Ctrl, oldBroken.closeCount.Load())
	}
	if working.Ctrl == oldWorking || oldWorking.closeCount.Load() != 1 {
		t.Fatalf("working sibling was not rebuilt: ctrl=%T closes=%d", working.Ctrl, oldWorking.closeCount.Load())
	}
}

func TestUpgradeDeepSeekProviderAccessDefersLeasedTabAndRebuildsSibling(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "DEEPSEEK_API_KEY", "sk-test")
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `default_model = "deepseek-flash/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	sessionDir := config.SessionDir()
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	leasedPath := filepath.Join(sessionDir, "upgrade-leased.jsonl")
	workingPath := filepath.Join(sessionDir, "upgrade-working.jsonl")
	externalLease, err := agent.TryAcquireSessionLease(leasedPath)
	if err != nil {
		t.Fatalf("TryAcquireSessionLease: %v", err)
	}
	t.Cleanup(externalLease.Release)
	newOldController := func(id, sessionPath string) *blockingSnapshotCtrl {
		session := agent.NewSession("old system prompt")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "history " + id})
		exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
		ctrl := control.New(control.Options{
			Executor: exec, SessionDir: sessionDir, SessionPath: sessionPath, Label: id, Sink: event.Discard,
		})
		wrapped := newBlockingSnapshotCtrl(ctrl)
		close(wrapped.releaseSnapshot)
		return wrapped
	}
	oldLeased := newOldController("upgrade-leased", leasedPath)
	oldWorking := newOldController("upgrade-working", workingPath)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	leased := &WorkspaceTab{
		ID: "a-leased", Scope: "global", WorkspaceRoot: workspace, SessionPath: leasedPath, Ready: true,
		Ctrl: oldLeased, model: "deepseek/deepseek-v4-flash", sink: &tabEventSink{tabID: "a-leased", app: app},
		disabledMCP: map[string]ServerView{},
	}
	working := &WorkspaceTab{
		ID: "b-working", Scope: "global", WorkspaceRoot: workspace, SessionPath: workingPath, Ready: true,
		Ctrl: oldWorking, model: "deepseek/deepseek-v4-flash", sink: &tabEventSink{tabID: "b-working", app: app},
		disabledMCP: map[string]ServerView{},
	}
	app.tabs = map[string]*WorkspaceTab{leased.ID: leased, working.ID: working}
	app.tabOrder = []string{leased.ID, working.ID}
	app.activeTabID = leased.ID
	t.Cleanup(func() {
		for _, tab := range []*WorkspaceTab{leased, working} {
			if tab.Ctrl != nil {
				tab.Ctrl.Close()
			}
			tab.releaseSessionLease()
		}
	})

	warning, err := app.UpgradeDeepSeekProviderAccess("deepseek")
	if err != nil {
		t.Fatalf("UpgradeDeepSeekProviderAccess: %v", err)
	}
	if !strings.Contains(warning, "saved, but the current session could not refresh yet") {
		t.Fatalf("UpgradeDeepSeekProviderAccess warning = %q, want deferred lease warning", warning)
	}
	if !app.deferredRebuildPending(leased.ID) {
		t.Fatal("leased tab did not receive a tab-bound deferred rebuild")
	}
	if app.deferredRebuildPending(working.ID) {
		t.Fatal("working sibling unexpectedly received a deferred rebuild")
	}
	if leased.Ctrl != oldLeased || oldLeased.closeCount.Load() != 0 {
		t.Fatalf("leased tab changed controller: ctrl=%T closes=%d", leased.Ctrl, oldLeased.closeCount.Load())
	}
	if working.Ctrl == oldWorking || oldWorking.closeCount.Load() != 1 {
		t.Fatalf("working sibling was not rebuilt: ctrl=%T closes=%d", working.Ctrl, oldWorking.closeCount.Load())
	}
}

func TestUpgradeDeepSeekProviderAccessRejectsDetachedRuntimeBeforeMutation(t *testing.T) {
	isolateDesktopUserDirs(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := `[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	detachedCtrl := control.New(control.Options{Label: "detached", Sink: event.Discard})
	app.detachedSessions = map[string]*WorkspaceTab{
		"detached": {ID: "detached", Scope: "global", Ready: true, Ctrl: detachedCtrl},
	}
	t.Cleanup(detachedCtrl.Close)

	_, err := app.UpgradeDeepSeekProviderAccess("deepseek")
	if err == nil || !strings.Contains(err.Error(), "background session is still open") {
		t.Fatalf("UpgradeDeepSeekProviderAccess error = %v, want detached-runtime guard", err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != raw {
		t.Fatalf("protocol changed despite detached-runtime rejection:\n%s", after)
	}
}

func TestProviderModelOverrideViewPreservesMaxOutputTokens(t *testing.T) {
	input := map[string]config.ProviderModelOverride{
		"limited": {ContextWindow: 64_000, MaxOutputTokens: 8_192},
		"omitted": {MaxOutputTokens: -1},
	}
	views := providerModelOverridesForView(input, []string{"limited", "omitted"})
	if len(views) != 2 || views[0].MaxOutputTokens != 8_192 || views[1].MaxOutputTokens != -1 {
		t.Fatalf("model override views = %+v, want positive and negative output-token semantics preserved", views)
	}
	roundTrip := providerModelOverridesForSave(views, []string{"limited", "omitted"})
	if got := roundTrip["limited"]; got.ContextWindow != 64_000 || got.MaxOutputTokens != 8_192 {
		t.Fatalf("limited override = %+v, want context and output limits preserved", got)
	}
	if got := roundTrip["omitted"]; got.MaxOutputTokens != -1 {
		t.Fatalf("omitted override = %+v, want negative wire-omission marker preserved", got)
	}
}

func TestDeepSeekProtocolUpgradeSourceAvailableWithLegacyGlobalAndProjectConfig(t *testing.T) {
	isolateDesktopUserDirs(t)
	legacyPath := config.LegacyUserConfigPath()
	if legacyPath == "" {
		t.Skip("platform has no distinct legacy user-config path")
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("# project config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !config.CanUpgradeDeepSeekProviderProtocolUserConfig("deepseek") {
		t.Fatal("project config must not hide an available legacy global upgrade source")
	}
}

func TestOfficialMimoAPITemplateRemoved(t *testing.T) {
	if entries, keyEnv, err := officialProviderTemplate("mimo-api", "en"); err == nil {
		t.Fatalf("officialProviderTemplate(mimo-api) = entries=%v key=%q nil error, want unknown template", entries, keyEnv)
	}
}

func TestOfficialDeepSeekTemplateUsesRegionalPricing(t *testing.T) {
	// Language no longer selects list-price tables; templates freeze the default
	// USD official rates. Display currency is independent (billing.display_currency).
	for _, language := range []string{"en", "zh"} {
		entries, keyEnv, err := officialProviderTemplate("deepseek", language)
		if err != nil {
			t.Fatalf("officialProviderTemplate(%s): %v", language, err)
		}
		if keyEnv != "DEEPSEEK_API_KEY" || len(entries) != 1 {
			t.Fatalf("template = %v/%q, want one DEEPSEEK_API_KEY entry", entries, keyEnv)
		}
		got := entries[0]
		if got.Kind != "anthropic" || got.BaseURL != "https://api.deepseek.com/anthropic" || !config.EffectiveWebSearch(&got) || got.Thinking != "enabled" {
			t.Fatalf("%s DeepSeek template = kind:%q base_url:%q web_search:%t thinking:%q, want Anthropic-compatible with web search", language, got.Kind, got.BaseURL, config.EffectiveWebSearch(&got), got.Thinking)
		}
		if price := got.Prices["deepseek-v4-flash"]; price == nil || price.Currency != "$" || price.Output != 0.28 {
			t.Fatalf("%s deepseek-v4-flash price = %+v, want frozen USD table", language, price)
		}
		if price := got.Prices["deepseek-v4-pro"]; price == nil || price.Currency != "$" || price.Output != 0.87 {
			t.Fatalf("%s deepseek-v4-pro price = %+v, want frozen USD table", language, price)
		}
	}
}

func TestSetAgentParamsIgnoresDeprecatedStepLimits(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetAgentParams(0.35, 37, 9, "custom system"); err != nil {
		t.Fatalf("SetAgentParams: %v", err)
	}

	view := app.Settings()
	if view.Agent.MaxSteps != 0 || view.Agent.PlannerMaxSteps != 0 {
		t.Fatalf("Settings().Agent = %+v, want deprecated step limits normalized to zero", view.Agent)
	}
	if view.Agent.Temperature != 0.35 || view.Agent.SystemPrompt != "custom system" {
		t.Fatalf("Settings().Agent did not preserve other agent params: %+v", view.Agent)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.MaxSteps != 0 || cfg.Agent.PlannerMaxSteps != 0 {
		t.Fatalf("saved config agent steps = max:%d planner:%d, want automatic 0/0", cfg.Agent.MaxSteps, cfg.Agent.PlannerMaxSteps)
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

func TestSetCompactRatioPersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	defaultView := app.Settings()
	if defaultView.Agent.CompactRatio != 0.85 || defaultView.Agent.EffectiveCompactRatio != 0.85 {
		t.Fatalf("default compact ratios = %v/%v, want 0.85/0.85", defaultView.Agent.CompactRatio, defaultView.Agent.EffectiveCompactRatio)
	}
	if err := app.SetCompactRatio(0.7); err != nil {
		t.Fatalf("SetCompactRatio: %v", err)
	}

	view := app.Settings()
	if view.Agent.CompactRatio != 0.7 {
		t.Fatalf("Settings().Agent.CompactRatio = %v, want 0.7", view.Agent.CompactRatio)
	}

	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.CompactRatio != 0.7 {
		t.Fatalf("saved compact ratio = %v, want 0.7", cfg.Agent.CompactRatio)
	}
	// Deprecated multi-threshold fields stay cleared / unused.
	if cfg.Agent.ToolResultSnipRatio != 0 || cfg.Agent.CompactForceRatio != 0 {
		t.Fatalf("setting compact ratio revived deprecated thresholds: %+v", cfg.Agent)
	}

	if err := app.SetCompactRatio(0.9); err == nil {
		t.Fatal("SetCompactRatio should reject values outside the Desktop safety range")
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	if cfg.Agent.CompactRatio != 0.7 {
		t.Fatalf("rejected update changed saved compact ratio to %v", cfg.Agent.CompactRatio)
	}
}

func TestSetCompactRatioRejectsActiveWorkBeforeSaving(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	app.setTestCtrl(newBackgroundJobController(t, "compact-ratio-job"), "")
	err := app.SetCompactRatio(0.7)
	if err == nil || !strings.Contains(err.Error(), "stop background jobs") {
		t.Fatalf("SetCompactRatio with background job error = %v, want active-work guard", err)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).Agent.CompactRatio; got != 0.85 {
		t.Fatalf("compact ratio changed after rejected update: %v", got)
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

func TestSetDesktopCurrencyPersistsDisplayWithoutRewritingOfficialPricing(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetDesktopCurrency("CNY"); err != nil {
		t.Fatalf("SetDesktopCurrency: %v", err)
	}

	view := app.Settings()
	if view.DesktopCurrency != "CNY" {
		t.Fatalf("Settings().DesktopCurrency = %q, want CNY", view.DesktopCurrency)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if got := cfg.DisplayCurrencyPref(); got != "CNY" {
		t.Fatalf("display pref = %q, want CNY", got)
	}
	flash, ok := cfg.Provider("deepseek-flash")
	// Display currency must not rewrite frozen list prices (default USD table).
	if !ok || flash.Price == nil || flash.Price.Output != 0.28 || flash.Price.Currency != "$" {
		t.Fatalf("saved DeepSeek flash price = %+v, want frozen USD official price", flash)
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

func TestSetAutoPlanCompatibilityCannotReenableRetiredFeature(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if err := app.SetAutoPlan("off"); err != nil {
		t.Fatalf("SetAutoPlan(off): %v", err)
	}
	if err := app.SetAutoPlan("on"); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("SetAutoPlan(on) error = %v, want retired error", err)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	if got.Agent.AutoPlan != "off" || got.Agent.AutoPlanClassifier != "" {
		t.Fatalf("retired auto-plan state = (%q, %q), want off/empty", got.Agent.AutoPlan, got.Agent.AutoPlanClassifier)
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

func TestSetDesktopUpdateChannelMigratesToStable(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if got := app.Settings().UpdateChannel; got != "stable" {
		t.Fatalf("Settings().UpdateChannel default = %q, want stable", got)
	}
	if err := app.SetDesktopUpdateChannel("canary"); err != nil {
		t.Fatalf("SetDesktopUpdateChannel: %v", err)
	}
	view := app.Settings()
	if view.UpdateChannel != "stable" {
		t.Fatalf("Settings().UpdateChannel = %q, want stable", view.UpdateChannel)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.Desktop.UpdateChannel != "" {
		t.Fatalf("desktop.update_channel = %q, want omitted legacy field", cfg.Desktop.UpdateChannel)
	}
	if cfg.DesktopUpdateChannel() != "stable" {
		t.Fatalf("DesktopUpdateChannel() = %q, want stable", cfg.DesktopUpdateChannel())
	}
}

func TestSetDesktopConversationWidthPersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if got := app.Settings().ConversationWidth; got != "standard" {
		t.Fatalf("Settings().ConversationWidth default = %q, want standard", got)
	}
	if got := app.DesktopStartupSettings().ConversationWidth; got != "standard" {
		t.Fatalf("DesktopStartupSettings().ConversationWidth default = %q, want standard", got)
	}
	if err := app.SetDesktopConversationWidth("full"); err != nil {
		t.Fatalf("SetDesktopConversationWidth: %v", err)
	}
	if got := app.Settings().ConversationWidth; got != "full" {
		t.Fatalf("Settings().ConversationWidth = %q, want full", got)
	}
	if got := app.DesktopStartupSettings().ConversationWidth; got != "full" {
		t.Fatalf("DesktopStartupSettings().ConversationWidth = %q, want full", got)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if got := cfg.DesktopConversationWidth(); got != "full" {
		t.Fatalf("persisted conversation width = %q, want full", got)
	}

	if err := app.SetDesktopConversationWidth("wide"); err == nil {
		t.Fatal("SetDesktopConversationWidth(wide) unexpectedly succeeded")
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DesktopConversationWidth(); got != "full" {
		t.Fatalf("invalid update changed persisted conversation width to %q", got)
	}

	raw, err := json.Marshal(app.DesktopStartupSettings())
	if err != nil {
		t.Fatalf("marshal DesktopStartupSettings: %v", err)
	}
	if !strings.Contains(string(raw), `"conversationWidth":"full"`) {
		t.Fatalf("startup bridge payload omitted conversationWidth: %s", raw)
	}
}

func TestSetDefaultToolApprovalModePersistsToUserConfig(t *testing.T) {
	isolateDesktopUserDirs(t)

	app := NewApp()
	if app.Settings().DefaultToolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("Settings().DefaultToolApprovalMode = %q, want auto", app.Settings().DefaultToolApprovalMode)
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

func TestRetiredAutoRecoveryCheckpointSettingsAreNoOps(t *testing.T) {
	isolateDesktopUserDirs(t)

	cfgPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("[agent]\nauto_recovery_checkpoint = \"off\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	app := NewApp()
	if err := app.SetDefaultAutoRecoveryCheckpoint(false); err != nil {
		t.Fatalf("legacy setter: %v", err)
	}
	if !app.RecoveryCheckpointEnabled() || !app.RecoveryCheckpointEnabledTab("legacy") {
		t.Fatal("retired config or legacy setter disabled built-in Auto Guard")
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

func TestProjectHooksSettingsUseActiveWorkspaceRootAndLoadByDefault(t *testing.T) {
	isolateDesktopUserDirs(t)
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
	loaded := hook.Load(hook.LoadOptions{ProjectRoot: project})
	if len(loaded) != 1 || loaded[0].Scope != hook.ScopeProject || loaded[0].Event != hook.Stop {
		t.Fatalf("project hooks should load by default: %+v", loaded)
	}
}

func TestLegacyTrustProjectHooksMethodsAreNoOps(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if err := app.TrustProjectHooks(); err != nil {
		t.Fatalf("TrustProjectHooks compatibility call: %v", err)
	}
	if err := app.TrustProjectHooksForRoot(t.TempDir()); err != nil {
		t.Fatalf("TrustProjectHooksForRoot compatibility call: %v", err)
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

func TestLoadDesktopUserConfigForRootDoesNotFollowActiveTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	userPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("default_model = \"local/m1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	targetRoot := t.TempDir()
	activeRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetRoot, "reasonix.toml"), []byte("[bot]\nenabled = true\nmodel = \"target\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeRoot, "reasonix.toml"), []byte("[bot]\nenabled = true\nmodel = \"active\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"active": {ID: "active", Scope: "project", WorkspaceRoot: activeRoot, Ready: true},
	}
	app.activeTabID = "active"

	cfg, _, err := app.loadDesktopUserConfigForViewForRoot(targetRoot)
	if err != nil {
		t.Fatalf("loadDesktopUserConfigForViewForRoot: %v", err)
	}
	if !cfg.Bot.Enabled || cfg.Bot.Model != "target" {
		t.Fatalf("root-specific view followed active tab: bot = %+v", cfg.Bot)
	}

	unlock := config.LockUserConfigEdits()
	_, _, err = app.loadDesktopUserConfigForEditForRoot(targetRoot)
	unlock()
	if err != nil {
		t.Fatalf("loadDesktopUserConfigForEditForRoot: %v", err)
	}
	migrated := config.LoadForEditWithoutCredentials(userPath)
	if !migrated.Bot.Enabled || migrated.Bot.Model != "target" {
		t.Fatalf("root-specific edit migrated the active tab instead: bot = %+v", migrated.Bot)
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
