package evolve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/fileutil"
)

// Store persists proposals under the Reasonix state home for one project.
//
// Layout: <UserDir>/evolve/<workspace-slug>/proposals/<id>.json
type Store struct {
	Dir string // .../evolve/<slug>
}

// StoreFor resolves the evolve store for a project. Empty userDir yields a
// zero Store that rejects writes.
func StoreFor(userDir, projectRoot string) Store {
	if strings.TrimSpace(userDir) == "" {
		return Store{}
	}
	root := projectRoot
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return Store{Dir: filepath.Join(userDir, "evolve", config.WorkspaceSlug(root))}
}

func (s Store) proposalsDir() string {
	if s.Dir == "" {
		return ""
	}
	return filepath.Join(s.Dir, "proposals")
}

// Save writes or overwrites a proposal file.
func (s Store) Save(p Proposal) error {
	if s.Dir == "" {
		return fmt.Errorf("evolve store unavailable (no user config dir)")
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		return fmt.Errorf("proposal id is required")
	}
	if err := validateID(p.ID); err != nil {
		return err
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	p.Status = NormalizeStatus(string(p.Status))
	p.Tier = NormalizeTier(string(p.Tier))
	dir := s.proposalsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, p.ID+".json")
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return fileutil.AtomicWriteFile(path, raw, 0o644)
}

// Get loads a proposal by id.
func (s Store) Get(id string) (Proposal, bool) {
	id = strings.TrimSpace(id)
	if s.Dir == "" || id == "" {
		return Proposal{}, false
	}
	path := filepath.Join(s.proposalsDir(), id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Proposal{}, false
	}
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return Proposal{}, false
	}
	p.Status = NormalizeStatus(string(p.Status))
	p.Tier = NormalizeTier(string(p.Tier))
	return p, true
}

// List returns proposals sorted by id for deterministic tests and UIs.
func (s Store) List() []Proposal {
	dir := s.proposalsDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Proposal
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		if p, ok := s.Get(id); ok {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validateID(id string) error {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid proposal id %q", id)
	}
	for _, r := range id {
		if unicodeSafeID(r) {
			continue
		}
		return fmt.Errorf("invalid proposal id %q", id)
	}
	return nil
}

func unicodeSafeID(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '-', '_', '.':
		return true
	}
	return false
}
