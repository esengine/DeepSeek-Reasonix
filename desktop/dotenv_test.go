package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

// TestUpsertEnvFile proves a new key is appended, an existing key is replaced in
// place, comments/other lines survive, and the process env is updated.
func TestUpsertEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("# comment\nFOO=old\nBAR=keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := upsertEnvFile(path, "FOO", "new"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := upsertEnvFile(path, "BAZ", "added"); err != nil {
		t.Fatalf("append: %v", err)
	}

	b, _ := os.ReadFile(path)
	got := string(b)
	for _, want := range []string{"# comment", "FOO=new", "BAR=keep", "BAZ=added"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "FOO=old") {
		t.Errorf("old value should be replaced:\n%s", got)
	}
	if os.Getenv("FOO") != "new" || os.Getenv("BAZ") != "added" {
		t.Errorf("process env not updated: FOO=%q BAZ=%q", os.Getenv("FOO"), os.Getenv("BAZ"))
	}
}

func TestRemoveEnvFileDeletesKeyAndUnsetsProcessEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("# comment\nFOO=old\nexport BAR=remove\nBAZ=keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BAR", "remove")

	if err := removeEnvFile(path, "BAR"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	b, _ := os.ReadFile(path)
	got := string(b)
	for _, want := range []string{"# comment", "FOO=old", "BAZ=keep"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "BAR=") {
		t.Errorf("removed key should be absent:\n%s", got)
	}
	if _, ok := os.LookupEnv("BAR"); ok {
		t.Errorf("process env BAR should be unset")
	}
}

// TestPromoteProviderKeysLiftsHomeEnvKey proves a provider key left in the
// legacy ~/.env bridge is copied into Reasonix's global .env, removed from
// ~/.env, and that unrelated env vars are ignored.
func TestPromoteProviderKeysLiftsHomeEnvKey(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	homeEnv := filepath.Join(home, ".env")
	if err := os.WriteFile(homeEnv, []byte("DEEPSEEK_API_KEY=sk-test\nNPM_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	promoteProviderKeysToCredentials(config.Default())

	cred, err := os.ReadFile(config.UserCredentialsPath())
	if err != nil {
		t.Fatalf("credentials not written: %v", err)
	}
	if !strings.Contains(string(cred), "DEEPSEEK_API_KEY=sk-test") {
		t.Errorf("provider key not promoted to credentials:\n%s", cred)
	}
	if strings.Contains(string(cred), "NPM_TOKEN") {
		t.Errorf("non-provider env var must not be promoted:\n%s", cred)
	}

	rest, _ := os.ReadFile(homeEnv)
	if strings.Contains(string(rest), "DEEPSEEK_API_KEY") {
		t.Errorf("promoted key must be stripped from ~/.env:\n%s", rest)
	}
	if !strings.Contains(string(rest), "NPM_TOKEN=secret") {
		t.Errorf("unrelated ~/.env line must survive:\n%s", rest)
	}
}

func TestPromoteProviderKeysIgnoresInheritedEnv(t *testing.T) {
	isolateDesktopUserDirs(t)
	t.Setenv("DEEPSEEK_API_KEY", "sk-inherited")

	promoteProviderKeysToCredentials(config.Default())

	if data, err := os.ReadFile(config.UserCredentialsPath()); err == nil && strings.Contains(string(data), "DEEPSEEK_API_KEY") {
		t.Errorf("inherited env var must not be promoted:\n%s", data)
	}
}

// TestPromoteProviderKeysLeavesExistingCredentialsKey proves promotion never
// overwrites a key already in Reasonix's global .env and leaves ~/.env untouched.
func TestPromoteProviderKeysLeavesExistingCredentialsKey(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserCredentialsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserCredentialsPath(), []byte("DEEPSEEK_API_KEY=sk-global\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	homeEnv := filepath.Join(home, ".env")
	if err := os.WriteFile(homeEnv, []byte("DEEPSEEK_API_KEY=sk-stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEEPSEEK_API_KEY", "sk-global")

	promoteProviderKeysToCredentials(config.Default())

	cred, _ := os.ReadFile(config.UserCredentialsPath())
	if !strings.Contains(string(cred), "DEEPSEEK_API_KEY=sk-global") {
		t.Errorf("existing credentials key was changed:\n%s", cred)
	}
	if data, err := os.Stat(homeEnv); err != nil || data.Size() == 0 {
		t.Errorf("~/.env should be untouched when key already global, err=%v", err)
	}
}
