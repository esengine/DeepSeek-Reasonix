package config

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateTestCredentials(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
}

func setTestCredential(t *testing.T, key, value string) {
	t.Helper()
	if _, err := SetCredential(key, value); err != nil {
		t.Fatalf("SetCredential(%s): %v", key, err)
	}
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s): %v", key, err)
	}
}
