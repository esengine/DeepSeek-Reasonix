package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/mcplaunch"
)

// MCP activation is the durable enable/disable switch for installed servers.
// Install remains the authorization action; this file only records whether an
// already authorized server is currently enabled for the catalog.

const (
	mcpActivationVersion  = 1
	mcpActivationFilename = "mcp-activation.json"
	mcpActivationLockFile = ".mcp-activation.lock"
)

// MCPActivationScope identifies where an enable override applies.
type MCPActivationScope string

const (
	MCPActivationGlobal    MCPActivationScope = "global"
	MCPActivationWorkspace MCPActivationScope = "workspace"
)

// MCPActivationOverride is one durable enable/disable decision.
type MCPActivationOverride struct {
	Scope     MCPActivationScope `json:"scope"`
	Workspace string             `json:"workspace,omitempty"`
	Source    string             `json:"source,omitempty"`
	Owner     string             `json:"owner,omitempty"`
	Server    string             `json:"server"`
	Enabled   bool               `json:"enabled"`
}

// MCPActivationFile is the on-disk shape of $REASONIX_HOME/mcp-activation.json.
type MCPActivationFile struct {
	Version   int                     `json:"version"`
	Overrides []MCPActivationOverride `json:"overrides"`
}

// MCPActivationStore loads and persists MCP enable overrides.
type MCPActivationStore struct {
	path string
	mu   sync.Mutex
}

// MCPActivationPath returns the durable activation file under Reasonix home.
func MCPActivationPath(reasonixHome string) string {
	return filepath.Join(strings.TrimSpace(reasonixHome), mcpActivationFilename)
}

// NewMCPActivationStore opens the activation store for reasonixHome.
func NewMCPActivationStore(reasonixHome string) *MCPActivationStore {
	return &MCPActivationStore{path: MCPActivationPath(reasonixHome)}
}

// DefaultMCPActivationStore uses the process Reasonix home.
func DefaultMCPActivationStore() *MCPActivationStore {
	return NewMCPActivationStore(ReasonixHomeDir())
}

// Path returns the store file path.
func (s *MCPActivationStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the activation file. Missing files yield an empty store.
func (s *MCPActivationStore) Load() (MCPActivationFile, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return MCPActivationFile{Version: mcpActivationVersion}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *MCPActivationStore) loadLocked() (MCPActivationFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return MCPActivationFile{Version: mcpActivationVersion}, nil
		}
		return MCPActivationFile{}, err
	}
	var file MCPActivationFile
	if err := json.Unmarshal(data, &file); err != nil {
		return MCPActivationFile{}, err
	}
	if file.Version == 0 {
		file.Version = mcpActivationVersion
	}
	file.Overrides = compactActivationOverrides(file.Overrides)
	return file, nil
}

// SetEnabled records a durable enable/disable override for one server.
func (s *MCPActivationStore) SetEnabled(override MCPActivationOverride) error {
	if s == nil {
		return nil
	}
	override = normalizeActivationOverride(override)
	if override.Server == "" {
		return nil
	}
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
	file.Version = mcpActivationVersion
	file.Overrides = upsertActivationOverride(file.Overrides, override)
	return s.saveLocked(file)
}

// Clear removes the override for one server identity, restoring default enable.
func (s *MCPActivationStore) Clear(override MCPActivationOverride) error {
	if s == nil {
		return nil
	}
	override = normalizeActivationOverride(override)
	if override.Server == "" {
		return nil
	}
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
	kept := file.Overrides[:0]
	for _, existing := range file.Overrides {
		if activationKey(existing) == activationKey(override) {
			continue
		}
		kept = append(kept, existing)
	}
	file.Overrides = kept
	file.Version = mcpActivationVersion
	return s.saveLocked(file)
}

// Lookup reports whether an override exists and its enabled value.
func (s *MCPActivationStore) Lookup(scope MCPActivationScope, workspace, source, owner, server string) (enabled bool, found bool, err error) {
	file, err := s.Load()
	if err != nil {
		return false, false, err
	}
	want := activationKey(normalizeActivationOverride(MCPActivationOverride{
		Scope:     scope,
		Workspace: workspace,
		Source:    source,
		Owner:     owner,
		Server:    server,
	}))
	for _, existing := range file.Overrides {
		if activationKey(existing) == want {
			return existing.Enabled, true, nil
		}
	}
	return false, false, nil
}

