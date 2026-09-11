package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

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
