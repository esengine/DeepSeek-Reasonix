// Package mcplaunch stores exact project MCP launch authorizations and mutable
// launcher locks. It does not classify tools or contribute to provider-visible
// schemas; ordinary tool policy lives in the plugin and permission layers.
package mcplaunch

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/fileutil"
)

const (
	StoreVersion   = 1
	StateFilename  = "mcp-security.json"
	workspaceScope = "workspace"
)

// ProjectLaunchIdentity is the secret-free canonical input to an exact project
// server launch identity digest.
// Environment and header values are intentionally excluded so credential
// rotation does not invalidate an otherwise identical authorization.
type ProjectLaunchIdentity struct {
	Server         string   `json:"server"`
	Transport      string   `json:"transport"`
	CommandPath    string   `json:"command_path,omitempty"`
	CommandSHA256  string   `json:"command_sha256,omitempty"`
	Args           []string `json:"args,omitempty"`
	Dir            string   `json:"dir,omitempty"`
	URL            string   `json:"url,omitempty"`
	EnvKeys        []string `json:"env_keys,omitempty"`
	HeaderKeys     []string `json:"header_keys,omitempty"`
	LauncherDigest string   `json:"launcher_digest,omitempty"`
}

type LauncherLock struct {
	Server          string    `json:"server"`
	Workspace       string    `json:"workspace_fingerprint,omitempty"`
	Locator         string    `json:"locator"`
	ResolvedVersion string    `json:"resolved_version"`
	ContentSHA256   string    `json:"content_sha256"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// LaunchGrant is durable consent to start one exact project-provided MCP.
// Scope remains in the JSON solely so previous Reasonix versions can read new
// writes; new code only creates workspace-scoped grants.
type LaunchGrant struct {
	Scope                string    `json:"scope"`
	WorkspaceFingerprint string    `json:"workspace_fingerprint,omitempty"`
	Server               string    `json:"server"`
	ConfigSource         string    `json:"config_source"`
	IdentityDigest       string    `json:"identity_fingerprint"`
	CreatedAt            time.Time `json:"created_at"`
}

// State stores server-level launch grants and exact mutable-launcher locks.
// Legacy per-tool reader receipts are deliberately not retained or consulted.
type State struct {
	Version       int             `json:"version"`
	LaunchGrants  []LaunchGrant   `json:"launch_grants,omitempty"`
	LauncherLocks []LauncherLock  `json:"launcher_locks,omitempty"`
	LegacyImports json.RawMessage `json:"legacy_imports,omitempty"`
}

type Manager struct {
	path                 string
	workspaceFingerprint string
	mu                   sync.Mutex
}

var managerRegistry struct {
	sync.Mutex
	items map[string]*Manager
}

func StatePath(reasonixHome string) string {
	if strings.TrimSpace(reasonixHome) == "" {
		return ""
	}
	return filepath.Join(reasonixHome, StateFilename)
}

func NewManager(path, workspace string) *Manager {
	return &Manager{path: path, workspaceFingerprint: WorkspaceFingerprint(workspace)}
}

// ForWorkspace returns the process-shared manager for one Reasonix home and
// workspace so sibling tabs observe one launch authorization state.
func ForWorkspace(reasonixHome, workspace string) *Manager {
	path := StatePath(reasonixHome)
	workspaceFP := WorkspaceFingerprint(workspace)
	key := path + "\x00" + workspaceFP
	managerRegistry.Lock()
	defer managerRegistry.Unlock()
	if managerRegistry.items == nil {
		managerRegistry.items = map[string]*Manager{}
	}
	if m := managerRegistry.items[key]; m != nil {
		return m
	}
	m := &Manager{path: path, workspaceFingerprint: workspaceFP}
	managerRegistry.items[key] = m
	return m
}

func WorkspaceFingerprint(workspace string) string {
	workspace = canonicalPath(workspace)
	if workspace == "" {
		return ""
	}
	return digestBytes([]byte(workspace))
}

func ProjectLaunchIdentityDigest(identity ProjectLaunchIdentity) (string, error) {
	identity = normalizeIdentity(identity, runtime.GOOS == "windows")
	// PackageDigest was never populated, but its empty field was part of the
	// original canonical JSON. Keep that placeholder so existing project launch
	// grants remain byte-for-byte valid after the internal cleanup.
	payload := struct {
		Server, Transport, CommandPath, CommandSHA256, Dir, URL string
		Args, EnvKeys, HeaderKeys                               []string
		PackageDigest, LauncherDigest                           string
	}{
		identity.Server, identity.Transport, identity.CommandPath, identity.CommandSHA256,
		identity.Dir, identity.URL, identity.Args, identity.EnvKeys, identity.HeaderKeys,
		"", identity.LauncherDigest,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

func normalizeIdentity(identity ProjectLaunchIdentity, envCaseInsensitive bool) ProjectLaunchIdentity {
	identity.Server = strings.TrimSpace(identity.Server)
	identity.Transport = normalizeTransport(identity.Transport)
	identity.CommandPath = canonicalPath(identity.CommandPath)
	identity.Dir = canonicalPath(identity.Dir)
	identity.URL = strings.TrimSpace(identity.URL)
	identity.Args = append([]string(nil), identity.Args...)
	identity.EnvKeys = cleanStrings(identity.EnvKeys, envCaseInsensitive)
	identity.HeaderKeys = cleanStrings(identity.HeaderKeys, true)
	return identity
}

func FileSHA256(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

func (m *Manager) Path() string { return m.path }

func (m *Manager) WorkspaceFingerprint() string { return m.workspaceFingerprint }

func (m *Manager) Load() (State, error) {
	if strings.TrimSpace(m.path) == "" {
		return State{Version: StoreVersion}, nil
	}
	body, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{Version: StoreVersion}, nil
		}
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return State{}, fmt.Errorf("parse MCP launch authorization state: %w", err)
	}
	if state.Version == 0 {
		state.Version = StoreVersion
	}
	if state.Version != StoreVersion {
		return State{}, fmt.Errorf("unsupported MCP launch authorization state version %d", state.Version)
	}
	normalizeState(&state)
	return state, nil
}

// Authorize records durable workspace consent for one exact server identity.
func (m *Manager) Authorize(server, configSource, identityDigest string) error {
	grant := LaunchGrant{
		Scope: workspaceScope, WorkspaceFingerprint: m.workspaceFingerprint,
		Server: strings.TrimSpace(server), ConfigSource: strings.TrimSpace(configSource),
		IdentityDigest: strings.TrimSpace(identityDigest), CreatedAt: time.Now().UTC(),
	}
	if grant.Server == "" || grant.ConfigSource == "" || grant.IdentityDigest == "" {
		return fmt.Errorf("MCP launch authorization requires server, config source, and identity")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		state.LaunchGrants = upsertLaunchGrant(state.LaunchGrants, grant)
	})
}

// LaunchAuthorized checks exact server-level consent without starting the server.
func (m *Manager) LaunchAuthorized(server, configSource, identityDigest string) (authorized, changed bool, err error) {
	server = strings.TrimSpace(server)
	configSource = strings.TrimSpace(configSource)
	identityDigest = strings.TrimSpace(identityDigest)
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return false, false, err
	}
	for _, grant := range state.LaunchGrants {
		if grant.Server != server || grant.ConfigSource != configSource || grant.WorkspaceFingerprint != m.workspaceFingerprint {
			continue
		}
		if grant.IdentityDigest == identityDigest {
			return true, false, nil
		}
		changed = true
	}
	return false, changed, nil
}

func (m *Manager) Revoke(server string) error {
	server = strings.TrimSpace(server)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		out := state.LaunchGrants[:0]
		for _, grant := range state.LaunchGrants {
			if grant.Server == server && grant.WorkspaceFingerprint == m.workspaceFingerprint {
				continue
			}
			out = append(out, grant)
		}
		state.LaunchGrants = out
	})
}

func (m *Manager) GetLauncherLock(server, locator string) (LauncherLock, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return LauncherLock{}, false, err
	}
	for _, lock := range state.LauncherLocks {
		if lock.Server == strings.TrimSpace(server) && lock.Locator == strings.TrimSpace(locator) && lock.Workspace == m.workspaceFingerprint {
			return lock, true, nil
		}
	}
	return LauncherLock{}, false, nil
}

func (m *Manager) PutLauncherLock(lock LauncherLock) error {
	lock.Server = strings.TrimSpace(lock.Server)
	lock.Locator = strings.TrimSpace(lock.Locator)
	lock.ResolvedVersion = strings.TrimSpace(lock.ResolvedVersion)
	lock.ContentSHA256 = strings.TrimSpace(lock.ContentSHA256)
	if lock.Server == "" || lock.Locator == "" || lock.ResolvedVersion == "" || lock.ContentSHA256 == "" {
		return fmt.Errorf("incomplete MCP launcher lock")
	}
	lock.Workspace = m.workspaceFingerprint
	lock.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		for i := range state.LauncherLocks {
			if state.LauncherLocks[i].Server == lock.Server && state.LauncherLocks[i].Locator == lock.Locator && state.LauncherLocks[i].Workspace == lock.Workspace {
				state.LauncherLocks[i] = lock
				return
			}
		}
		state.LauncherLocks = append(state.LauncherLocks, lock)
	})
}

func LauncherLockFingerprint(lock LauncherLock) string {
	payload := struct {
		Server, Workspace, Locator, ResolvedVersion, ContentSHA256 string
	}{lock.Server, lock.Workspace, lock.Locator, lock.ResolvedVersion, lock.ContentSHA256}
	body, _ := json.Marshal(payload)
	return digestBytes(body)
}

func (m *Manager) updatePersistent(update func(*State)) error {
	if strings.TrimSpace(m.path) == "" {
		return fmt.Errorf("MCP launch authorization state path is unavailable")
	}
	unlock, err := acquireFileLock(m.path+".lock", 2*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := m.Load()
	if err != nil {
		return err
	}
	update(&state)
	state.Version = StoreVersion
	normalizeState(&state)
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return fileutil.AtomicWriteFile(m.path, body, 0o600)
}

func normalizeState(state *State) {
	if state.Version == 0 {
		state.Version = StoreVersion
	}
	state.LaunchGrants = dedupeLaunchGrants(state.LaunchGrants)
	sort.Slice(state.LauncherLocks, func(i, j int) bool {
		a, b := state.LauncherLocks[i], state.LauncherLocks[j]
		if a.Server != b.Server {
			return a.Server < b.Server
		}
		return a.Locator < b.Locator
	})
	sort.Slice(state.LaunchGrants, func(i, j int) bool {
		a, b := state.LaunchGrants[i], state.LaunchGrants[j]
		if a.Server != b.Server {
			return a.Server < b.Server
		}
		return a.ConfigSource < b.ConfigSource
	})
}

func upsertLaunchGrant(grants []LaunchGrant, grant LaunchGrant) []LaunchGrant {
	for i := range grants {
		if grants[i].WorkspaceFingerprint == grant.WorkspaceFingerprint && grants[i].Server == grant.Server && grants[i].ConfigSource == grant.ConfigSource {
			grant.CreatedAt = grants[i].CreatedAt
			grants[i] = grant
			return grants
		}
	}
	return append(grants, grant)
}

func dedupeLaunchGrants(grants []LaunchGrant) []LaunchGrant {
	out := make([]LaunchGrant, 0, len(grants))
	for _, grant := range grants {
		out = upsertLaunchGrant(out, grant)
	}
	return out
}

func normalizeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "stdio":
		return "stdio"
	case "http", "streamable-http", "streamable_http":
		return "http"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func canonicalPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}

func cleanStrings(values []string, fold bool) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if fold {
			value = strings.ToLower(value)
		}
		out = append(out, value)
	}
	sort.Strings(out)
	return compactStrings(out)
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func digestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func acquireFileLock(path string, wait time.Duration) (func(), error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate MCP authorization lock owner: %w", err)
	}
	owner := []byte(fmt.Sprintf("%d %s\n", os.Getpid(), hex.EncodeToString(token)))
	deadline := time.Now().Add(wait)
	for {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, writeErr := f.Write(owner)
			closeErr := f.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(path)
				if writeErr != nil {
					return nil, writeErr
				}
				return nil, closeErr
			}
			return func() { removeOwnedFileLock(path, owner) }, nil
		}
		if !errors.Is(err, os.ErrExist) && !launchLockContention(err) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			current, readErr := os.ReadFile(path)
			if readErr == nil && !fileLockOwnerAlive(current) {
				removeOwnedFileLock(path, current)
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for MCP authorization state lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func fileLockOwnerAlive(owner []byte) bool {
	fields := strings.Fields(string(owner))
	if len(fields) == 0 {
		return false
	}
	pid, err := strconv.Atoi(fields[0])
	return err == nil && launchLockProcessAlive(pid)
}

func removeOwnedFileLock(path string, owner []byte) {
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(owner) {
		return
	}
	_ = os.Remove(path)
}
