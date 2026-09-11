package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrozenModelCredentialsIncludeMissingValue(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	c := Default()
	c.Providers = []ProviderEntry{{Name: "test", APIKeyEnv: "MODEL_SNAPSHOT_KEY", Model: "m"}}
	c.FreezeProviderCredentials()
	before := c.ModelRuntimeFingerprint("test/m")
	if err := os.WriteFile(UserCredentialsPath(), []byte("MODEL_SNAPSHOT_KEY=later-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, _ := c.ResolveModel("test/m")
	if entry.APIKey() != "" {
		t.Fatal("frozen absent credential read a newer value")
	}
	if before != c.ModelRuntimeFingerprint("test/m") {
		t.Fatal("frozen fingerprint changed")
	}
	next := Default()
	next.Providers = []ProviderEntry{{Name: "test", APIKeyEnv: "MODEL_SNAPSHOT_KEY", Model: "m"}}
	next.FreezeProviderCredentials()
	if next.ModelRuntimeFingerprint("test/m") == before {
		t.Fatal("new snapshot did not observe rotation")
	}
}

func TestModelRuntimeFingerprintIgnoresNewSessionDefault(t *testing.T) {
	c := Default()
	c.FreezeProviderCredentials()
	before := c.ModelRuntimeFingerprint("deepseek/a")
	c.DefaultModel = "other/model"
	if c.ModelRuntimeFingerprint("deepseek/a") != before {
		t.Fatal("global default invalidated an existing session")
	}
	c.Agent.PlannerModel = "other/model"
	if c.ModelRuntimeFingerprint("deepseek/a") == before {
		t.Fatal("planner change not detected")
	}
}

func TestSaveModelSettingsPreservesUnknownFields(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	raw := `config_version = 5
future_root = "keep"
[agent]
future_agent = "keep-agent"
planner_model = ""
[[providers]]
name = "custom"
kind = "openai"
base_url = "http://localhost:1234/v1"
model = "m"
future_provider = "keep-provider"
[providers.future_nested]
value = "keep-nested"
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	unlock := LockUserConfigEdits()
	defer unlock()
	c, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	baseline := c.ModelSettingsBaseline()
	c.Agent.PlannerModel = "custom/m"
	for i := range c.Providers {
		if c.Providers[i].Name == "custom" {
			c.Providers[i].BaseURL = "http://localhost:5678/v1"
		}
	}
	if err := c.SaveModelSettingsTo(path, baseline); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"keep", "keep-agent", "keep-provider", "keep-nested", "5678"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("lost %s", want)
		}
	}
}
