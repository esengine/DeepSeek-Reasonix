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
	t.Setenv("USERPROFILE", home)
	t.Setenv("PINNED", "from_env")

	loadDotEnv()

	if got := os.Getenv("PINNED"); got != "from_env" {
		t.Errorf("env var must win over .env: PINNED=%q want from_env", got)
	}
}
