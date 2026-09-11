package main

import (
	"os"
	"testing"

	"reasonix/internal/config"
)

func TestTabMetaExtrasRefreshDoesNotPinCredentials(t *testing.T) {
	isolateDesktopUserDirs(t)
	const key = "TEST_META_READ_ONLY_KEY"
	if _, err := config.SetCredential(key, "saved-meta-key"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key, "existing-environment")
	cfg := config.Default()
	cfg.DefaultModel = "custom/vision"
	cfg.Agent.VisionModel = "custom/vision"
	cfg.Providers = []config.ProviderEntry{{
		Name: "custom", Kind: "openai", BaseURL: "https://example.invalid/v1",
		APIKeyEnv: key, Models: []string{"vision"}, VisionModels: []string{"vision"},
	}}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	tab := &WorkspaceTab{ID: "meta-read", WorkspaceRoot: t.TempDir(), model: cfg.DefaultModel}
	app.tabs[tab.ID] = tab
	app.activeTabID = tab.ID
	app.refreshTabMetaExtras(tab)
	if got := os.Getenv(key); got != "existing-environment" {
		t.Fatalf("metadata refresh rewrote process credential environment: %q", got)
	}
	extras := tab.metaExtras.Load()
	if extras == nil || !extras.imageInputEnabled || !extras.visionFallbackEnabled {
		t.Fatalf("credential-free metadata lost configured image capabilities: %+v", extras)
	}
}
