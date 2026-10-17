package agentpreset

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/contract/agentcomposition"
)

type Catalog struct {
	Presets     []agentcomposition.Version           `json:"presets"`
	Default     string                               `json:"default"`
	Diagnostics []agentcomposition.Diagnostic        `json:"diagnostics"`
	Modules     map[string][]agentcomposition.Module `json:"modules,omitempty"`
}

type preferences struct {
	Default         string            `json:"default,omitempty"`
	Workspaces      map[string]string `json:"workspaces,omitempty"`
	PersonaMigrated bool              `json:"personaMigrated,omitempty"`
	DefaultChosen   bool              `json:"defaultChosen,omitempty"`
}

func (s *Store) List() (Catalog, error) {
	out := Catalog{Presets: []agentcomposition.Version{}, Diagnostics: []agentcomposition.Diagnostic{}}
	var err error
	if out.Default, err = s.Default(); err != nil {
		return out, err
	}
	for _, d := range agentcomposition.Builtins() {
		out.Presets = append(out.Presets, agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)})
	}
	for _, scope := range []string{"user", "project"} {
		if scope == "project" && s.Workspace == "" {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(s.directory(scope), "definitions"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return out, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			id := scope + ":" + strings.TrimSuffix(entry.Name(), ".json")
			v, err := s.Get(id)
			if err != nil {
				out.Diagnostics = append(out.Diagnostics, agentcomposition.Diagnostic{Code: "agent_composition.invalid", Module: id, Detail: err.Error()})
				continue
			}
			out.Presets = append(out.Presets, v)
		}
	}
	sort.SliceStable(out.Presets, func(i, j int) bool { return out.Presets[i].Definition.ID < out.Presets[j].Definition.ID })
	if _, err := s.Get(out.Default); err != nil {
		out.Diagnostics = append(out.Diagnostics, agentcomposition.Diagnostic{Code: "agent_composition.unavailable", Module: out.Default, Detail: "The selected default is unavailable. Select an available default explicitly."})
	}
	return out, nil
}

func (s *Store) preferences() (preferences, error) {
	p := preferences{Default: "builtin:general", Workspaces: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(s.directory("user"), "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	if p.Workspaces == nil {
		p.Workspaces = map[string]string{}
	}
	return p, nil
}

func (s *Store) Default() (string, error) {
	p, err := s.preferences()
	if err != nil {
		return "", err
	}
	if id := p.Workspaces[s.Workspace]; id != "" {
		return id, nil
	}
	return p.Default, nil
}

func (s *Store) SetDefault(id string, workspace bool) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	if !workspace && strings.HasPrefix(id, "project:") || workspace && s.Workspace == "" {
		return agentcomposition.ErrInvalid
	}
	path := filepath.Join(s.directory("user"), "settings.json")
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	p, err := s.preferences()
	if err != nil {
		return err
	}
	if workspace {
		p.Workspaces[s.Workspace] = id
	} else {
		p.Default = id
		p.DefaultChosen = true
	}
	return writeJSON(path, p)
}
