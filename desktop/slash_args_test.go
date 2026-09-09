package main

import (
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestSlashArgsEffortUsesActiveTabCapability(t *testing.T) {
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "SLASH_ARGS_KEY", "sk-test")
	cfg := config.Default()
	entry := config.ProviderEntry{
		Name: "slash-args", Kind: "openai", BaseURL: "https://example.invalid/v1",
		Model: "model", APIKeyEnv: "SLASH_ARGS_KEY",
		SupportedEfforts: []string{"low"}, DefaultEffort: "low",
	}
	if err := cfg.UpsertProvider(entry); err != nil {
		t.Fatal(err)
	}
	cfg.DefaultModel = "slash-args/model"
	cfg.Desktop.ProviderAccess = []string{"slash-args"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}

	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	app := NewApp()
	app.setTestCtrl(ctrl, "slash-args/model")
	got := app.SlashArgs("/effort l")
	if len(got.Items) != 1 || got.Items[0].Label != "low" {
		t.Fatalf("SlashArgs(/effort l) = %+v, want active-tab effort capability", got.Items)
	}
}
