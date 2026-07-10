package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestLoadDotEnvDoesNotImportProjectOrHomeEnv(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()

	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("KEY_CWD=from_cwd\nKEY_SHARED=cwd_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("KEY_HOME=from_home\nKEY_SHARED=home_loses\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(cwd)
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.

	// Start clean so the file values are what land (Setenv auto-restores).
	t.Setenv("KEY_CWD", "")
	os.Unsetenv("KEY_CWD")
	t.Setenv("KEY_HOME", "")
	os.Unsetenv("KEY_HOME")
	t.Setenv("KEY_SHARED", "")
	os.Unsetenv("KEY_SHARED")

	loadDotEnv()

	if got := os.Getenv("KEY_CWD"); got != "" {
		t.Errorf("project .env key was imported into process env: KEY_CWD=%q", got)
	}
	if got := os.Getenv("KEY_HOME"); got != "" {
		t.Errorf("home .env key was loaded: KEY_HOME=%q", got)
	}
	if got := os.Getenv("KEY_SHARED"); got != "" {
		t.Errorf("project/home .env shared key was imported: KEY_SHARED=%q", got)
	}
}

// TestLoadDotEnvReadsGlobalCredentials proves `reasonix setup`'s target — the
// reasonix-owned credentials file under Reasonix home — is loaded from any
// working directory and wins over a project ./.env on a shared key.
func TestLoadDotEnvReadsGlobalCredentials(t *testing.T) {
	cwd := t.TempDir()
	cfgHome := t.TempDir()

	t.Chdir(cwd)
	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))

	cred := UserCredentialsPath()
	if cred == "" {
		t.Skip("user config dir unresolved on this platform")
	}
	if err := os.MkdirAll(filepath.Dir(cred), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, []byte("KEY_GLOBAL=from_credentials\nKEY_SHARED=global_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("KEY_SHARED=cwd_wins\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("KEY_GLOBAL", "")
	os.Unsetenv("KEY_GLOBAL")
	t.Setenv("KEY_SHARED", "")
	os.Unsetenv("KEY_SHARED")

	loadDotEnv()

	if got := os.Getenv("KEY_GLOBAL"); got != "from_credentials" {
		t.Errorf("global credentials not loaded: KEY_GLOBAL=%q want from_credentials", got)
	}
	if got := os.Getenv("KEY_SHARED"); got != "global_wins" {
		t.Errorf("global credentials should win over project .env: KEY_SHARED=%q want global_wins", got)
	}
}

func TestLoadDotEnvDecodesGB18030Credentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "reasonix-home"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("PINNED_CN", "")
	os.Unsetenv("PINNED_CN")

	cred := UserCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(cred), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cred, fileencoding.Encode("PINNED_CN=中文\n", fileencoding.GB18030), 0o600); err != nil {
		t.Fatal(err)
	}

	loadDotEnv()
	if got := os.Getenv("PINNED_CN"); got != "中文" {
		t.Fatalf("PINNED_CN = %q, want decoded Chinese value", got)
	}
}

