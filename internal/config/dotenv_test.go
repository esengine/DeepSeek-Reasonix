package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDotEnvFallsBackToHome proves the unified-key behaviour: the working
// directory's .env wins, but a key only present in ~/.env is still picked up —
// so a key set once in the home .env (the desktop app writes there) reaches the
// CLI run from any project directory. Existing env vars beat both files.
func TestLoadDotEnvFallsBackToHome(t *testing.T) {
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

	if got := os.Getenv("KEY_CWD"); got != "from_cwd" {
		t.Errorf("cwd-only key not loaded: KEY_CWD=%q", got)
	}
	if got := os.Getenv("KEY_HOME"); got != "from_home" {
		t.Errorf("~/.env fallback failed: KEY_HOME=%q want from_home", got)
	}
	if got := os.Getenv("KEY_SHARED"); got != "cwd_wins" {
		t.Errorf("cwd .env should take precedence over ~/.env: KEY_SHARED=%q want cwd_wins", got)
	}
}

// TestLoadDotEnvReadsGlobalCredentials proves `reasonix setup`'s target — the
// reasonix-owned credentials file under Reasonix home — is loaded from any
// working directory, while a project ./.env still wins on a shared key.
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
	if err := os.WriteFile(cred, []byte("KEY_GLOBAL=from_credentials\nKEY_SHARED=global_loses\n"), 0o600); err != nil {
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
	if got := os.Getenv("KEY_SHARED"); got != "cwd_wins" {
		t.Errorf("project .env should win over global credentials: KEY_SHARED=%q want cwd_wins", got)
	}
}

func TestResolveCredentialSourceShowsProjectEnvShadowingCredentials(t *testing.T) {
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
	if !got.Set || got.Source.Kind != CredentialSourceProjectEnv {
		t.Fatalf("source = %+v set=%v, want project .env", got.Source, got.Set)
	}
	foundCredentialsShadow := false
	for _, source := range got.Shadowed {
		if source.Kind == CredentialSourceCredentials {
			foundCredentialsShadow = true
		}
	}
	if !foundCredentialsShadow {
		t.Fatalf("shadowed = %+v, want credentials shadowed by project .env", got.Shadowed)
	}
}

func TestResolveCredentialSourceShowsEmptyProjectEnvShadowingCredentials(t *testing.T) {
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
		t.Fatalf("shadowed = %+v, want empty project .env shadowing credentials", got.Shadowed)
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

// TestLoadDotEnvDoesNotOverrideEnv confirms an already-set environment variable
// beats both .env files (the documented first-wins contract).
func TestLoadDotEnvDoesNotOverrideEnv(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".env"), []byte("PINNED=from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("PINNED", "from_env")

	loadDotEnv()

	if got := os.Getenv("PINNED"); got != "from_env" {
		t.Errorf("env var must win over .env: PINNED=%q want from_env", got)
	}
}
