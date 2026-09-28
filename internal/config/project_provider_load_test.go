package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadForRootKeepsOfficialProviderAliasesDistinct(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
config_version = 2
default_model = "deepseek/deepseek-v4-flash"

[desktop]
provider_access = ["deepseek"]

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "USER_DEEPSEEK_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://api.deepseek.com"
model = "deepseek-v4-flash"
api_key_env = "PROJECT_DEEPSEEK_KEY"
effort = "max"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, root)

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	userProvider, ok := cfg.Provider("deepseek")
	if !ok {
		t.Fatalf("user deepseek provider missing: %+v", cfg.Providers)
	}
	if userProvider.APIKeyEnv != "USER_DEEPSEEK_KEY" {
		t.Fatalf("deepseek provider = %+v, want user provider preserved", userProvider)
	}
	projectProvider, ok := cfg.Provider("deepseek-flash")
	if !ok {
		t.Fatalf("project deepseek-flash provider missing: %+v", cfg.Providers)
	}
	if projectProvider.APIKeyEnv != "PROJECT_DEEPSEEK_KEY" || projectProvider.Effort != "max" {
		t.Fatalf("deepseek-flash provider = %+v, want project provider preserved", projectProvider)
	}
}

func TestLoadForRootKeepsUserProviderOverSameNamedProjectProvider(t *testing.T) {
	isolateUserConfigHome(t)
	root := t.TempDir()
	userPath := UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
[[providers]]
name = "shared"
kind = "openai"
base_url = "https://global.example/v1"
model = "global-model"
api_key_env = "GLOBAL_SHARED_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[[providers]]
name = "shared"
kind = "openai"
base_url = "https://project.example/v1"
model = "project-model"
api_key_env = "PROJECT_SHARED_KEY"

[[providers]]
name = "project-only"
kind = "openai"
base_url = "https://project.example/v1"
model = "project-only-model"
api_key_env = "PROJECT_ONLY_KEY"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, root)

	cfg, err := LoadForRoot(root)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	shared, ok := cfg.Provider("shared")
	if !ok {
		t.Fatalf("shared provider missing: %+v", cfg.Providers)
	}
	if shared.BaseURL != "https://global.example/v1" || shared.APIKeyEnv != "GLOBAL_SHARED_KEY" || shared.Model != "global-model" {
		t.Fatalf("shared provider = %+v, want global provider to win over project provider", shared)
	}
	if _, ok := cfg.Provider("project-only"); !ok {
		t.Fatalf("project-only provider missing: %+v", cfg.Providers)
	}
}

func TestLoadForRootResolvesProviderCredentialsOverInheritedEnv(t *testing.T) {
	project := t.TempDir()
	cfgHome := t.TempDir()
	key := "KEY_PROVIDER_GLOBAL_PRIORITY"

	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))
	t.Setenv(key, "from_env")

	cred := UserCredentialsPath()
	if cred == "" {
		t.Skip("user config dir unresolved on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(cred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte(key+"=from_credentials\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "custom/m"
[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "m"
api_key_env = "`+key+`"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	provider, ok := cfg.Provider("custom")
	if !ok {
		t.Fatalf("provider missing: %+v", cfg.Providers)
	}
	if got := provider.APIKey(); got != "from_credentials" {
		t.Fatalf("provider API key = %q, want credentials value", got)
	}
	if got := os.Getenv(key); got != "from_credentials" {
		t.Fatalf("process env = %q, want credentials value pinned over inherited env", got)
	}
}

func TestLoadForRootIgnoresProjectProviderEnvAndInheritedEnv(t *testing.T) {
	project := t.TempDir()
	cfgHome := t.TempDir()
	key := "KEY_PROVIDER_PROJECT_PRIORITY"

	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))
	t.Setenv(key, "from_env")

	if err := os.WriteFile(filepath.Join(project, ".env"), []byte(key+"=from_project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`
default_model = "custom/m"
[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.invalid/v1"
model = "m"
api_key_env = "`+key+`"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	approveWorkspace(t, project)

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	provider, ok := cfg.Provider("custom")
	if !ok {
		t.Fatalf("provider missing: %+v", cfg.Providers)
	}
	if got := provider.APIKey(); got != "" {
		t.Fatalf("provider API key = %q, want no key without global credentials", got)
	}
	if got := os.Getenv(key); got != "from_env" {
		t.Fatalf("process env = %q, want inherited env left untouched", got)
	}
}