func TestLoadForRootExpandsPluginAuthFromProjectDotEnv(t *testing.T) {
	project := t.TempDir()
	cfgHome := t.TempDir()

	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))
	t.Setenv("STRIPE_KEY", "")
	os.Unsetenv("STRIPE_KEY")

	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("STRIPE_KEY=project-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(`{
  "mcpServers": {
    "stripe": {
      "type": "http",
      "url": "https://mcp.example.test",
      "headers": { "Authorization": "Bearer ${STRIPE_KEY}" }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("plugins = %+v", cfg.Plugins)
	}
	got := cfg.Plugins[0].ExpandedPlugin().Headers["Authorization"]
	if got != "Bearer project-token" {
		t.Fatalf("expanded auth header = %q, want project token", got)
	}
	if got := os.Getenv("STRIPE_KEY"); got != "" {
		t.Fatalf("project .env leaked into process env: STRIPE_KEY=%q", got)
	}
}

func TestProjectDotEnvUsesDotenvSyntax(t *testing.T) {
	project := t.TempDir()
	cfgHome := t.TempDir()

	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))
	t.Setenv("PLUGIN_TOKEN", "")
	os.Unsetenv("PLUGIN_TOKEN")

	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("export PLUGIN_TOKEN='project token # kept'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(`{
  "mcpServers": {
    "svc": {
      "type": "http",
      "url": "https://mcp.example.test",
      "headers": { "Authorization": "Bearer ${PLUGIN_TOKEN}" }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	got := cfg.Plugins[0].ExpandedPlugin().Headers["Authorization"]
	if got != "Bearer project token # kept" {
		t.Fatalf("expanded auth header = %q", got)
	}
	if got := os.Getenv("PLUGIN_TOKEN"); got != "" {
		t.Fatalf("project .env leaked into process env: %q", got)
	}
}

func TestDotEnvFileWarningsReportDuplicateKeysWithoutValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("TOKEN=secret-one\nOTHER=ok\nexport TOKEN='secret two'\nOTHER=last\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, ok := readDotEnvFile(path)
	if !ok {
		t.Fatal("readDotEnvFile failed")
	}
	if got := strings.Join(file.Duplicates, ","); got != "OTHER,TOKEN" {
		t.Fatalf("Duplicates = %#v", file.Duplicates)
	}
	warnings := strings.Join(file.warnings(), "\n")
	for _, want := range []string{"duplicate .env key OTHER", "duplicate .env key TOKEN", "last parsed value wins"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("warnings missing %q:\n%s", want, warnings)
		}
	}
	for _, leak := range []string{"secret-one", "secret two"} {
		if strings.Contains(warnings, leak) {
			t.Fatalf("warnings leaked secret value %q:\n%s", leak, warnings)
		}
	}
}

func TestDotEnvFileFilteredPreservesProjectScopeRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PLUGIN_TOKEN=project\nREASONIX_HOME=blocked\nHOME=blocked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, ok := readDotEnvFile(path)
	if !ok {
		t.Fatal("readDotEnvFile failed")
	}
	got := file.filtered(func(key string) bool { return !isProjectDotEnvControlKey(key) })
	if got["PLUGIN_TOKEN"] != "project" {
		t.Fatalf("PLUGIN_TOKEN = %q", got["PLUGIN_TOKEN"])
	}
	if _, ok := got["REASONIX_HOME"]; ok {
		t.Fatalf("REASONIX_HOME should be filtered: %+v", got)
	}
	if _, ok := got["HOME"]; ok {
		t.Fatalf("HOME should be filtered: %+v", got)
	}
}

