package main

import (
	"github.com/BurntSushi/toml"
	"os"
	"path/filepath"
	"reasonix/internal/config"
	"testing"
)

func TestProviderDisplayNamePreservesIdentity(t *testing.T) {
	c := &config.Config{Providers: []config.ProviderEntry{{Name: "stable", Kind: "openai", BaseURL: "https://example.com/v1", Models: []string{"model"}, APIKeyEnv: "TEST_KEY"}}}
	c.DefaultModel = "stable/model"
	view := providerViewFromEntry(c.Providers[0], false, true)
	label := "工作账号"
	view.DisplayName = &label
	if err := saveProviderConfig(c, view); err != nil {
		t.Fatal(err)
	}
	if len(c.Providers) != 1 || c.Providers[0].Name != "stable" || c.DefaultModel != "stable/model" || c.Providers[0].DisplayName != label {
		t.Fatalf("identity or label changed: %+v", c.Providers)
	}
	var decoded config.Config
	if _, err := toml.Decode(config.RenderTOML(c), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Providers) == 0 || decoded.Providers[0].DisplayName != label {
		t.Fatal("display name lost in TOML")
	}
	view.DisplayName = nil // An older frontend sends no displayName.
	if err := saveProviderConfig(c, view); err != nil {
		t.Fatal(err)
	}
	if c.Providers[0].DisplayName != label {
		t.Fatal("old client erased label")
	}
	empty := ""
	view.DisplayName = &empty
	if err := saveProviderConfig(c, view); err != nil {
		t.Fatal(err)
	}
	if c.Providers[0].DisplayName != "" {
		t.Fatal("explicit clear not saved")
	}
}

func TestRenameConnectionOnlyChangesLabel(t *testing.T) {
	c := &config.Config{Providers: []config.ProviderEntry{{Name: "stable", DisplayName: "old", Kind: "openai", BaseURL: "https://example.com/v1", Models: []string{"m"}, APIKeyEnv: "TEST_KEY"}}}
	original := c.Providers[0]
	if err := renameProviderConnections(c, []string{"stable", "missing"}, "new"); err == nil {
		t.Fatal("missing connection accepted")
	}
	if c.Providers[0].DisplayName != "old" {
		t.Fatal("partial mutation")
	}
	if err := renameProviderConnections(c, []string{"stable"}, " new "); err != nil {
		t.Fatal(err)
	}
	renamed := c.Providers[0]
	renamed.DisplayName = original.DisplayName
	if !config.ProviderEntriesConfigEqual(renamed, original) {
		t.Fatal("rename changed configuration")
	}
	if c.Providers[0].DisplayName != "new" {
		t.Fatal("label not saved")
	}
	oldDraft := providerViewFromEntry(original, false, true)
	oldDraft.DisplayName = nil
	if err := saveProviderConfig(c, oldDraft); err != nil {
		t.Fatal(err)
	}
	if c.Providers[0].DisplayName != "new" {
		t.Fatal("configuration draft overwrote name")
	}
}

func TestBuiltinProviderRemainsEditableAfterReload(t *testing.T) {
	c := &config.Config{Providers: []config.ProviderEntry{{Name: "deepseek", Kind: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", Models: []string{"deepseek-v4-flash"}, APIKeyEnv: "DEEPSEEK_API_KEY"}}}
	view := providerViewFromEntry(c.Providers[0], true, true)
	view.Kind = "responses"
	view.BaseURL = "https://gateway.example/v1"
	view.RequestURL = "https://gateway.example/v1/responses"
	view.Models = []string{"custom-model"}
	view.Default = "custom-model"
	if err := saveProviderConfig(c, view); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(config.RenderTOML(c)), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadForEditWithoutCredentialsReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := got.Provider("deepseek")
	if !ok || entry.Kind != "responses" || entry.BaseURL != view.BaseURL || entry.RequestURL != view.RequestURL || len(entry.Models) != 1 || entry.Models[0] != "custom-model" {
		t.Fatalf("customized builtin reset after reload: %+v", entry)
	}
}
