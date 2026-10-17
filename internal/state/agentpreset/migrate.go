package agentpreset

import (
	"errors"
	"fmt"
	"path/filepath"

	"reasonix/internal/contract/agentcomposition"
)

func (s *Store) MigratePersona(persona agentcomposition.Persona) error {
	path := filepath.Join(s.directory("user"), "settings.json")
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	p, err := s.preferences()
	if err != nil || p.PersonaMigrated {
		return err
	}
	if persona.Prefix != "" || persona.Suffix != "" || persona.Complete || !persona.IncludeRuntimeContext {
		id, err := s.retainMigratedPersona(persona)
		if err != nil {
			return err
		}
		if !p.DefaultChosen {
			p.Default = id
		}
	}
	p.PersonaMigrated = true
	return writeJSON(path, p)
}

func (s *Store) retainMigratedPersona(persona agentcomposition.Persona) (string, error) {
	d := agentcomposition.Builtins()[0]
	d.Name, d.Persona = "个人 Agent", persona
	for attempt := range 100 {
		d.ID = "user:migrated-persona"
		if attempt > 0 {
			d.ID = fmt.Sprintf("user:migrated-persona-%s-%d", agentcomposition.Digest(persona)[:12], attempt)
		}
		existing, err := s.Get(d.ID)
		if errors.Is(err, ErrNotFound) {
			if _, err := s.Save(d, ""); err != nil {
				return "", err
			}
			return d.ID, nil
		}
		if err != nil {
			return "", err
		}
		if existing.Revision == agentcomposition.Revision(d) {
			return d.ID, nil
		}
	}
	return "", ErrConflict
}
