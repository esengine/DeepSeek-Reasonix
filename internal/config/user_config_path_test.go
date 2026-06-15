package config

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUserConfigDirFallsBackToHomeDotConfigOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("the explicit ~/.config fallback is for Linux/Unix XDG config paths")
	}
	home := t.TempDir()
	oldUserConfigDir := osUserConfigDir
	oldUserHomeDir := osUserHomeDir
	t.Cleanup(func() {
		osUserConfigDir = oldUserConfigDir
		osUserHomeDir = oldUserHomeDir
	})
	osUserConfigDir = func() (string, error) {
		return "", errors.New("config dir unavailable")
	}
	osUserHomeDir = func() (string, error) {
		return home, nil
	}

	base := filepath.Join(home, ".config", "reasonix")
	if got := UserConfigPath(); got != filepath.Join(base, "config.toml") {
		t.Fatalf("UserConfigPath() = %q, want %q", got, filepath.Join(base, "config.toml"))
	}
	if got := UserCredentialsPath(); got != filepath.Join(base, "credentials") {
		t.Fatalf("UserCredentialsPath() = %q, want %q", got, filepath.Join(base, "credentials"))
	}
	if got := ArchiveDir(); got != filepath.Join(base, "archive") {
		t.Fatalf("ArchiveDir() = %q, want %q", got, filepath.Join(base, "archive"))
	}
	if got := SessionDir(); got != filepath.Join(base, "sessions") {
		t.Fatalf("SessionDir() = %q, want %q", got, filepath.Join(base, "sessions"))
	}
	if got := CacheDir(); got != filepath.Join(base, "cache") {
		t.Fatalf("CacheDir() = %q, want %q", got, filepath.Join(base, "cache"))
	}
	if got := MemoryUserDir(); got != base {
		t.Fatalf("MemoryUserDir() = %q, want %q", got, base)
	}
}
