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

func TestLegacyHomeEnvProviderKeyIsNotPromoted(t *testing.T) {
	home := isolateDesktopUserDirs(t)
	homeEnv := filepath.Join(home, ".env")
	if err := os.WriteFile(homeEnv, []byte("DEEPSEEK_API_KEY=sk-test\nNPM_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := config.LoadForRoot(t.TempDir()); err != nil {
		t.Fatalf("LoadForRoot: %v", err)
	}
	if data, err := os.ReadFile(config.UserCredentialsPath()); err == nil && strings.Contains(string(data), "DEEPSEEK_API_KEY") {
		t.Errorf("legacy ~/.env provider key must not be imported:\n%s", data)
	}
	rest, _ := os.ReadFile(homeEnv)
	if !strings.Contains(string(rest), "DEEPSEEK_API_KEY=sk-test") || !strings.Contains(string(rest), "NPM_TOKEN=secret") {
		t.Errorf("legacy ~/.env should be left untouched:\n%s", rest)
	}
}
