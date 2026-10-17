package control

import (
	"fmt"

	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/persona"
)

// PersonaSettings is the user's system persona as Studio edits it: the four
// stored fields, the user config path it lives in, and the built-in starting
// points. Presets ride the same response so the page cannot drift from the
// kernel's list.
type PersonaSettings struct {
	Prefix                string           `json:"prefix"`
	Suffix                string           `json:"suffix"`
	Complete              bool             `json:"complete"`
	IncludeRuntimeContext bool             `json:"include_runtime_context"`
	Path                  string           `json:"path"`
	Presets               []persona.Preset `json:"presets"`
}

// PersonaSettings reports the persona stored in the user config. The persona is
// composed once into the cache-stable prompt prefix at boot, so this value is
// what the next session will get, not necessarily what the running one has.
func (c *Controller) PersonaSettings() PersonaSettings {
	path := config.UserConfigPath()
	p := config.LoadForEdit(path).Agent.Persona.Normalize()
	return PersonaSettings{
		Prefix:                p.Prefix,
		Suffix:                p.Suffix,
		Complete:              p.Complete,
		IncludeRuntimeContext: p.RuntimeContextEnabled(),
		Path:                  path,
		Presets:               persona.Presets(),
	}
}

// SavePersonaSettings persists the persona to the user config only and rebuilds
// nothing: the prompt bytes belong to the provider-cached prefix, so the running
// session keeps what it was built with and the saved value applies to new ones.
func (c *Controller) SavePersonaSettings(p config.PersonaConfig) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetPersona(p); err != nil {
		return fmt.Errorf("persona: %w", err)
	}
	return cfg.SaveTo(path)
}
