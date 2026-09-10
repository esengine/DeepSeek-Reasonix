package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func migratedSelectionConfig(t *testing.T) *Config {
	t.Helper()
	c := &Config{ConfigVersion: 9, Providers: []ProviderEntry{{
		Name: "go", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go",
		APIKeyEnv: "SELECTION_TEST_KEY", Models: []string{"deepseek-v4-flash", "deepseek-v4-pro"}, Default: "deepseek-v4-flash",
	}}}
	j, _ := planOpenCodeGoUpgrade(c)
	c.openCodeGoJournal = &j
	c.ConfigVersion = 10
	return c
}

func TestOpenCodeGoCurrentSelectionAfterConnectionEdit(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ProviderEntry)
	}{
		{"proxy", func(p *ProviderEntry) { p.NoProxy = true }},
		{"headers", func(p *ProviderEntry) { p.Headers = map[string]string{"X-User": "changed"} }},
		{"credential reference", func(p *ProviderEntry) { p.APIKeyEnv = "NEW_SELECTION_TEST_KEY" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := migratedSelectionConfig(t)
			tc.edit(&c.Providers[0])
			for _, ref := range []string{"go/deepseek-v4-flash", "go", "deepseek-v4-flash"} {
				if err := c.ModelReferenceError(ref); err != nil {
					t.Fatal(err)
				}
				if e, ok := c.ResolveModel(ref); !ok || e.Model != "deepseek-v4-flash" {
					t.Fatalf("current selection %q failed: %+v", ref, e)
				}
				if _, err := c.ResolveHistoricalModel(ref); err == nil || !strings.Contains(err.Error(), "MIGRATED_MODEL_UNAVAILABLE") {
					t.Fatalf("historical identity was not protected: %v", err)
				}
			}
		})
	}
}

func TestOpenCodeGoCurrentDefaultAndHistoricalDefault(t *testing.T) {
	c := migratedSelectionConfig(t)
	c.Providers[0].Default = "deepseek-v4-pro"
	if e, ok := c.ResolveModel("go"); !ok || e.Model != "deepseek-v4-pro" {
		t.Fatalf("current default ignored: %+v", e)
	}
	if ref, err := c.ResolveHistoricalModel("go"); err != nil || ref != "go/deepseek-v4-flash" {
		t.Fatalf("historical default changed: %q, %v", ref, err)
	}
	c.Providers[0].Models = []string{"deepseek-v4-pro"}
	if e, ok := c.ResolveModel("go"); !ok || e.Model != "deepseek-v4-pro" {
		t.Fatalf("deleted old default blocked new default: %+v", e)
	}
	if _, err := c.ResolveHistoricalModel("go"); err == nil {
		t.Fatal("deleted historical model must fail closed")
	}
}

func TestOpenCodeGoSelectionEditSaveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`config_version = 9
[[providers]]
name = "go"
kind = "anthropic"
base_url = "https://opencode.ai/zen/go"
api_key_env = "SELECTION_TEST_KEY"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	c := LoadForEdit(path)
	if c.openCodeGoJournal == nil {
		t.Fatal("missing durable migration journal")
	}
	c.Providers[0].NoProxy = true
	c.Providers[0].Default = "deepseek-v4-pro"
	if err := c.SaveToScope(path, RenderScopeFull); err != nil {
		t.Fatal(err)
	}
	c = LoadForEdit(path)
	if e, ok := c.ResolveModel("go"); !ok || e.Model != "deepseek-v4-pro" || !e.NoProxy {
		t.Fatalf("selection edit did not survive reload: %+v", e)
	}
	if _, err := c.ResolveHistoricalModel("go"); err == nil {
		t.Fatal("save erased the historical identity guard")
	}
}

func TestOpenCodeGoExplicitSelectionIdentity(t *testing.T) {
	c := migratedSelectionConfig(t)
	c.Providers[0].NoProxy = true
	ref := "go/deepseek-v4-flash"
	identity := c.ModelSelectionIdentity(ref)
	if len(identity) != 64 || strings.Contains(identity, "SELECTION_TEST_KEY") {
		t.Fatal("selection must contain only an identity digest")
	}
	if _, err := c.ResolveSavedModel(ref, ""); err == nil {
		t.Fatal("legacy selection adopted edited connection")
	}
	if got, err := c.ResolveSavedModel(ref, identity); err != nil || got != ref {
		t.Fatalf("acknowledged selection failed: %q, %v", got, err)
	}
	c.Providers[0].Default = "deepseek-v4-pro"
	if _, err := c.ResolveSavedModel(ref, identity); err != nil {
		t.Fatalf("default edit changed explicit model identity: %v", err)
	}
	c.Providers[0].APIKeyEnv = "OTHER_KEY"
	if _, err := c.ResolveSavedModel(ref, identity); err == nil {
		t.Fatal("acknowledgement silently followed a later account edit")
	}
}

func TestOpenCodeGoAutomaticSearchNeverUsesAnotherAccountAfterEdit(t *testing.T) {
	c := migratedSelectionConfig(t)
	c.Providers[0].NoProxy = true
	c.Providers = append(c.Providers, ProviderEntry{Name: "other", Kind: "anthropic", BaseURL: "https://opencode.ai/zen/go", Model: "deepseek-v4-flash", APIKeyEnv: "OTHER_KEY", WebSearch: boolPointer(true)})
	for i := range c.Providers {
		c.Providers[i].resolvedAPIKey = "test-key"
	}
	chat, _ := c.ResolveModel("go/deepseek-v4-flash")
	if got := c.ResolveWebSearch(chat); got.Entry != nil {
		t.Fatalf("automatic search crossed accounts: %s", got.Entry.Name)
	}
	if got, err := c.ResolveWebSearchModel("other/deepseek-v4-flash"); err != nil || got.Name != "other" {
		t.Fatalf("explicit search choice blocked: %v", err)
	}
}
