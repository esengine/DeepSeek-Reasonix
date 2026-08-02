package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAndWriteRefusesConcurrentModification(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	body, err := renderTOMLForScopeErr(Default(), RenderScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stateID, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	// Another process modifies the file between edit-read and write.
	if err := os.WriteFile(path, []byte("default_model = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := writeConfigOptions{scope: RenderScopeUser}
	err = validateAndWriteConfigResolved(path, body, 0o600, opts, stateID)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected concurrent-change error, got %v", err)
	}
	// The concurrent content survives.
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "other") {
		t.Errorf("concurrent write was overwritten: %q", b)
	}
}

func TestValidateAndWriteRequiresParseableOutput(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	// A body with an invalid TOML escape must never be written.
	err := validateAndWriteConfigResolved(path, "command = \"D:\\开发\\x.exe\"\n", 0o600, writeConfigOptions{scope: RenderScopeUser}, "")
	if err == nil {
		t.Fatal("write of unparseable config succeeded")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("file was created despite validation failure: %v", statErr)
	}
}

func TestConfigFileStateIDChangesWithContent(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("a = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("a = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("state ID did not change with content")
	}
	absent, err := configFileStateID(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if absent != "absent" {
		t.Fatalf("missing file state = %q, want absent", absent)
	}
}

func TestValidateAndWriteDetectsSilentlyDroppedField(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	// A body whose decoded config does NOT match the intended config (the
	// renderer "forgot" default_model) must be rejected by the persisted
	// semantics comparison.
	want := Default()
	want.DefaultModel = "deepseek-pro"
	body, err := renderTOMLForScopeErr(want, RenderScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a renderer dropping the field: strip default_model from the body.
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "default_model") {
			continue
		}
		kept = append(kept, line)
	}
	opts := writeConfigOptions{scope: RenderScopeUser, want: want}
	err = validateAndWriteConfigResolved(path, strings.Join(kept, "\n"), 0o600, opts, "")
	if err == nil || !strings.Contains(err.Error(), "default_model") {
		t.Fatalf("dropped-field body accepted: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("dropped-field body was written")
	}
}
