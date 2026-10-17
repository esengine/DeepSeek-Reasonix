package control

import (
	"errors"
	"os"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestPersonaSettingsReadWriteAndValidation(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := config.UserConfigPath()
	raw := "# user note\n[agent]\ntemperature = 0.3\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c := New(Options{})
	defer c.Close()

	got := c.PersonaSettings()
	if got.Path != path || got.Prefix != "" || got.Suffix != "" || got.Complete || !got.IncludeRuntimeContext {
		t.Fatalf("unset persona reads %+v", got)
	}
	if len(got.Presets) == 0 {
		t.Fatal("GET served no presets")
	}

	if err := c.SavePersonaSettings(config.PersonaConfig{Complete: true}); !errors.Is(err, config.ErrPersonaCompleteRequiresPrefix) {
		t.Fatalf("complete without prefix = %v", err)
	}
	if onDisk, _ := os.ReadFile(path); string(onDisk) != raw {
		t.Fatal("a refused save changed the user config")
	}

	off := false
	if err := c.SavePersonaSettings(config.PersonaConfig{Prefix: "  IDENTITY  ", Suffix: "TAIL", IncludeRuntimeContext: &off}); err != nil {
		t.Fatal(err)
	}
	saved := c.PersonaSettings()
	if saved.Prefix != "IDENTITY" || saved.Suffix != "TAIL" || saved.Complete || saved.IncludeRuntimeContext {
		t.Fatalf("saved persona = %+v, want normalized values", saved)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# user note", "[agent.persona]", "include_runtime_context = false", "prefix = "} {
		if !strings.Contains(string(onDisk), want) {
			t.Fatalf("user config lacks %q:\n%s", want, onDisk)
		}
	}
}
