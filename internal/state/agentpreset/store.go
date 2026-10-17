// Package agentpreset owns preset declarations and their retained revisions.
package agentpreset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/trustedstate"
)

var ErrConflict = errors.New("agent preset revision conflict")
var ErrNotFound = errors.New("agent preset not found")
var ErrReadOnly = errors.New("built-in agent preset is read-only")

type Store struct{ Home, Workspace, StateRoot string }

func New(home, workspace string) *Store {
	return &Store{Home: home, Workspace: workspace, StateRoot: config.RootsForHome(home).MemoryUserDir()}
}

func (s *Store) directory(scope string) string {
	if scope == "project" {
		return filepath.Join(s.Workspace, ".reasonix", "agent-presets")
	}
	return filepath.Join(s.StateRoot, "agent-presets")
}

func (s *Store) UserDirectory() string { return s.directory("user") }

func (s *Store) trustedDirectory() string {
	return filepath.Join(s.StateRoot, trustedstate.DirectoryName, "agent-presets")
}

func (s *Store) declarationPath(id string) (string, error) {
	d := agentcomposition.Builtins()[0]
	d.ID = id
	if err := d.Validate(); err != nil {
		return "", err
	}
	scope, name, _ := strings.Cut(id, ":")
	if scope == "builtin" {
		return "", ErrReadOnly
	}
	if scope == "project" && s.Workspace == "" {
		return "", agentcomposition.ErrInvalid
	}
	path := filepath.Join(s.directory(scope), "definitions", name+".json")
	if scope == "project" {
		if err := s.checkProjectDeclaration(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func (s *Store) Get(id string) (agentcomposition.Version, error) {
	for _, d := range agentcomposition.Builtins() {
		if d.ID == id {
			return agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, nil
		}
	}
	if strings.HasPrefix(id, "builtin:") {
		return agentcomposition.Version{}, ErrNotFound
	}
	path, err := s.declarationPath(id)
	if err != nil {
		return agentcomposition.Version{}, err
	}
	b, err := s.readDeclaration(id, path)
	if errors.Is(err, os.ErrNotExist) {
		return agentcomposition.Version{}, ErrNotFound
	}
	if err != nil {
		return agentcomposition.Version{}, err
	}
	d, err := agentcomposition.Decode(b)
	if err != nil {
		return agentcomposition.Version{}, err
	}
	if d.ID != id {
		return agentcomposition.Version{}, fmt.Errorf("%w: identity mismatch", agentcomposition.ErrInvalid)
	}
	return agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, nil
}

func (s *Store) Save(d agentcomposition.Definition, expected string) (agentcomposition.Version, error) {
	if err := d.Validate(); err != nil {
		return agentcomposition.Version{}, err
	}
	path, err := s.declarationPath(d.ID)
	if err != nil {
		return agentcomposition.Version{}, err
	}
	unlock, err := s.lockDeclaration(d.ID, path)
	if err != nil {
		return agentcomposition.Version{}, err
	}
	defer unlock()
	old, err := s.Get(d.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return agentcomposition.Version{}, err
	}
	if old.Revision != expected {
		return agentcomposition.Version{}, ErrConflict
	}
	v := agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}
	if err := writeJSON(filepath.Join(s.directory("user"), "revisions", v.Revision+".json"), v); err != nil {
		return v, err
	}
	return v, s.writeDeclaration(path, d)
}

func (s *Store) Version(revision string) (agentcomposition.Version, error) {
	if len(revision) != 64 || strings.Trim(revision, "0123456789abcdef") != "" {
		return agentcomposition.Version{}, agentcomposition.ErrInvalid
	}
	var v agentcomposition.Version
	b, err := os.ReadFile(filepath.Join(s.directory("user"), "revisions", revision+".json"))
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	if v.Revision != revision || agentcomposition.Revision(v.Definition) != revision {
		return v, agentcomposition.ErrInvalid
	}
	return v, v.Definition.Validate()
}

func (s *Store) Delete(id, expected string) error {
	path, err := s.declarationPath(id)
	if err != nil {
		return err
	}
	unlock, err := s.lockDeclaration(id, path)
	if err != nil {
		return err
	}
	defer unlock()
	v, err := s.Get(id)
	if err != nil {
		return err
	}
	if v.Revision != expected {
		return ErrConflict
	}
	return s.removeDeclaration(id, path)
}

func lock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return filelock.Acquire(ctx, path+".lock")
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, append(b, '\n'), 0o600)
}