// IsEnabled resolves the product enable state for one plugin entry.
// An explicit activation override wins; otherwise auto_start=false maps to
// disabled and true/nil map to enabled. Safe Mode callers should skip plugins
// entirely rather than consulting this helper.
func (s *MCPActivationStore) IsEnabled(entry PluginEntry, workspace string) (bool, error) {
	scope, workspaceFP, source, owner := ActivationIdentity(entry, workspace)
	if s != nil {
		if enabled, found, err := s.Lookup(scope, workspaceFP, source, owner, entry.Name); err != nil {
			return false, err
		} else if found {
			return enabled, nil
		}
	}
	return entry.ShouldAutoStart(), nil
}

// SetServerEnabled records a durable enable/disable override for entry.
func (s *MCPActivationStore) SetServerEnabled(entry PluginEntry, workspace string, enabled bool) error {
	scope, workspaceFP, source, owner := ActivationIdentity(entry, workspace)
	return s.SetEnabled(MCPActivationOverride{
		Scope:     scope,
		Workspace: workspaceFP,
		Source:    source,
		Owner:     owner,
		Server:    entry.Name,
		Enabled:   enabled,
	})
}

// ClearServer removes the activation override for entry, restoring defaults.
func (s *MCPActivationStore) ClearServer(entry PluginEntry, workspace string) error {
	scope, workspaceFP, source, owner := ActivationIdentity(entry, workspace)
	return s.Clear(MCPActivationOverride{
		Scope:     scope,
		Workspace: workspaceFP,
		Source:    source,
		Owner:     owner,
		Server:    entry.Name,
	})
}

// ActivationIdentity returns the durable key components for one plugin entry.
func ActivationIdentity(entry PluginEntry, workspace string) (scope MCPActivationScope, workspaceFP, source, owner string) {
	return activationIdentity(entry, workspace)
}

func (s *MCPActivationStore) saveLocked(file MCPActivationFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.AtomicWriteFile(s.path, data, 0o600)
}

// lockUpdates serializes the full read-modify-write transaction across both
// independent store instances and separate Reasonix processes. Atomic rename
// prevents torn JSON; this lock additionally prevents the last writer from
// silently dropping another server's override.
func (s *MCPActivationStore) lockUpdates() (func(), error) {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := filelock.Acquire(ctx, filepath.Join(dir, mcpActivationLockFile))
	if err != nil {
		return nil, err
	}
	return unlock, nil
}

func activationIdentity(entry PluginEntry, workspace string) (MCPActivationScope, string, string, string) {
	source := strings.TrimSpace(string(entry.Source))
	owner := ""
	if entry.Source == MCPSourcePluginPackage {
		// Plugin-package servers key by owner+server to avoid collisions when
		// two packages expose the same short server name. Owner is filled by
		// the caller when known; Source alone still disambiguates packages.
		owner = strings.TrimSpace(source)
		return MCPActivationGlobal, "", source, owner
	}
	if entry.Source.ProjectScoped() || source == "workspace_config" || source == "project" || source == ".mcp.json" {
		return MCPActivationWorkspace, mcplaunch.WorkspaceFingerprint(workspace), source, owner
	}
	return MCPActivationGlobal, "", source, owner
}

func normalizeActivationOverride(o MCPActivationOverride) MCPActivationOverride {
	o.Server = strings.TrimSpace(o.Server)
	o.Source = strings.TrimSpace(o.Source)
	o.Owner = strings.TrimSpace(o.Owner)
	o.Workspace = strings.TrimSpace(o.Workspace)
	switch o.Scope {
	case MCPActivationWorkspace:
		// keep
	default:
		o.Scope = MCPActivationGlobal
		o.Workspace = ""
	}
	return o
}

func activationKey(o MCPActivationOverride) string {
	o = normalizeActivationOverride(o)
	return strings.Join([]string{string(o.Scope), o.Workspace, o.Source, o.Owner, o.Server}, "\x00")
}

func upsertActivationOverride(overrides []MCPActivationOverride, next MCPActivationOverride) []MCPActivationOverride {
	next = normalizeActivationOverride(next)
	key := activationKey(next)
	for i, existing := range overrides {
		if activationKey(existing) == key {
			overrides[i] = next
			return overrides
		}
	}
	return append(overrides, next)
}

func compactActivationOverrides(overrides []MCPActivationOverride) []MCPActivationOverride {
	if len(overrides) == 0 {
		return nil
	}
	out := make([]MCPActivationOverride, 0, len(overrides))
	seen := map[string]int{}
	for _, o := range overrides {
		o = normalizeActivationOverride(o)
		if o.Server == "" {
			continue
		}
		key := activationKey(o)
		if idx, ok := seen[key]; ok {
			out[idx] = o
			continue
		}
		seen[key] = len(out)
		out = append(out, o)
	}
	return out
}
