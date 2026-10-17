package control

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/persona"
	"reasonix/internal/state/agentpreset"
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
	Error                 string           `json:"error,omitempty"`
}

// PersonaSettings reports the persona stored in the user config. The persona is
// composed once into the cache-stable prompt prefix at boot, so this value is
// what the next session will get, not necessarily what the running one has.
func (c *Controller) PersonaSettings() PersonaSettings {
	_, v, err := c.defaultPersonaVersion()
	p := v.Definition.Persona
	result := PersonaSettings{
		Prefix:                p.Prefix,
		Suffix:                p.Suffix,
		Complete:              p.Complete,
		IncludeRuntimeContext: p.IncludeRuntimeContext,
		Path:                  c.PresetStore().UserDirectory(),
		Presets:               persona.Presets(),
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

// SavePersonaSettings edits the personal default; running sessions keep their binding.
func (c *Controller) SavePersonaSettings(p config.PersonaConfig) error {
	cfg := &config.Config{}
	if err := cfg.SetPersona(p); err != nil {
		return fmt.Errorf("persona: %w", err)
	}
	s, v, err := c.defaultPersonaVersion()
	if err != nil {
		return err
	}
	d := v.Definition
	expected := v.Revision
	if strings.HasPrefix(d.ID, "builtin:") {
		d, err = unusedPersonaDefinition(s, d)
		if err != nil {
			return err
		}
		expected = ""
	}
	normalized := cfg.Agent.Persona
	d.Persona = agentcomposition.Persona{Prefix: normalized.Prefix, Suffix: normalized.Suffix, Complete: normalized.Complete, IncludeRuntimeContext: normalized.RuntimeContextEnabled()}
	if _, err := s.Save(d, expected); err != nil {
		return err
	}
	return s.SetDefault(d.ID, false)
}

func (c *Controller) defaultPersonaVersion() (*agentpreset.Store, agentcomposition.Version, error) {
	s, err := c.migrateLegacyPersona()
	if err != nil {
		return s, agentcomposition.Version{}, err
	}
	id, err := s.Default()
	if err != nil {
		return s, agentcomposition.Version{}, err
	}
	v, err := s.Get(id)
	return s, v, err
}

func (c *Controller) migrateLegacyPersona() (*agentpreset.Store, error) {
	s := agentpreset.New(c.PresetStore().Home, "")
	p := config.LoadForEdit(filepath.Join(s.Home, "config.toml")).Agent.Persona.Normalize()
	if err := s.MigratePersona(agentcomposition.Persona{Prefix: p.Prefix, Suffix: p.Suffix, Complete: p.Complete, IncludeRuntimeContext: p.RuntimeContextEnabled()}); err != nil {
		return s, err
	}
	return s, nil
}

func unusedPersonaDefinition(s *agentpreset.Store, d agentcomposition.Definition) (agentcomposition.Definition, error) {
	d.Name = "个人 Agent"
	for attempt := range 100 {
		d.ID = "user:persona"
		if attempt > 0 {
			d.ID = fmt.Sprintf("user:persona-%d", attempt)
		}
		if _, err := s.Get(d.ID); errors.Is(err, agentpreset.ErrNotFound) {
			return d, nil
		} else if err != nil {
			return d, err
		}
	}
	return d, agentpreset.ErrConflict
}
