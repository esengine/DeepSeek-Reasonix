package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/mcplaunch"
	"reasonix/internal/workspaceid"
)

// Install authorizes a capability; this file only records what is currently on
// and where that decision applies — for MCP servers and skills alike.

const (
	activationVersion  = 2
	activationFilename = "capability-activation.json"
	activationLockFile = ".capability-activation.lock"

	// legacyMCPFilename held MCP-only overrides keyed by a path fingerprint. It
	// is read once and never written, so downgrading keeps its own state.
	legacyMCPFilename = "mcp-activation.json"
	// activationLegacyPrefix tags a key carried over from that file. The path it
	// digested is unrecoverable, so such a row is matched by re-deriving the
	// fingerprint from the live root and upgraded on the next write.
	activationLegacyPrefix = "legacy:"
)

// CapabilityKind separates the two things a row can switch.
type CapabilityKind string

const (
	CapabilityMCP   CapabilityKind = "mcp"
	CapabilitySkill CapabilityKind = "skill"
)

// ActivationScope identifies how far one override reaches.
type ActivationScope string

const (
	ActivationGlobal  ActivationScope = "global"
	ActivationProject ActivationScope = "project"
)

// ActivationOverride is one durable enable/disable decision. Key is the
// workspace identity for a project override and empty for a global one; Source
// and Owner disambiguate MCP servers declared by different packages and stay
// empty for skills, which the user switches by the name they invoke.
type ActivationOverride struct {
	Kind    CapabilityKind  `json:"kind"`
	Scope   ActivationScope `json:"scope"`
	Key     string          `json:"key,omitempty"`
	Source  string          `json:"source,omitempty"`
	Owner   string          `json:"owner,omitempty"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
}

// ActivationFile is the on-disk shape of $REASONIX_HOME/capability-activation.json.
type ActivationFile struct {
	Version   int                  `json:"version"`
	Overrides []ActivationOverride `json:"overrides"`
}

// ActivationStore loads and persists capability enable overrides.
type ActivationStore struct {
	path       string
	legacyPath string
	mu         sync.Mutex
}

// ActivationPath returns the durable activation file under Reasonix home.
func ActivationPath(reasonixHome string) string {
	return filepath.Join(strings.TrimSpace(reasonixHome), activationFilename)
}

// NewActivationStore opens the activation store for reasonixHome.
func NewActivationStore(reasonixHome string) *ActivationStore {
	home := strings.TrimSpace(reasonixHome)
	return &ActivationStore{
		path:       ActivationPath(home),
		legacyPath: filepath.Join(home, legacyMCPFilename),
	}
}

// DefaultActivationStore uses the process Reasonix home.
func DefaultActivationStore() *ActivationStore {
	return NewActivationStore(ReasonixHomeDir())
}

// Path returns the store file path.
func (s *ActivationStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the activation file, folding in the legacy MCP file when this
// home has not been written since the rename. Missing files yield an empty store.
func (s *ActivationStore) Load() (ActivationFile, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return ActivationFile{Version: activationVersion}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *ActivationStore) loadLocked() (ActivationFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return ActivationFile{}, fmt.Errorf("%w: %w", ErrActivationUnavailable, err)
		}
		return s.loadLegacyLocked()
	}
	var file ActivationFile
	if err := json.Unmarshal(data, &file); err != nil {
		// A file the user can open and fix, which is not the same answer as a
		// disk that would not read it.
		return ActivationFile{}, asUnparsedJSON(s.path, data, err)
	}
	file.Version = activationVersion
	file.Overrides = compactOverrides(file.Overrides)
	return file, nil
}

// legacyOverride is the v1 row shape: MCP only, keyed by a path fingerprint.
type legacyOverride struct {
	Scope     string `json:"scope"`
	Workspace string `json:"workspace"`
	Source    string `json:"source"`
	Owner     string `json:"owner"`
	Server    string `json:"server"`
	Enabled   bool   `json:"enabled"`
}

func (s *ActivationStore) loadLegacyLocked() (ActivationFile, error) {
	empty := ActivationFile{Version: activationVersion}
	data, err := os.ReadFile(s.legacyPath)
	if err != nil {
		return empty, nil
	}
	var legacy struct {
		Overrides []legacyOverride `json:"overrides"`
	}
	if json.Unmarshal(data, &legacy) != nil {
		return empty, nil
	}
	out := make([]ActivationOverride, 0, len(legacy.Overrides))
	for _, row := range legacy.Overrides {
		next := ActivationOverride{
			Kind:    CapabilityMCP,
			Scope:   ActivationScope(row.Scope),
			Source:  row.Source,
			Owner:   row.Owner,
			Name:    row.Server,
			Enabled: row.Enabled,
		}
		if next.Scope == ActivationProject && strings.TrimSpace(row.Workspace) != "" {
			next.Key = activationLegacyPrefix + strings.TrimSpace(row.Workspace)
		}
		out = append(out, next)
	}
	empty.Overrides = compactOverrides(out)
	return empty, nil
}

// SetOverride records one durable enable/disable decision.
func (s *ActivationStore) SetOverride(override ActivationOverride) error {
	if s == nil {
		return nil
	}
	override = normalizeOverride(override)
	if override.Name == "" {
		return nil
	}
	return s.update(func(file *ActivationFile) {
		file.Overrides = upsertOverride(file.Overrides, override)
	})
}

// ClearOverride removes the override for one identity, restoring the default.
func (s *ActivationStore) ClearOverride(override ActivationOverride) error {
	if s == nil {
		return nil
	}
	override = normalizeOverride(override)
	if override.Name == "" {
		return nil
	}
	return s.update(func(file *ActivationFile) {
		file.Overrides = dropOverride(file.Overrides, overrideKey(override))
	})
}

// Resolve reports the effective state for one identity in root, trying the
// project layer before the global one. declared is the value to use when no
// override applies — auto_start for a server, frontmatter for a skill.
// ActivationDecision is what the store holds. Undecided is not Disabled: one
// nobody ruled on and one the user switched off differ by whether to ask.
type ActivationDecision int

const (
	ActivationUndecided ActivationDecision = iota
	ActivationEnabled
	ActivationDisabled
)

// Decide reports the stored row for want, project layer first. The absence of a
// row gets no default here: that belongs to the caller, which knows what the
// capability inherits.
func (s *ActivationStore) Decide(want ActivationOverride, root string) (ActivationDecision, error) {
	return s.decide(want, root, false)
}

// decide answers Decide, and skips the global layer for an identity pinned to
// the project. ServerOverrideFor forces that pinning on writes; reading a global
// row anyway would let one row approve a repository-declared server everywhere,
// which is the thing the pinning exists to prevent.
func (s *ActivationStore) decide(want ActivationOverride, root string, projectOnly bool) (ActivationDecision, error) {
	if s == nil {
		return ActivationUndecided, nil
	}
	file, err := s.Load()
	if err != nil {
		return ActivationUndecided, err
	}
	want = normalizeOverride(want)
	for _, key := range projectKeys(root) {
		probe := want
		probe.Scope, probe.Key = ActivationProject, key
		if row, ok := findOverride(file.Overrides, overrideKey(probe)); ok {
			return decisionOf(row.Enabled), nil
		}
	}
	if projectOnly {
		return ActivationUndecided, nil
	}
	probe := want
	probe.Scope, probe.Key = ActivationGlobal, ""
	if row, ok := findOverride(file.Overrides, overrideKey(probe)); ok {
		return decisionOf(row.Enabled), nil
	}
	return ActivationUndecided, nil
}

func decisionOf(enabled bool) ActivationDecision {
	if enabled {
		return ActivationEnabled
	}
	return ActivationDisabled
}

func (s *ActivationStore) Resolve(want ActivationOverride, root string, declared bool) (bool, error) {
	if s == nil {
		return declared, nil
	}
	file, err := s.Load()
	if err != nil {
		return false, err
	}
	want = normalizeOverride(want)
	for _, key := range projectKeys(root) {
		probe := want
		probe.Scope = ActivationProject
		probe.Key = key
		if row, ok := findOverride(file.Overrides, overrideKey(probe)); ok {
			return row.Enabled, nil
		}
	}
	probe := want
	probe.Scope = ActivationGlobal
	probe.Key = ""
	if row, ok := findOverride(file.Overrides, overrideKey(probe)); ok {
		return row.Enabled, nil
	}
	return declared, nil
}

// ProjectOverrides returns the rows that apply to root alone — what the
// settings surface counts when it reports how many exceptions a project holds.
func (s *ActivationStore) ProjectOverrides(root string) ([]ActivationOverride, error) {
	if s == nil {
		return nil, nil
	}
	file, err := s.Load()
	if err != nil {
		return nil, err
	}
	keys := projectKeys(root)
	var out []ActivationOverride
	for _, row := range file.Overrides {
		if row.Scope != ActivationProject {
			continue
		}
		if slices.Contains(keys, row.Key) {
			out = append(out, row)
		}
	}
	return out, nil
}

// update runs one locked read-modify-write transaction, then upgrades any
// legacy-keyed rows the mutation left addressable under the current identity.
func (s *ActivationStore) update(mutate func(*ActivationFile)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlockFile, err := s.lockUpdates()
	if err != nil {
		return err
	}
	defer unlockFile()
	file, err := s.loadLocked()
	if err != nil {
		return err
	}
	mutate(&file)
	file.Version = activationVersion
	return s.saveLocked(file)
}

// ProjectKey is the identity a project-scoped override is filed under: the
// repository when root is in one, so every linked worktree shares it.
func ProjectKey(root string) string { return workspaceid.Key(root) }

// placeProjectRow files override under root, falling back to the global layer
// when there is no project to name. A project row with no key is unreachable —
// nothing resolves it — so a switch flipped with no workspace open would
// silently do nothing.
func placeProjectRow(override ActivationOverride, root string) ActivationOverride {
	if override.Scope != ActivationProject {
		return override
	}
	if key := ProjectKey(root); key != "" {
		override.Key = key
		return override
	}
	override.Scope = ActivationGlobal
	override.Key = ""
	return override
}

// projectKeys lists the identities a project override may be stored under, most
// current first. The legacy entry lets a pre-rename override keep working.
func projectKeys(root string) []string {
	var keys []string
	if key := workspaceid.Key(root); key != "" {
		keys = append(keys, key)
	}
	if fingerprint := mcplaunch.WorkspaceFingerprint(root); fingerprint != "" {
		keys = append(keys, activationLegacyPrefix+fingerprint)
	}
	return keys
}

func (s *ActivationStore) saveLocked(file ActivationFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("%w: %w", ErrActivationUnavailable, err)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrActivationUnavailable, err)
	}
	data = append(data, '\n')
	if err := fileutil.AtomicWriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("%w: %w", ErrActivationUnavailable, err)
	}
	return nil
}

// lockUpdates serializes the full read-modify-write transaction across both
// independent store instances and separate Reasonix processes. Atomic rename
// prevents torn JSON; this lock additionally prevents the last writer from
// silently dropping another server's override.
func (s *ActivationStore) lockUpdates() (func(), error) {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActivationUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, filepath.Join(dir, activationLockFile))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrActivationUnavailable, err)
	}
	return unlock, nil
}

func normalizeOverride(o ActivationOverride) ActivationOverride {
	o.Name = strings.TrimSpace(o.Name)
	o.Source = strings.TrimSpace(o.Source)
	o.Owner = strings.TrimSpace(o.Owner)
	o.Key = strings.TrimSpace(o.Key)
	if o.Kind != CapabilitySkill {
		o.Kind = CapabilityMCP
	}
	if o.Scope != ActivationProject {
		o.Scope = ActivationGlobal
		o.Key = ""
	}
	return o
}

func overrideKey(o ActivationOverride) string {
	o = normalizeOverride(o)
	return strings.Join([]string{string(o.Kind), string(o.Scope), o.Key, o.Source, o.Owner, o.Name}, "\x00")
}

func findOverride(overrides []ActivationOverride, key string) (ActivationOverride, bool) {
	for _, existing := range overrides {
		if overrideKey(existing) == key {
			return existing, true
		}
	}
	return ActivationOverride{}, false
}

func upsertOverride(overrides []ActivationOverride, next ActivationOverride) []ActivationOverride {
	key := overrideKey(next)
	for i, existing := range overrides {
		if overrideKey(existing) == key {
			overrides[i] = next
			return overrides
		}
	}
	return append(overrides, next)
}

func dropOverride(overrides []ActivationOverride, key string) []ActivationOverride {
	kept := overrides[:0]
	for _, existing := range overrides {
		if overrideKey(existing) == key {
			continue
		}
		kept = append(kept, existing)
	}
	return kept
}

func compactOverrides(overrides []ActivationOverride) []ActivationOverride {
	if len(overrides) == 0 {
		return nil
	}
	out := make([]ActivationOverride, 0, len(overrides))
	seen := map[string]int{}
	for _, o := range overrides {
		o = normalizeOverride(o)
		if o.Name == "" {
			continue
		}
		key := overrideKey(o)
		if idx, ok := seen[key]; ok {
			out[idx] = o
			continue
		}
		seen[key] = len(out)
		out = append(out, o)
	}
	return out
}
