package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestPersonaNormalizeTrimsAndCollapsesOnlyTheEmptyChoice(t *testing.T) {
	zero := PersonaConfig{}.Normalize()
	if !reflect.DeepEqual(zero, PersonaConfig{}) {
		t.Fatalf("zero config normalized to %+v", zero)
	}
	blank := PersonaConfig{Prefix: " \n\t", Suffix: "  "}.Normalize()
	if !reflect.DeepEqual(blank, PersonaConfig{}) {
		t.Fatalf("blank bodies normalized to %+v, want zero", blank)
	}
	trimmed := PersonaConfig{Prefix: "  lead  ", Suffix: " tail \n"}.Normalize()
	if trimmed.Prefix != "lead" || trimmed.Suffix != "tail" {
		t.Fatalf("normalized = %+v", trimmed)
	}
	// An explicit runtime-context choice is a contribution by itself: the Studio
	// switch may be the only field the user touched, and collapsing it would make
	// the save read back as unchanged.
	pointerOnly := PersonaConfig{IncludeRuntimeContext: new(false)}.Normalize()
	if pointerOnly.IsZero() || pointerOnly.IncludeRuntimeContext == nil || *pointerOnly.IncludeRuntimeContext {
		t.Fatalf("pointer-only persona collapsed or lost: %+v", pointerOnly)
	}
	if (PersonaConfig{Complete: true}).IsZero() {
		t.Fatal("complete without a prefix must survive for repair, not read as absent")
	}
}

func TestPersonaValidateRejectsUnhonorableValues(t *testing.T) {
	tooBig := strings.Repeat("x", maxPersonaPartBytes)
	for _, tc := range []struct {
		name string
		p    PersonaConfig
		want error
	}{
		{"complete without prefix", PersonaConfig{Complete: true}, ErrPersonaCompleteRequiresPrefix},
		{"complete with blank prefix", PersonaConfig{Complete: true, Prefix: "   "}, ErrPersonaCompleteRequiresPrefix},
		{"prefix over cap", PersonaConfig{Prefix: tooBig + "x"}, ErrPersonaTooLarge},
		{"suffix over cap", PersonaConfig{Suffix: tooBig + "x"}, ErrPersonaTooLarge},
	} {
		if err := tc.p.Validate(); !errors.Is(err, tc.want) {
			t.Errorf("%s: Validate = %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := (PersonaConfig{Prefix: tooBig, Suffix: tooBig}).Validate(); err != nil {
		t.Fatalf("two parts at the cap = %v, want accepted", err)
	}
	var c Config
	if err := c.SetPersona(PersonaConfig{Complete: true}); !errors.Is(err, ErrPersonaCompleteRequiresPrefix) {
		t.Fatalf("SetPersona = %v, want ErrPersonaCompleteRequiresPrefix", err)
	}
	if !c.Agent.Persona.IsZero() {
		t.Fatalf("a rejected persona reached the config: %+v", c.Agent.Persona)
	}
}

func TestPersonaRuntimeContextDefaultsEnabled(t *testing.T) {
	if !(PersonaConfig{}).RuntimeContextEnabled() {
		t.Fatal("absent include_runtime_context reads as disabled")
	}
	if !(PersonaConfig{IncludeRuntimeContext: new(true)}).RuntimeContextEnabled() {
		t.Fatal("explicit true reads as disabled")
	}
	if (PersonaConfig{IncludeRuntimeContext: new(false)}).RuntimeContextEnabled() {
		t.Fatal("explicit false reads as enabled")
	}
}

// Render → load must return the same persona, including the pointer-only case
// where the runtime-context switch is the user's whole configuration.
func TestPersonaRendersAndLoadsUnchanged(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := UserConfigPath()
	for _, want := range []PersonaConfig{
		{Prefix: "IDENTITY", Suffix: "TAIL", Complete: true, IncludeRuntimeContext: new(false)},
		{Prefix: "IDENTITY", IncludeRuntimeContext: new(true)},
		{Suffix: "TAIL"},
		{IncludeRuntimeContext: new(false)},
	} {
		c := Default()
		if err := c.SetPersona(want); err != nil {
			t.Fatalf("SetPersona(%+v): %v", want, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		rendered := RenderTOMLForScope(c, RenderScopeUser)
		if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
			t.Fatal(err)
		}
		loaded := LoadForEdit(path).Agent.Persona
		if !reflect.DeepEqual(loaded, want.Normalize()) {
			t.Fatalf("round-trip %+v:\nloaded %+v\nrendered:\n%s", want, loaded, rendered)
		}
	}
}

func TestPersonaZeroRendersNothing(t *testing.T) {
	rendered := RenderTOMLForScope(Default(), RenderScopeUser)
	if strings.Contains(rendered, "[agent.persona]") {
		t.Fatalf("an absent persona rendered a section:\n%s", rendered)
	}
}

func TestPersonaNeverEntersProjectConfig(t *testing.T) {
	c := Default()
	if err := c.SetPersona(PersonaConfig{Prefix: "IDENTITY", Suffix: "TAIL"}); err != nil {
		t.Fatal(err)
	}
	if rendered := RenderTOMLForScope(c, RenderScopeProject); strings.Contains(rendered, "[agent.persona]") {
		t.Fatalf("project render carried a user-level persona:\n%s", rendered)
	}
	if delta := RenderTOMLProjectDelta(c); strings.Contains(delta, "[agent.persona]") {
		t.Fatalf("project delta carried a user-level persona:\n%s", delta)
	}
}

func TestOldUserConfigWithoutPersonaStillLoads(t *testing.T) {
	path, _ := seedUserConfigFrom1x(t)
	loaded := LoadForEdit(path)
	if !loaded.Agent.Persona.IsZero() {
		t.Fatalf("old config grew a persona: %+v", loaded.Agent.Persona)
	}
	if rendered := RenderTOMLForScope(loaded, RenderScopeUser); strings.Contains(rendered, "[agent.persona]") {
		t.Fatalf("saving an old config added a persona section:\n%s", rendered)
	}
}