func TestLoadForRootScopesProjectDotEnvPerWorkspace(t *testing.T) {
	home := t.TempDir()
	projectA := t.TempDir()
	projectB := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("SHARED_TOKEN", "")
	os.Unsetenv("SHARED_TOKEN")

	for _, tc := range []struct {
		root  string
		value string
	}{
		{projectA, "token-a"},
		{projectB, "token-b"},
	} {
		if err := os.WriteFile(filepath.Join(tc.root, ".env"), []byte("SHARED_TOKEN="+tc.value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tc.root, ".mcp.json"), []byte(`{
  "mcpServers": {
    "svc": {
      "type": "http",
      "url": "https://mcp.example.test",
      "headers": { "Authorization": "Bearer ${SHARED_TOKEN}" }
    }
  }
}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cfgA, err := LoadForRoot(projectA)
	if err != nil {
		t.Fatalf("LoadForRoot A: %v", err)
	}
	cfgB, err := LoadForRoot(projectB)
	if err != nil {
		t.Fatalf("LoadForRoot B: %v", err)
	}
	if got := cfgA.Plugins[0].ExpandedPlugin().Headers["Authorization"]; got != "Bearer token-a" {
		t.Fatalf("project A auth = %q, want token-a", got)
	}
	if got := cfgB.Plugins[0].ExpandedPlugin().Headers["Authorization"]; got != "Bearer token-b" {
		t.Fatalf("project B auth = %q, want token-b", got)
	}
	if got := os.Getenv("SHARED_TOKEN"); got != "" {
		t.Fatalf("project token leaked into process env: %q", got)
	}
}

func TestLoadForRootFiltersProjectDotEnvControlVars(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	redirect := filepath.Join(project, "state")

	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_STATE_HOME", "")
	os.Unsetenv("REASONIX_STATE_HOME")

	wantCred := UserCredentialsPath()
	if wantCred == "" {
		t.Skip("user credentials path unavailable")
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("REASONIX_STATE_HOME="+redirect+"\nPLUGIN_TOKEN=project-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(`{
  "mcpServers": {
    "svc": {
      "type": "http",
      "url": "https://mcp.example.test",
      "headers": {
        "Authorization": "Bearer ${PLUGIN_TOKEN}",
        "State": "${REASONIX_STATE_HOME:-default-state}"
      }
    }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	headers := cfg.Plugins[0].ExpandedPlugin().Headers
	if got := headers["Authorization"]; got != "Bearer project-token" {
		t.Fatalf("plugin token = %q, want project token", got)
	}
	if got := headers["State"]; got != "default-state" {
		t.Fatalf("control var expansion = %q, want default-state", got)
	}
	if got := UserCredentialsPath(); got != wantCred {
		t.Fatalf("project .env redirected credentials path: %q want %q", got, wantCred)
	}
	if got := os.Getenv("REASONIX_STATE_HOME"); got != "" {
		t.Fatalf("project control var leaked into process env: %q", got)
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

func TestResolveCredentialGlobalFirstDoesNotMutateProjectEnv(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	key := "KEY_GLOBAL_PRIORITY"
	if err := os.MkdirAll(filepath.Dir(UserCredentialsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserCredentialsPath(), []byte(key+"=from_credentials\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte(key+"=from_project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key, "")
	os.Unsetenv(key)
	t.Setenv(key, "from_project")

	if got := os.Getenv(key); got != "from_project" {
		t.Fatalf("precondition: existing env should be project value, got %q", got)
	}
	got := ResolveCredentialForRootGlobalFirst(project, key)
	if got.Value != "from_credentials" || got.Source.Kind != CredentialSourceCredentials {
		t.Fatalf("credential = %+v, want global credentials for settings display", got)
	}
	if env := os.Getenv(key); env != "from_project" {
		t.Fatalf("global-first resolution mutated process env: %q", env)
	}
}

func TestCredentialResolverCachesGlobalFirstLookups(t *testing.T) {
	project := t.TempDir()
	key := "KEY_SETTINGS_CACHE"
	calls := 0
	stubStoredCredentialValueForTest(t, func(got string) (string, CredentialSource, bool) {
		calls++
		if got != key {
			t.Fatalf("stored credential lookup key = %q, want %q", got, key)
		}
		return "from_credentials", CredentialSource{Kind: CredentialSourceCredentials, Label: "Reasonix credentials"}, true
	})

	resolver := NewCredentialResolverForRoot(project)
	first := resolver.ResolveGlobalFirst(key)
	second := resolver.ResolveGlobalFirst(key)

	if !first.Set || first.Value != "from_credentials" {
		t.Fatalf("first credential = %+v, want stored credential", first)
	}
	if !second.Set || second.Value != "from_credentials" {
		t.Fatalf("second credential = %+v, want cached stored credential", second)
	}
	if calls != 1 {
		t.Fatalf("stored credential lookups = %d, want 1 for repeated key in one resolver", calls)
	}
}

func stubStoredCredentialValueForTest(t *testing.T, fn func(string) (string, CredentialSource, bool)) {
	t.Helper()
	old := storedCredentialValueLookup
	storedCredentialValueLookup = fn
	t.Cleanup(func() {
		storedCredentialValueLookup = old
	})
}

func TestResolveCredentialSourceShowsCredentialsShadowingProjectEnv(t *testing.T) {
	cwd := t.TempDir()
	cfgHome := t.TempDir()

	t.Chdir(cwd)
	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))

	key := "KEY_SOURCE_PROJECT"
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
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte(key+"=from_project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key, "")
	os.Unsetenv(key)

	loadDotEnv()

	got := ResolveCredentialForRoot(cwd, key)
	if !got.Set || got.Source.Kind != CredentialSourceCredentials {
		t.Fatalf("source = %+v set=%v, want Reasonix credentials", got.Source, got.Set)
	}
	foundProjectShadow := false
	for _, source := range got.Shadowed {
		if source.Kind == CredentialSourceProjectEnv {
			foundProjectShadow = true
		}
	}
	if !foundProjectShadow {
		t.Fatalf("shadowed = %+v, want project .env shadowed by credentials", got.Shadowed)
	}
}

func TestResolveCredentialSourceShowsCredentialsShadowingEmptyProjectEnv(t *testing.T) {
	cwd := t.TempDir()
	cfgHome := t.TempDir()

	t.Chdir(cwd)
	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))

	key := "KEY_SOURCE_EMPTY_PROJECT"
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
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte(key+"=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key, "")
	os.Unsetenv(key)

	if _, err := StoreCredentialLines([]string{key + "=from_credentials"}); err != nil {
		t.Fatalf("StoreCredentialLines: %v", err)
	}

	got := ResolveCredentialForRoot(cwd, key)
	foundProjectShadow := false
	for _, source := range got.Shadowed {
		if source.Kind == CredentialSourceProjectEnv {
			foundProjectShadow = true
		}
	}
	if !foundProjectShadow {
		t.Fatalf("shadowed = %+v, want empty project .env shadowed by credentials", got.Shadowed)
	}
}

func TestResolveCredentialSourceShowsCredentialsBeforeHomeEnv(t *testing.T) {
	cwd := t.TempDir()
	cfgHome := t.TempDir()

	t.Chdir(cwd)
	t.Setenv("HOME", cfgHome)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgHome, ".config"))
	t.Setenv("AppData", filepath.Join(cfgHome, "AppData"))

	key := "KEY_SOURCE_CREDENTIALS"
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
	if err := os.WriteFile(filepath.Join(cfgHome, ".env"), []byte(key+"=from_home\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key, "")
	os.Unsetenv(key)

	loadDotEnv()

	got := ResolveCredentialForRoot(cwd, key)
	if !got.Set || got.Source.Kind != CredentialSourceCredentials {
		t.Fatalf("source = %+v set=%v, want Reasonix credentials", got.Source, got.Set)
	}
	if got.Value != "from_credentials" {
		t.Fatalf("value = %q, want credentials value", got.Value)
	}
}

func TestStoreCredentialLinesFileMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("KEY_FILE_MODE", "")
	os.Unsetenv("KEY_FILE_MODE")

	target, err := StoreCredentialLines([]string{"KEY_FILE_MODE=from_file_store"})
	if err != nil {
		t.Fatalf("StoreCredentialLines: %v", err)
	}
	if target != UserCredentialsPath() {
		t.Fatalf("target = %q, want %q", target, UserCredentialsPath())
	}
	data, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}
	if string(data) != "KEY_FILE_MODE=from_file_store\n" {
		t.Fatalf("credentials file = %q", data)
	}
	if got := os.Getenv("KEY_FILE_MODE"); got != "from_file_store" {
		t.Fatalf("process env = %q, want stored value", got)
	}
}

func TestUserCredentialsPathIgnoresReasonixStateHome(t *testing.T) {
	home := t.TempDir()
	state := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_HOME", filepath.Join(home, "reasonix-home"))
	t.Setenv("REASONIX_STATE_HOME", state)

	want := filepath.Join(home, "reasonix-home", ".env")
	if got := UserCredentialsPath(); got != want {
		t.Fatalf("UserCredentialsPath() = %q, want %q", got, want)
	}
	if strings.HasPrefix(UserCredentialsPath(), state) {
		t.Fatalf("credentials path must not live under REASONIX_STATE_HOME: %q", UserCredentialsPath())
	}
}

func TestRemoveCredentialMarksClearedAndSetRemovesMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	if _, err := SetCredential("KEY_REMOVE_MARKER", "old"); err != nil {
		t.Fatalf("SetCredential old: %v", err)
	}
	if err := RemoveCredential("KEY_REMOVE_MARKER"); err != nil {
		t.Fatalf("RemoveCredential: %v", err)
	}
	if CredentialStored("KEY_REMOVE_MARKER") {
		t.Fatal("removed key should not be stored")
	}
	if !credentialCurrentStoreClearedKey("KEY_REMOVE_MARKER") {
		t.Fatal("removed key should leave a cleared marker")
	}
	if _, err := SetCredential("KEY_REMOVE_MARKER", "new"); err != nil {
		t.Fatalf("SetCredential new: %v", err)
	}
	data, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials: %v", err)
	}
	if got := string(data); got != "KEY_REMOVE_MARKER=new\n" {
		t.Fatalf("credentials after re-set = %q", got)
	}
}

func TestStoreCredentialLinesRejectsUnsafeFileLines(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	_, err := StoreCredentialLines([]string{
		"VALID_KEY=kept",
		"BAD-KEY=ignored",
		"MULTILINE=first\nINJECTED=second",
	})
	if err != nil {
		t.Fatalf("StoreCredentialLines: %v", err)
	}
	data, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}
	if string(data) != "VALID_KEY=kept\n" {
		t.Fatalf("credentials file = %q", data)
	}
	if got := os.Getenv("INJECTED"); got != "" {
		t.Fatalf("injected env was set: %q", got)
	}
}

func TestStoreCredentialLinesParsesDotenvQuotes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	_, err := StoreCredentialLines([]string{`export QUOTED_KEY="value with spaces # kept"`})
	if err != nil {
		t.Fatalf("StoreCredentialLines: %v", err)
	}
	data, err := os.ReadFile(UserCredentialsPath())
	if err != nil {
		t.Fatalf("read credentials file: %v", err)
	}
	if string(data) != "QUOTED_KEY=\"value with spaces # kept\"\n" {
		t.Fatalf("credentials file = %q", data)
	}
	if got := os.Getenv("QUOTED_KEY"); got != "value with spaces # kept" {
		t.Fatalf("env QUOTED_KEY = %q", got)
	}
}

func TestSetCredentialRejectsInvalidInput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")

	if _, err := SetCredential("BAD-KEY", "value"); err == nil {
		t.Fatal("SetCredential accepted invalid key")
	}
	if _, err := SetCredential("VALID_KEY", "first\nsecond"); err == nil {
		t.Fatal("SetCredential accepted newline value")
	}
	if _, err := os.Stat(UserCredentialsPath()); !os.IsNotExist(err) {
		t.Fatalf("credentials file should not be created for rejected input, stat err=%v", err)
	}
}

func TestProjectConfigCannotOverrideCredentialStoreMode(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "")
	os.Unsetenv("REASONIX_CREDENTIALS_STORE")
	if err := os.MkdirAll(filepath.Dir(UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserConfigPath(), []byte(`credentials_store = "file"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(`credentials_store = "keyring"`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if cfg.CredentialsStore != CredentialsStoreFile {
		t.Fatalf("CredentialsStore = %q, want file from user config", cfg.CredentialsStore)
	}
}

// TestLoadDotEnvGlobalCredentialsOverrideEnv confirms Reasonix-owned global
// credentials beat inherited environment variables.
func TestLoadDotEnvGlobalCredentialsOverrideEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Setenv("PINNED", "from_env")
	if err := os.MkdirAll(filepath.Dir(UserCredentialsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserCredentialsPath(), []byte("PINNED=from_credentials\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	loadDotEnv()

	if got := os.Getenv("PINNED"); got != "from_credentials" {
		t.Errorf("global credentials must win over inherited env: PINNED=%q want from_credentials", got)
	}
}
