package main

import (
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestRemoveProviderAccessesRemovesGroupedOpenCodeGoRoutesAtomically(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	fallback := config.ProviderEntry{
		Name: "mimo-pro", Kind: "openai", BaseURL: "https://mimo.example/v1",
		Model: "mimo-v2.5-pro", APIKeyEnv: "REASONIX_TEST_KEY",
	}
	cfg.DefaultModel = "opencode-go/glm-5.3"
	cfg.Agent.PlannerModel = "opencode-go-anthropic/qwen3.7-plus"
	cfg.Agent.VisionModel = "opencode-go/kimi-k3"
	cfg.Agent.RecoveryModel = "opencode-go-anthropic/qwen3.7-plus"
	cfg.Agent.SubagentModel = "opencode-go-responses/grok-4.5"
	cfg.Agent.SubagentModels = map[string]string{"review": "opencode-go/glm-5.3"}
	cfg.Bot.Model = "opencode-go-anthropic/qwen3.7-plus"
	cfg.Bot.QQ.Model = "opencode-go/glm-5.3"
	cfg.Bot.Dingtalk.Model = "opencode-go-responses/grok-4.5"
	cfg.Bot.Routes = []config.BotRouteConfig{{ConnectionID: "route", Model: "opencode-go-anthropic/qwen3.7-plus"}}
	cfg.Bot.Connections = []config.BotConnectionConfig{
		{ID: "opencode", Provider: "feishu", Model: "opencode-go-responses/grok-4.5"},
		{ID: "fallback", Provider: "weixin", Model: "mimo-pro/mimo-v2.5-pro"},
	}
	cfg.Desktop.ProviderAccess = []string{
		"opencode-go", "opencode-go-anthropic", "opencode-go-responses",
		"opencode-go-deepseek-responses", "mimo-pro",
	}
	cfg.Providers = []config.ProviderEntry{
		{
			Name: "opencode-go", Kind: "openai", BaseURL: "https://opencode.ai/zen/go/v1",
			Model: "glm-5.3", APIKeyEnv: "REASONIX_TEST_KEY", PresetID: "opencode-go",
		},
		{
			Name: "opencode-go-anthropic", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go",
			Model: "qwen3.7-plus", APIKeyEnv: "REASONIX_TEST_KEY", PresetID: "opencode-go-anthropic",
		},
		{
			Name: "opencode-go-responses", Kind: "responses", BaseURL: "https://opencode.ai/zen/go/v1",
			Model: "grok-4.5", APIKeyEnv: "REASONIX_TEST_KEY", PresetID: "opencode-go-responses",
		},
		{
			Name: "opencode-go-deepseek-responses", Kind: "responses", BaseURL: "https://opencode.ai/zen/go/v1",
			Model: "deepseek-v4-flash", APIKeyEnv: "REASONIX_TEST_KEY", PresetID: "opencode-go-deepseek-responses",
		},
		fallback,
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	tabs := []*WorkspaceTab{
		{ID: "chat", Scope: "global", model: cfg.DefaultModel},
		{ID: "planner", Scope: "global", model: cfg.Agent.PlannerModel},
		{ID: "subagent", Scope: "global", model: cfg.Agent.SubagentModel},
	}
	app.tabs = map[string]*WorkspaceTab{}
	for _, tab := range tabs {
		app.tabs[tab.ID] = tab
	}
	app.tabOrder = []string{"chat", "planner", "subagent"}
	app.activeTabID = "chat"

	if err := app.RemoveProviderAccesses([]string{"opencode-go", "opencode-go-anthropic", "opencode-go-responses", "opencode-go-deepseek-responses"}); err != nil {
		t.Fatalf("RemoveProviderAccesses: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	access := providerAccessSet(got.Desktop.ProviderAccess)
	if access["opencode-go"] || access["opencode-go-anthropic"] || access["opencode-go-responses"] || access["opencode-go-deepseek-responses"] || !access["mimo-pro"] {
		t.Fatalf("provider_access = %+v, want only mimo-pro", got.Desktop.ProviderAccess)
	}
	for _, name := range []string{"opencode-go", "opencode-go-anthropic", "opencode-go-responses", "opencode-go-deepseek-responses"} {
		if _, ok := got.Provider(name); ok {
			t.Fatalf("provider %q still exists after grouped removal", name)
		}
	}
	wantFallback := "mimo-pro"
	if got.DefaultModel != wantFallback || got.Agent.PlannerModel != wantFallback || got.Agent.RecoveryModel != wantFallback || got.Agent.SubagentModel != wantFallback || got.Agent.SubagentModels["review"] != wantFallback {
		t.Fatalf("provider refs were not retargeted: default=%q planner=%q recovery=%q subagent=%q skills=%+v", got.DefaultModel, got.Agent.PlannerModel, got.Agent.RecoveryModel, got.Agent.SubagentModel, got.Agent.SubagentModels)
	}
	if got.Agent.VisionModel != "" {
		t.Fatalf("vision_model = %q, want cleared removed provider reference", got.Agent.VisionModel)
	}
	if got.Bot.Model != wantFallback || got.Bot.QQ.Model != wantFallback || got.Bot.Dingtalk.Model != wantFallback || len(got.Bot.Routes) != 1 || got.Bot.Routes[0].Model != wantFallback || len(got.Bot.Connections) != 2 || got.Bot.Connections[0].Model != wantFallback || got.Bot.Connections[1].Model != "mimo-pro/mimo-v2.5-pro" {
		t.Fatalf("bot refs were not retargeted selectively: model=%q qq=%q dingtalk=%q routes=%+v connections=%+v", got.Bot.Model, got.Bot.QQ.Model, got.Bot.Dingtalk.Model, got.Bot.Routes, got.Bot.Connections)
	}
	for _, tab := range tabs {
		if tab.model != "mimo-pro/mimo-v2.5-pro" {
			t.Fatalf("tab %q model = %q, want mimo-pro/mimo-v2.5-pro", tab.ID, tab.model)
		}
	}
}

func TestProviderRemovalStateFingerprintCoversAuxiliaryModelReferences(t *testing.T) {
	newConfig := func() *config.Config {
		cfg := config.Default()
		cfg.Providers = []config.ProviderEntry{{Name: "provider", Kind: "openai", BaseURL: "https://provider.example/v1", Model: "model"}}
		return cfg
	}
	base := providerRemovalStateFingerprint(newConfig(), "revision")
	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{name: "vision", mutate: func(cfg *config.Config) { cfg.Agent.VisionModel = "provider/model" }},
		{name: "guardian", mutate: func(cfg *config.Config) { cfg.Agent.GuardianModel = "provider/model" }},
		{name: "recovery", mutate: func(cfg *config.Config) { cfg.Agent.RecoveryModel = "provider/model" }},
		{name: "bot default", mutate: func(cfg *config.Config) { cfg.Bot.Model = "provider/model" }},
		{name: "qq", mutate: func(cfg *config.Config) { cfg.Bot.QQ.Model = "provider/model" }},
		{name: "dingtalk", mutate: func(cfg *config.Config) { cfg.Bot.Dingtalk.Model = "provider/model" }},
		{name: "route", mutate: func(cfg *config.Config) { cfg.Bot.Routes = []config.BotRouteConfig{{Model: "provider/model"}} }},
		{name: "connection", mutate: func(cfg *config.Config) {
			cfg.Bot.Connections = []config.BotConnectionConfig{{Model: "provider/model"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig()
			tt.mutate(cfg)
			if got := providerRemovalStateFingerprint(cfg, "revision"); got == base {
				t.Fatalf("fingerprint did not cover %s model reference", tt.name)
			}
		})
	}
}

func TestRemoveProviderAccessesKeepsCoreRoutesWhenRemovingOpenCodeGoSearchSubset(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

	coreNames := []string{"opencode-go", "opencode-go-anthropic", "opencode-go-responses"}
	searchNames := []string{"opencode-go-deepseek-anthropic", "opencode-go-deepseek-responses"}
	cfg := config.Default()
	cfg.DefaultModel = "opencode-go/glm-5.3"
	cfg.Desktop.ProviderAccess = append(append(append([]string(nil), coreNames...), searchNames...), "fallback")
	cfg.Providers = []config.ProviderEntry{
		{Name: "opencode-go", Kind: "openai", BaseURL: "https://opencode.ai/zen/go/v1", Model: "glm-5.3", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "opencode-go-anthropic", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go", Model: "qwen3.7-plus", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "opencode-go-responses", Kind: "responses", BaseURL: "https://opencode.ai/zen/go/v1", Model: "grok-4.5", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "opencode-go-deepseek-anthropic", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go", Model: "deepseek-v4-flash", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "opencode-go-deepseek-responses", Kind: "responses", BaseURL: "https://opencode.ai/zen/go/v1", Model: "deepseek-v4-flash", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "fallback", Kind: "openai", BaseURL: "https://fallback.example/v1", Model: "fallback-model", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if err := NewApp().RemoveProviderAccesses(searchNames); err != nil {
		t.Fatalf("RemoveProviderAccesses: %v", err)
	}

	got := config.LoadForEdit(config.UserConfigPath())
	for _, name := range coreNames {
		if _, ok := got.Provider(name); !ok {
			t.Fatalf("core OpenCode Go provider %q was removed with the search subset", name)
		}
	}
	for _, name := range searchNames {
		if _, ok := got.Provider(name); ok {
			t.Fatalf("search provider %q still exists", name)
		}
	}
	if got.DefaultModel != cfg.DefaultModel {
		t.Fatalf("default_model = %q, want preserved %q", got.DefaultModel, cfg.DefaultModel)
	}
}

func TestRemoveProviderAccessesStillRejectsUnrelatedCustomBatch(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "REASONIX_TEST_KEY", "sk-test")

	cfg := config.Default()
	cfg.Desktop.ProviderAccess = []string{"custom-a", "custom-b"}
	cfg.Providers = []config.ProviderEntry{
		{Name: "custom-a", Kind: "openai", BaseURL: "https://a.example/v1", Model: "a", APIKeyEnv: "REASONIX_TEST_KEY"},
		{Name: "custom-b", Kind: "openai", BaseURL: "https://b.example/v1", Model: "b", APIKeyEnv: "REASONIX_TEST_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := NewApp().RemoveProviderAccesses([]string{"custom-a", "custom-b"})
	if err == nil || !strings.Contains(err.Error(), "custom provider") {
		t.Fatalf("RemoveProviderAccesses error = %v, want custom-provider batch rejection", err)
	}
	got := config.LoadForEdit(config.UserConfigPath())
	if _, ok := got.Provider("custom-a"); !ok {
		t.Fatal("custom-a was deleted after rejected batch")
	}
	if _, ok := got.Provider("custom-b"); !ok {
		t.Fatal("custom-b was deleted after rejected batch")
	}
}
