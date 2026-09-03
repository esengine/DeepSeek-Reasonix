package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestAddProviderPresetAccountCreatesSecondKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if _, err := app.AddProviderPresetAccess("opencode-go-recommended", "sk-main"); err != nil {
		t.Fatalf("install main: %v", err)
	}
	if _, err := app.AddProviderPresetAccount("opencode-go-recommended", "团队账号", "sk-team"); err != nil {
		t.Fatalf("add team account: %v", err)
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	var mainEnv, teamEnv string
	for _, account := range cfg.ProviderAccounts {
		if account.ProviderID != "opencode-go" {
			continue
		}
		switch account.ID {
		case config.MainProviderAccountID:
			mainEnv = account.APIKeyEnv
		case "team":
			teamEnv = account.APIKeyEnv
		}
	}
	if mainEnv == "" || teamEnv == "" || mainEnv == teamEnv {
		t.Fatalf("account envs = %q %q", mainEnv, teamEnv)
	}
	data, err := os.ReadFile(config.UserCredentialsPath())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, mainEnv+"=sk-main") || !strings.Contains(text, teamEnv+"=sk-team") {
		t.Fatalf("credentials missing both keys:\n%s", text)
	}
	if _, ok := cfg.Provider("opencode-go--team"); !ok {
		t.Fatalf("missing team provider, have %v", providerNames(cfg))
	}
}

func TestSettingsViewEncodesEmptyProviderAccounts(t *testing.T) {
	isolateDesktopUserDirs(t)
	view := NewApp().defaultSettingsView()
	if view.ProviderAccounts == nil {
		t.Fatal("ProviderAccounts is nil")
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"providerAccounts":[]`) {
		t.Fatalf("providerAccounts not encoded as []: %s", raw)
	}
}

func TestRejectedAccountCreateDoesNotWriteKey(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	if _, err := app.AddProviderPresetAccount("not-a-preset", "团队", "sk-should-not-save"); err == nil {
		t.Fatal("expected unknown preset error")
	}
	if _, err := os.Stat(config.UserCredentialsPath()); err == nil {
		data, _ := os.ReadFile(config.UserCredentialsPath())
		if strings.Contains(string(data), "sk-should-not-save") {
			t.Fatalf("rejected create wrote key: %s", data)
		}
	}
}

func providerNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Providers))
	for _, p := range cfg.Providers {
		names = append(names, p.Name)
	}
	return names
}

func TestAccountKeyEnvSharedScansRetainedAndCustomProviders(t *testing.T) {
	account := config.ProviderAccount{ProviderID: "deepseek", ID: "team", APIKeyEnv: "SHARED_KEY", Retired: true}
	cfg := &config.Config{
		ProviderAccounts: []config.ProviderAccount{account},
		Providers: []config.ProviderEntry{
			{Name: "deepseek--team", APIKeyEnv: "SHARED_KEY", AccountProviderID: "deepseek", AccountID: "team"},
		},
	}
	if accountKeyEnvShared(cfg, account) {
		t.Fatal("retained entries belonging to the same retired account should not keep its key live")
	}
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "custom", APIKeyEnv: "SHARED_KEY"})
	if !accountKeyEnvShared(cfg, account) {
		t.Fatal("custom provider sharing the key env must prevent key deletion")
	}
}
