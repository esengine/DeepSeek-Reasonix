package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

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

func TestAddOfficialProviderAccessPreservesExistingOfficialCustomization(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
models = ["deepseek-v4-flash"]
default = "deepseek-v4-flash"
api_key_env = "MY_DEEPSEEK_KEY"
headers = { X-Route = "official-custom" }
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().AddOfficialProviderAccess("deepseek", ""); err != nil {
		t.Fatalf("AddOfficialProviderAccess: %v", err)
	}
	cfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	p, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider not saved")
	}
	if p.Kind != "openai" || p.BaseURL != "https://api.deepseek.com/v1" || p.APIKeyEnv != "MY_DEEPSEEK_KEY" || p.Headers["X-Route"] != "official-custom" {
		t.Fatalf("existing official customization was overwritten: %+v", p)
	}
	if !providerAccessSet(cfg.Desktop.ProviderAccess)["deepseek"] {
		t.Fatalf("provider_access missing preserved DeepSeek provider: %+v", cfg.Desktop.ProviderAccess)
	}
}

func TestAddOfficialProviderAccessRepairsCatalogWithoutResettingOfficialTransport(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com/v1"
api_key_env = "MY_DEEPSEEK_KEY"
headers = { X-Route = "official-custom" }
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().AddOfficialProviderAccess("deepseek", "test-key"); err != nil {
		t.Fatalf("AddOfficialProviderAccess: %v", err)
	}
	cfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	p, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatal("deepseek provider not saved")
	}
	if p.Kind != "openai" || p.BaseURL != "https://api.deepseek.com/v1" || p.APIKeyEnv != "MY_DEEPSEEK_KEY" || p.Headers["X-Route"] != "official-custom" {
		t.Fatalf("official transport customization was overwritten: %+v", p)
	}
	if got := p.ModelList(); len(got) != 2 || got[0] != "deepseek-v4-flash" || got[1] != "deepseek-v4-pro" {
		t.Fatalf("repaired model catalog = %v, want official Flash/Pro models", got)
	}
	if !config.CredentialStored("MY_DEEPSEEK_KEY") || config.CredentialStored("DEEPSEEK_API_KEY") {
		t.Fatal("credential was not saved exclusively under the preserved api_key_env")
	}
}

func TestAddOfficialProviderAccessRetargetsLegacyDeepSeekReferences(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "deepseek-flash"

[agent]
planner_model = "deepseek-pro"
subagent_model = "deepseek-flash/custom-flash"
subagent_models = { vision = "deepseek-pro/custom-pro", keep = "other/model" }

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := NewApp().AddOfficialProviderAccess("deepseek", ""); err != nil {
		t.Fatalf("AddOfficialProviderAccess: %v", err)
	}
	cfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	if cfg.DefaultModel != "deepseek/deepseek-v4-flash" || cfg.Agent.PlannerModel != "deepseek/deepseek-v4-pro" ||
		cfg.Agent.SubagentModel != "deepseek/custom-flash" || cfg.Agent.SubagentModels["vision"] != "deepseek/custom-pro" ||
		cfg.Agent.SubagentModels["keep"] != "other/model" {
		t.Fatalf("legacy DeepSeek references were not retargeted: default=%q planner=%q subagent=%q skills=%v",
			cfg.DefaultModel, cfg.Agent.PlannerModel, cfg.Agent.SubagentModel, cfg.Agent.SubagentModels)
	}
}

func TestAddOfficialProviderAccessRejectsSameNameCustomEndpointBeforeSavingKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://proxy.example.invalid/v1"
models = ["deepseek-v4-flash"]
default = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := NewApp().AddOfficialProviderAccess("deepseek", "test-key")
	if err == nil || !strings.Contains(err.Error(), "already belongs to a custom endpoint") {
		t.Fatalf("AddOfficialProviderAccess error = %v, want same-name custom endpoint rejection", err)
	}
	if data, readErr := os.ReadFile(config.UserCredentialsPath()); readErr == nil && strings.Contains(string(data), "DEEPSEEK_API_KEY") {
		t.Fatalf("credential was saved before provider name conflict rejection:\n%s", data)
	}
	cfg := config.LoadForEditWithoutCredentials(config.UserConfigPath())
	p, ok := cfg.Provider("deepseek")
	if !ok || p.BaseURL != "https://proxy.example.invalid/v1" {
		t.Fatalf("same-name custom provider changed after rejection: %+v", p)
	}
}
