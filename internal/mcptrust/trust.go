// Package mcptrust owns host-local trust receipts for MCP servers and tools.
// Trust state is deliberately kept out of provider-visible tool schemas and
// prompts: it only decides whether an externally asserted reader is accepted
// as a locally trusted reader.
package mcptrust

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
	StoreVersion  = 1
	StateFilename = "mcp-security.json"
)

type Scope string

const (
	ScopeSession   Scope = "session"
	ScopeWorkspace Scope = "workspace"
	ScopeGlobal    Scope = "global"
)

type Source string

const (
	SourceUser            Source = "user"
	SourceOfficialCatalog Source = "official_catalog"
	SourceLegacyImport    Source = "legacy_import"
)

type TrustState string

const (
	TrustUntrusted TrustState = "untrusted"
	TrustSession   TrustState = "session"
	TrustWorkspace TrustState = "workspace"
	TrustOfficial  TrustState = "official"
	TrustChanged   TrustState = "changed"
)

type IsolationState string

const (
	IsolationEnforced              IsolationState = "enforced"
	IsolationUnavailableUnconfined IsolationState = "unavailable_unconfined"
	IsolationNotApplicable         IsolationState = "not_applicable"
)

// Identity is the secret-free, canonical input to a server identity digest.
// Environment and header values are intentionally excluded so credential
// rotation does not invalidate an otherwise identical audited server.
type Identity struct {
	Server          string   `json:"server"`
	Transport       string   `json:"transport"`
	CommandPath     string   `json:"command_path,omitempty"`
	CommandSHA256   string   `json:"command_sha256,omitempty"`
	Args            []string `json:"args,omitempty"`
	Dir             string   `json:"dir,omitempty"`
	URL             string   `json:"url,omitempty"`
	EnvKeys         []string `json:"env_keys,omitempty"`
	HeaderKeys      []string `json:"header_keys,omitempty"`
	PackageDigest   string   `json:"package_digest,omitempty"`
	LauncherDigest  string   `json:"launcher_digest,omitempty"`
	ConfigSource    string   `json:"config_source,omitempty"`
	Network         bool     `json:"network"`
	WriteRoots      []string `json:"write_roots,omitempty"`
	ReadRoots       []string `json:"read_roots,omitempty"`
	ForbidReadRoots []string `json:"forbid_read_roots,omitempty"`
	IsolationPolicy string   `json:"isolation_policy,omitempty"`
}

// Capability is the host-observed safety snapshot for one raw MCP tool.
type Capability struct {
	RawName      string          `json:"raw_name"`
	ModelName    string          `json:"model_name"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	ReadOnly     bool            `json:"read_only"`
	Destructive  bool            `json:"destructive"`
}

type ToolReceipt struct {
	RawName       string `json:"raw_name"`
	ModelName     string `json:"model_name"`
	Fingerprint   string `json:"fingerprint"`
	ReadOnly      bool   `json:"read_only"`
	Destructive   bool   `json:"destructive"`
	TrustedReader bool   `json:"trusted_reader,omitempty"`
}

type Receipt struct {
	Scope                Scope         `json:"scope"`
	WorkspaceFingerprint string        `json:"workspace_fingerprint,omitempty"`
	Server               string        `json:"server"`
	ConfigSource         string        `json:"config_source,omitempty"`
	IdentityFingerprint  string        `json:"identity_fingerprint"`
	Tools                []ToolReceipt `json:"tools"`
	Source               Source        `json:"source"`
	CatalogEntryID       string        `json:"catalog_entry_id,omitempty"`
	CreatedAt            time.Time     `json:"created_at"`
	LastVerifiedAt       time.Time     `json:"last_verified_at"`
}

type LauncherLock struct {
	Server          string    `json:"server"`
	Workspace       string    `json:"workspace_fingerprint,omitempty"`
	Locator         string    `json:"locator"`
	ResolvedVersion string    `json:"resolved_version"`
	ContentSHA256   string    `json:"content_sha256"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// LaunchGrant is the pre-execution consent for repository-controlled MCP
// configuration. It is separate from capability receipts because a server must
// not be started merely to discover its tools before the user approves launch.
type LaunchGrant struct {
	Scope                Scope     `json:"scope"`
	WorkspaceFingerprint string    `json:"workspace_fingerprint,omitempty"`
	Server               string    `json:"server"`
	ConfigSource         string    `json:"config_source"`
	IdentityFingerprint  string    `json:"identity_fingerprint"`
	CreatedAt            time.Time `json:"created_at"`
}

type State struct {
	Version         int            `json:"version"`
	Receipts        []Receipt      `json:"receipts,omitempty"`
	LaunchGrants    []LaunchGrant  `json:"launch_grants,omitempty"`
	LauncherLocks   []LauncherLock `json:"launcher_locks,omitempty"`
	OfficialDenials []string       `json:"official_denials,omitempty"`
	LegacyImports   []string       `json:"legacy_imports,omitempty"`
}

// Evaluation is a point-in-time comparison between a receipt and a live tool
// snapshot. TrustedReaders is keyed by raw MCP tool name.
type Evaluation struct {
	State           TrustState
	Source          Source
	Scope           Scope
	IdentityChanged bool
	TrustedReaders  map[string]bool
	ChangedTools    []string
	ToolChanges     []ToolChange
}

type ToolChange struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type Manager struct {
	path                 string
	workspaceFingerprint string

	mu            sync.Mutex
	session       []Receipt
	sessionLaunch []LaunchGrant
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
// workspace. Controllers for sibling tabs therefore share session receipts.
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

func IdentityFingerprint(identity Identity) (string, error) {
	identity = normalizeIdentity(identity, runtime.GOOS == "windows")
	// Runtime confinement is deliberately not part of server identity. Tightening
	// Reasonix's own sandbox, changing workspace roots, or moving a config between
	// equivalent source labels must not invalidate trust in unchanged server code.
	payload := struct {
		Server, Transport, CommandPath, CommandSHA256, Dir, URL string
		Args, EnvKeys, HeaderKeys                               []string
		PackageDigest, LauncherDigest                           string
	}{
		identity.Server, identity.Transport, identity.CommandPath, identity.CommandSHA256,
		identity.Dir, identity.URL, identity.Args, identity.EnvKeys, identity.HeaderKeys,
		identity.PackageDigest, identity.LauncherDigest,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

// LegacyIdentityFingerprints returns the policy-coupled identity variants used
// by the first trust-receipt rollout. Callers use them only for exact digest
// migration; new receipts always use IdentityFingerprint above.
func LegacyIdentityFingerprints(identity Identity) []string {
	variants := make([]Identity, 0, 4)
	for _, source := range compactStrings([]string{identity.ConfigSource, "workspace_config"}) {
		withSource := identity
		withSource.ConfigSource = source
		variants = append(variants, withSource)
		withoutForbid := withSource
		withoutForbid.ForbidReadRoots = nil
		variants = append(variants, withoutForbid)
	}
	out := make([]string, 0, len(variants)*2)
	seen := map[string]bool{}
	for _, variant := range variants {
		for _, insensitive := range []bool{runtime.GOOS == "windows", false} {
			fp, err := legacyIdentityFingerprint(variant, insensitive)
			if err == nil && fp != "" && !seen[fp] {
				seen[fp] = true
				out = append(out, fp)
			}
		}
	}
	return out
}

func normalizeIdentity(identity Identity, envCaseInsensitive bool) Identity {
	identity.Server = strings.TrimSpace(identity.Server)
	identity.Transport = normalizeTransport(identity.Transport)
	identity.CommandPath = canonicalPath(identity.CommandPath)
	identity.Dir = canonicalPath(identity.Dir)
	identity.URL = strings.TrimSpace(identity.URL)
	identity.ConfigSource = strings.TrimSpace(identity.ConfigSource)
	// Command arguments are an ordered vector, not a set. Preserve both order
	// and duplicates (including intentional whitespace) so changing launcher
	// semantics always invalidates the saved identity.
	identity.Args = append([]string(nil), identity.Args...)
	// Environment names are case-sensitive on Unix and case-insensitive on
	// Windows. Header names are case-insensitive on every supported platform.
	identity.EnvKeys = cleanStrings(identity.EnvKeys, envCaseInsensitive)
	identity.HeaderKeys = cleanStrings(identity.HeaderKeys, true)
	identity.WriteRoots = canonicalPaths(identity.WriteRoots)
	identity.ReadRoots = canonicalPaths(identity.ReadRoots)
	identity.ForbidReadRoots = canonicalPaths(identity.ForbidReadRoots)
	return identity
}

func legacyIdentityFingerprint(identity Identity, envCaseInsensitive bool) (string, error) {
	identity = normalizeIdentity(identity, envCaseInsensitive)
	body, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

func CapabilityFingerprint(cap Capability) (string, error) {
	in, err := canonicalSecuritySchema(cap.InputSchema)
	if err != nil {
		return "", fmt.Errorf("input schema: %w", err)
	}
	out, err := canonicalSecuritySchema(cap.OutputSchema)
	if err != nil {
		return "", fmt.Errorf("output schema: %w", err)
	}
	payload := struct {
		RawName     string          `json:"raw_name"`
		ModelName   string          `json:"model_name"`
		Input       json.RawMessage `json:"input,omitempty"`
		Output      json.RawMessage `json:"output,omitempty"`
		ReadOnly    bool            `json:"read_only"`
		Destructive bool            `json:"destructive"`
	}{
		RawName: strings.TrimSpace(cap.RawName), ModelName: strings.TrimSpace(cap.ModelName),
		Input: in, Output: out, ReadOnly: cap.ReadOnly, Destructive: cap.Destructive,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
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
		return State{}, fmt.Errorf("parse MCP trust state: %w", err)
	}
	if state.Version == 0 {
		state.Version = StoreVersion
	}
	if state.Version != StoreVersion {
		return State{}, fmt.Errorf("unsupported MCP trust state version %d", state.Version)
	}
	normalizeState(&state)
	return state, nil
}

func (m *Manager) Trust(scope Scope, source Source, server, configSource, identityFingerprint, catalogEntryID string, capabilities []Capability) error {
	return m.trust(scope, source, server, configSource, identityFingerprint, catalogEntryID, capabilities, nil)
}

// TrustOfficial records all capability fingerprints for drift detection but
// grants reader authority only to names explicitly listed by the signed catalog.
func (m *Manager) TrustOfficial(server, configSource, identityFingerprint, catalogEntryID string, capabilities []Capability, readerNames []string) error {
	readers := map[string]bool{}
	for _, name := range readerNames {
		if name = strings.TrimSpace(name); name != "" {
			readers[name] = true
		}
	}
	return m.trust(ScopeGlobal, SourceOfficialCatalog, server, configSource, identityFingerprint, catalogEntryID, capabilities, readers)
}

// TrustLaunch records the user's consent to start one exact project-provided
// server identity. Session grants stay in memory; workspace grants are persisted.
func (m *Manager) TrustLaunch(scope Scope, server, configSource, identityFingerprint string) error {
	if scope != ScopeSession && scope != ScopeWorkspace {
		return fmt.Errorf("invalid MCP launch grant scope %q", scope)
	}
	grant := LaunchGrant{
		Scope: scope, Server: strings.TrimSpace(server), ConfigSource: strings.TrimSpace(configSource),
		IdentityFingerprint: strings.TrimSpace(identityFingerprint), CreatedAt: time.Now().UTC(),
	}
	if grant.Server == "" || grant.ConfigSource == "" || grant.IdentityFingerprint == "" {
		return fmt.Errorf("MCP launch grant requires server, config source, and identity")
	}
	if scope == ScopeWorkspace {
		grant.WorkspaceFingerprint = m.workspaceFingerprint
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if scope == ScopeSession {
		m.sessionLaunch = upsertLaunchGrant(m.sessionLaunch, grant)
		return nil
	}
	m.sessionLaunch = removeLaunchGrantSlot(m.sessionLaunch, grant.Server, grant.ConfigSource, m.workspaceFingerprint)
	return m.updatePersistent(func(state *State) {
		state.LaunchGrants = upsertLaunchGrant(state.LaunchGrants, grant)
	})
}

// LaunchAuthorized checks consent without starting the server. A matching
// capability receipt from an earlier version also counts as consent so the new
// launch gate does not re-prompt already trusted projects.
func (m *Manager) LaunchAuthorized(server, configSource, identityFingerprint string) (authorized, changed bool, err error) {
	server = strings.TrimSpace(server)
	configSource = strings.TrimSpace(configSource)
	identityFingerprint = strings.TrimSpace(identityFingerprint)
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return false, false, err
	}
	changed = false
	for _, grant := range append(append([]LaunchGrant(nil), m.sessionLaunch...), state.LaunchGrants...) {
		if grant.Server != server || grant.ConfigSource != configSource {
			continue
		}
		if grant.Scope != ScopeSession && grant.WorkspaceFingerprint != m.workspaceFingerprint {
			continue
		}
		if grant.IdentityFingerprint == identityFingerprint {
			return true, false, nil
		}
		changed = true
	}
	receipts := append(append([]Receipt(nil), m.session...), state.Receipts...)
	for _, receipt := range receipts {
		if receipt.Server != server || receipt.ConfigSource != configSource {
			continue
		}
		if receipt.Scope != ScopeGlobal && receipt.WorkspaceFingerprint != m.workspaceFingerprint {
			continue
		}
		if receipt.IdentityFingerprint == identityFingerprint {
			return true, false, nil
		}
		changed = true
	}
	return false, changed, nil
}

func (m *Manager) trust(scope Scope, source Source, server, configSource, identityFingerprint, catalogEntryID string, capabilities []Capability, selectedReaders map[string]bool) error {
	if !validScope(scope) {
		return fmt.Errorf("invalid MCP trust scope %q", scope)
	}
	if strings.TrimSpace(server) == "" || strings.TrimSpace(identityFingerprint) == "" {
		return fmt.Errorf("MCP trust requires server and identity fingerprint")
	}
	if scope == ScopeGlobal && source != SourceOfficialCatalog {
		return fmt.Errorf("global MCP trust is reserved for official catalog entries")
	}
	now := time.Now().UTC()
	receipt, err := buildReceipt(scope, source, m.workspaceFingerprint, server, configSource, identityFingerprint, catalogEntryID, capabilities, selectedReaders, now)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if scope == ScopeSession {
		m.session = upsertReceipt(m.session, receipt)
		return nil
	}
	if scope == ScopeWorkspace {
		m.session = removeReceiptSlot(m.session, receipt.Server, receipt.ConfigSource, m.workspaceFingerprint)
	}
	return m.updatePersistent(func(state *State) {
		state.Receipts = upsertReceipt(state.Receipts, receipt)
	})
}

// ImportLegacy records only the explicitly configured reader names. Live
// capability comparison upgrades the receipt without trusting new tools.
func (m *Manager) ImportLegacy(server, configSource, identityFingerprint string, rawReaders []string) error {
	caps := make([]Capability, 0, len(rawReaders))
	for _, name := range cleanStrings(rawReaders, false) {
		caps = append(caps, Capability{RawName: name, ModelName: name, ReadOnly: true})
	}
	if len(caps) == 0 {
		return nil
	}
	if err := m.Trust(ScopeWorkspace, SourceLegacyImport, server, configSource, identityFingerprint, "", caps); err != nil {
		return err
	}
	return m.MarkLegacyImported(server, configSource)
}

func (m *Manager) LegacyImported(server, configSource string) (bool, error) {
	key := m.legacyImportKey(server, configSource)
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return false, err
	}
	for _, imported := range state.LegacyImports {
		if imported == key {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) MarkLegacyImported(server, configSource string) error {
	key := m.legacyImportKey(server, configSource)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		state.LegacyImports = append(state.LegacyImports, key)
	})
}

func (m *Manager) legacyImportKey(server, configSource string) string {
	payload := strings.Join([]string{m.workspaceFingerprint, strings.TrimSpace(server), strings.TrimSpace(configSource)}, "\x00")
	return digestBytes([]byte(payload))
}

func (m *Manager) Revoke(server string) error {
	server = strings.TrimSpace(server)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.session = removeReceipts(m.session, server, m.workspaceFingerprint)
	m.sessionLaunch = removeLaunchGrants(m.sessionLaunch, server, m.workspaceFingerprint)
	return m.updatePersistent(func(state *State) {
		state.Receipts = removeReceipts(state.Receipts, server, m.workspaceFingerprint)
		state.LaunchGrants = removeLaunchGrants(state.LaunchGrants, server, m.workspaceFingerprint)
	})
}

// RevokeOfficial removes only receipts created by one signed catalog entry;
// user workspace/session decisions for an unrelated server remain intact.
func (m *Manager) RevokeOfficial(catalogEntryID string) error {
	catalogEntryID = strings.TrimSpace(catalogEntryID)
	if catalogEntryID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		out := state.Receipts[:0]
		for _, receipt := range state.Receipts {
			if receipt.Source == SourceOfficialCatalog && receipt.CatalogEntryID == catalogEntryID {
				continue
			}
			out = append(out, receipt)
		}
		state.Receipts = out
	})
}

// DenyOfficial records a local user override so an otherwise valid signed
// catalog entry is not silently re-trusted on the next connection.
func (m *Manager) DenyOfficial(catalogEntryID string) error {
	catalogEntryID = strings.TrimSpace(catalogEntryID)
	if catalogEntryID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		state.OfficialDenials = append(state.OfficialDenials, catalogEntryID)
	})
}

func (m *Manager) AllowOfficial(catalogEntryID string) error {
	catalogEntryID = strings.TrimSpace(catalogEntryID)
	if catalogEntryID == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.updatePersistent(func(state *State) {
		out := state.OfficialDenials[:0]
		for _, denied := range state.OfficialDenials {
			if denied != catalogEntryID {
				out = append(out, denied)
			}
		}
		state.OfficialDenials = out
	})
}

func (m *Manager) OfficialDenied(catalogEntryID string) (bool, error) {
	catalogEntryID = strings.TrimSpace(catalogEntryID)
	if catalogEntryID == "" {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return false, err
	}
	for _, denied := range state.OfficialDenials {
		if denied == catalogEntryID {
			return true, nil
		}
	}
	return false, nil
}

// GetLauncherLock returns the immutable launcher resolution for this server,
// original locator, and workspace. Global official plugins are pinned by their
// signed package digest instead and do not use mutable launcher locks.
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

// PutLauncherLock atomically persists one exact package/version/content
// resolution without modifying the user's MCP configuration.
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

// LauncherLockFingerprint binds a receipt to the exact local launcher
// resolution while keeping the mutable user's original config unchanged.
func LauncherLockFingerprint(lock LauncherLock) string {
	payload := struct {
		Server, Workspace, Locator, ResolvedVersion, ContentSHA256 string
	}{lock.Server, lock.Workspace, lock.Locator, lock.ResolvedVersion, lock.ContentSHA256}
	body, _ := json.Marshal(payload)
	return digestBytes(body)
}

// HasReceipt reports whether a receipt already exists for server in the
// current workspace or globally. It is used to make legacy config migration a
// one-time import instead of re-authorizing a tool after later safety drift.
func (m *Manager) HasReceipt(server, configSource string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return false, err
	}
	receipts := append(append([]Receipt(nil), m.session...), state.Receipts...)
	_, ok := selectReceipt(receipts, strings.TrimSpace(server), strings.TrimSpace(configSource), m.workspaceFingerprint)
	return ok, nil
}

// IdentityChanged compares only the pre-execution server identity. It lets the
// host stop a changed binary/endpoint before spawning it; capability drift is
// checked after an explicit sandboxed preflight lists tools.
func (m *Manager) IdentityChanged(server, configSource, identityFingerprint string) (hasReceipt, changed bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return false, false, err
	}
	receipts := append(append([]Receipt(nil), m.session...), state.Receipts...)
	receipt, ok := selectReceipt(receipts, strings.TrimSpace(server), strings.TrimSpace(configSource), m.workspaceFingerprint)
	if !ok {
		return false, false, nil
	}
	return true, receipt.IdentityFingerprint != identityFingerprint, nil
}

// OfficialAuthority is the signed catalog's live reader policy for one entry.
// It is evaluation-time input only: the stored receipt keeps the identity and
// capability drift snapshot, while the current signed allowlist decides which
// unchanged capabilities act as trusted readers. Receipts therefore never gain
// authority the current catalog no longer signs.
type OfficialAuthority struct {
	CatalogEntryID string
	Readers        []string
}

func (m *Manager) Evaluate(server, configSource, identityFingerprint string, capabilities []Capability) (Evaluation, error) {
	return m.evaluate(server, configSource, identityFingerprint, capabilities, nil)
}

// EvaluateOfficial evaluates an official catalog server against both its
// receipt snapshot and the current signed reader allowlist. A reader removed
// from the catalog loses authority immediately; a newly listed reader is
// granted only when its already-verified capability snapshot still matches and
// it remains a non-destructive reader. Every caller that can execute official
// tools — the live handshake and the schema-cache preflight alike — must use
// this entry point instead of Evaluate.
func (m *Manager) EvaluateOfficial(server, configSource, identityFingerprint string, capabilities []Capability, authority OfficialAuthority) (Evaluation, error) {
	return m.evaluate(server, configSource, identityFingerprint, capabilities, &authority)
}

func (m *Manager) evaluate(server, configSource, identityFingerprint string, capabilities []Capability, authority *OfficialAuthority) (Evaluation, error) {
	eval := Evaluation{State: TrustUntrusted, TrustedReaders: map[string]bool{}}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.Load()
	if err != nil {
		return eval, err
	}
	receipts := append(append([]Receipt(nil), m.session...), state.Receipts...)
	receipt, ok := selectReceipt(receipts, strings.TrimSpace(server), strings.TrimSpace(configSource), m.workspaceFingerprint)
	if !ok {
		return eval, nil
	}
	eval.Source, eval.Scope = receipt.Source, receipt.Scope
	var officialReaders map[string]bool
	if authority != nil && receipt.Source == SourceOfficialCatalog {
		if receipt.CatalogEntryID != strings.TrimSpace(authority.CatalogEntryID) {
			// The receipt was issued by a different catalog entry. Nothing in it
			// may grant authority for the current entry.
			return eval, nil
		}
		officialReaders = make(map[string]bool, len(authority.Readers))
		for _, name := range authority.Readers {
			if name = strings.TrimSpace(name); name != "" {
				officialReaders[name] = true
			}
		}
	}
	if receipt.IdentityFingerprint != identityFingerprint {
		eval.State = TrustChanged
		eval.IdentityChanged = true
		return eval, nil
	}
	eval.State = stateForReceipt(receipt)
	live := make(map[string]Capability, len(capabilities))
	for _, cap := range capabilities {
		live[cap.RawName] = cap
	}
	for _, saved := range receipt.Tools {
		cap, exists := live[saved.RawName]
		if !exists {
			continue // removal does not invalidate other readers
		}
		fp, fpErr := CapabilityFingerprint(cap)
		if fpErr != nil {
			return eval, fpErr
		}
		if saved.Fingerprint != fp || saved.ReadOnly != cap.ReadOnly || saved.Destructive != cap.Destructive {
			eval.ChangedTools = append(eval.ChangedTools, saved.RawName)
			eval.ToolChanges = append(eval.ToolChanges, ToolChange{Name: saved.RawName, Kind: toolChangeKind(saved, cap)})
			continue
		}
		trustedReader := saved.TrustedReader
		if officialReaders != nil {
			// The live signed allowlist is the sole reader authority for
			// unchanged official capabilities; the receipt's historical flag is
			// only a drift snapshot.
			trustedReader = officialReaders[saved.RawName]
		}
		if trustedReader && cap.ReadOnly && !cap.Destructive {
			eval.TrustedReaders[saved.RawName] = true
		}
	}
	for _, cap := range capabilities {
		if _, ok := findToolReceipt(receipt.Tools, cap.RawName); !ok {
			eval.ChangedTools = append(eval.ChangedTools, cap.RawName)
			eval.ToolChanges = append(eval.ToolChanges, ToolChange{Name: cap.RawName, Kind: "added"})
		}
	}
	if len(eval.ChangedTools) > 0 {
		sort.Strings(eval.ChangedTools)
		eval.ChangedTools = compactStrings(eval.ChangedTools)
		sort.Slice(eval.ToolChanges, func(i, j int) bool {
			if eval.ToolChanges[i].Name != eval.ToolChanges[j].Name {
				return eval.ToolChanges[i].Name < eval.ToolChanges[j].Name
			}
			return eval.ToolChanges[i].Kind < eval.ToolChanges[j].Kind
		})
		eval.State = TrustChanged
	}
	return eval, nil
}

// MigrateIdentityFingerprint atomically rewrites receipts whose identity is
// exactly legacyFingerprint to currentFingerprint, for one server and config
// source. With live capabilities it refuses when any of them drifted from the
// receipt snapshot; nil capabilities (the pre-start gate, where nothing has
// listed tools yet) skip that comparison — the tool snapshot itself is never
// modified, so the post-handshake evaluation still performs the full drift
// check. A migration therefore can never widen trust, and it compares digests
// only — URLs and credential values are never read or persisted here. This
// keeps credential rotation and the credential-aware URL normalization rollout
// from demanding a redundant re-trust. Remove together with the legacy URL
// fingerprint calculator (see docs/MIGRATING.md).
func (m *Manager) MigrateIdentityFingerprint(server, configSource, legacyFingerprint, currentFingerprint string, capabilities []Capability) (bool, error) {
	return m.MigrateIdentityFingerprints(server, configSource, []string{configSource}, []string{legacyFingerprint}, currentFingerprint, capabilities)
}

// MigrateIdentityFingerprints upgrades receipts from a bounded set of legacy
// source labels and identity digests. It is used when runtime provenance becomes
// more precise (for example workspace_config -> user_config/project_mcp_json)
// without treating that host-only classification change as a new server.
func (m *Manager) MigrateIdentityFingerprints(server, configSource string, legacySources, legacyFingerprints []string, currentFingerprint string, capabilities []Capability) (bool, error) {
	server = strings.TrimSpace(server)
	configSource = strings.TrimSpace(configSource)
	currentFingerprint = strings.TrimSpace(currentFingerprint)
	sourceSet := map[string]bool{}
	for _, source := range legacySources {
		if source = strings.TrimSpace(source); source != "" {
			sourceSet[source] = true
		}
	}
	fingerprintSet := map[string]bool{}
	for _, fingerprint := range legacyFingerprints {
		if fingerprint = strings.TrimSpace(fingerprint); fingerprint != "" && fingerprint != currentFingerprint {
			fingerprintSet[fingerprint] = true
		}
	}
	if server == "" || configSource == "" || currentFingerprint == "" || len(sourceSet) == 0 || len(fingerprintSet) == 0 {
		return false, nil
	}
	upgrade := func(receipts []Receipt) bool {
		changed := false
		for i := range receipts {
			r := &receipts[i]
			if r.Server != server || !sourceSet[r.ConfigSource] {
				continue
			}
			if r.Scope != ScopeGlobal && r.WorkspaceFingerprint != m.workspaceFingerprint {
				continue
			}
			if !fingerprintSet[r.IdentityFingerprint] || receiptCapabilitiesChanged(*r, capabilities) {
				continue
			}
			r.ConfigSource = configSource
			r.IdentityFingerprint = currentFingerprint
			r.LastVerifiedAt = time.Now().UTC()
			changed = true
		}
		return changed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	migrated := upgrade(m.session)
	if strings.TrimSpace(m.path) != "" {
		state, err := m.Load()
		if err != nil {
			return migrated, err
		}
		if upgrade(state.Receipts) {
			persisted := false
			if err := m.updatePersistent(func(state *State) {
				persisted = upgrade(state.Receipts)
			}); err != nil {
				return migrated, err
			}
			migrated = migrated || persisted
		}
	}
	return migrated, nil
}

// receiptCapabilitiesChanged is the boolean twin of Evaluate's drift
// comparison: any fingerprint/safety change or newly added tool counts as
// drift, while a removed tool does not.
func receiptCapabilitiesChanged(receipt Receipt, capabilities []Capability) bool {
	live := make(map[string]Capability, len(capabilities))
	for _, cap := range capabilities {
		live[cap.RawName] = cap
	}
	for _, saved := range receipt.Tools {
		cap, exists := live[saved.RawName]
		if !exists {
			continue
		}
		fp, err := CapabilityFingerprint(cap)
		if err != nil {
			return true
		}
		if saved.Fingerprint != fp || saved.ReadOnly != cap.ReadOnly || saved.Destructive != cap.Destructive {
			return true
		}
	}
	for _, cap := range capabilities {
		if _, ok := findToolReceipt(receipt.Tools, cap.RawName); !ok {
			return true
		}
	}
	return false
}

func toolChangeKind(saved ToolReceipt, live Capability) string {
	switch {
	case saved.ReadOnly && !saved.Destructive && live.Destructive:
		return "reader_to_destructive"
	case saved.ReadOnly && !saved.Destructive && !live.ReadOnly:
		return "reader_to_writer"
	case !saved.ReadOnly && live.ReadOnly && !live.Destructive:
		return "writer_to_reader"
	case saved.Destructive != live.Destructive || saved.ReadOnly != live.ReadOnly:
		return "safety_changed"
	case saved.ModelName != strings.TrimSpace(live.ModelName):
		return "name_changed"
	default:
		return "schema_changed"
	}
}

func (m *Manager) updatePersistent(update func(*State)) error {
	if strings.TrimSpace(m.path) == "" {
		return fmt.Errorf("MCP trust state path is unavailable")
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

func buildReceipt(scope Scope, source Source, workspaceFP, server, configSource, identityFP, catalogEntryID string, capabilities []Capability, selectedReaders map[string]bool, now time.Time) (Receipt, error) {
	receipt := Receipt{
		Scope: scope, Server: strings.TrimSpace(server), ConfigSource: strings.TrimSpace(configSource),
		IdentityFingerprint: identityFP, Source: source, CatalogEntryID: strings.TrimSpace(catalogEntryID),
		CreatedAt: now, LastVerifiedAt: now,
	}
	if scope == ScopeWorkspace || scope == ScopeSession {
		receipt.WorkspaceFingerprint = workspaceFP
	}
	for _, cap := range capabilities {
		fp, err := CapabilityFingerprint(cap)
		if err != nil {
			return Receipt{}, fmt.Errorf("fingerprint MCP tool %q: %w", cap.RawName, err)
		}
		trustedReader := cap.ReadOnly && !cap.Destructive
		if selectedReaders != nil {
			trustedReader = trustedReader && selectedReaders[cap.RawName]
		}
		receipt.Tools = append(receipt.Tools, ToolReceipt{
			RawName: strings.TrimSpace(cap.RawName), ModelName: strings.TrimSpace(cap.ModelName), Fingerprint: fp,
			ReadOnly: cap.ReadOnly, Destructive: cap.Destructive,
			TrustedReader: trustedReader,
		})
	}
	sort.Slice(receipt.Tools, func(i, j int) bool { return receipt.Tools[i].RawName < receipt.Tools[j].RawName })
	return receipt, nil
}

func canonicalSecuritySchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	stripDisplayFields(value)
	body, err := json.Marshal(value)
	return json.RawMessage(body), err
}

func stripDisplayFields(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"description", "title", "examples", "$comment"} {
			delete(v, key)
		}
		for key, child := range v {
			// These keywords contain maps keyed by user-defined property/schema
			// names. Recurse into their values, not the container itself: deleting
			// a key named "title" from properties would erase a real argument from
			// the security fingerprint rather than a display-only annotation.
			// dependentRequired maps property names to name arrays and the legacy
			// dependencies keyword maps them to either a schema or a name array;
			// both forms keep their keys and constraint lists verbatim while
			// schema-form values still shed their display-only annotations.
			switch key {
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependentRequired", "dependencies":
				if named, ok := child.(map[string]any); ok {
					for _, schema := range named {
						stripDisplayFields(schema)
					}
					continue
				}
			}
			stripDisplayFields(child)
		}
	case []any:
		for _, child := range v {
			stripDisplayFields(child)
		}
	}
}

func normalizeState(state *State) {
	if state.Version == 0 {
		state.Version = StoreVersion
	}
	state.Receipts = dedupeReceipts(state.Receipts)
	state.LaunchGrants = dedupeLaunchGrants(state.LaunchGrants)
	for i := range state.Receipts {
		r := &state.Receipts[i]
		sort.Slice(r.Tools, func(i, j int) bool { return r.Tools[i].RawName < r.Tools[j].RawName })
	}
	sort.Slice(state.Receipts, func(i, j int) bool {
		a, b := state.Receipts[i], state.Receipts[j]
		if a.Server != b.Server {
			return a.Server < b.Server
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.WorkspaceFingerprint < b.WorkspaceFingerprint
	})
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
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.ConfigSource < b.ConfigSource
	})
	state.OfficialDenials = cleanStrings(state.OfficialDenials, false)
	state.LegacyImports = cleanStrings(state.LegacyImports, false)
}

func upsertLaunchGrant(grants []LaunchGrant, grant LaunchGrant) []LaunchGrant {
	for i := range grants {
		if sameLaunchGrantKey(grants[i], grant) {
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

func sameLaunchGrantKey(a, b LaunchGrant) bool {
	return a.Scope == b.Scope && a.WorkspaceFingerprint == b.WorkspaceFingerprint && a.Server == b.Server && a.ConfigSource == b.ConfigSource
}

func upsertReceipt(receipts []Receipt, receipt Receipt) []Receipt {
	for i := range receipts {
		if sameReceiptKey(receipts[i], receipt) {
			// A legacy import must never overwrite an explicit user decision;
			// the reverse replacement is deliberate.
			if receiptSourceRank(receipt.Source) < receiptSourceRank(receipts[i].Source) {
				return receipts
			}
			receipt.CreatedAt = receipts[i].CreatedAt
			receipts[i] = receipt
			return receipts
		}
	}
	return append(receipts, receipt)
}

// dedupeReceipts collapses states written before Source left the receipt key:
// one decision slot keeps the explicit user receipt over a legacy import, and
// within one source the most recently verified receipt. It runs on every load
// so old duplicated states normalize without a store-version bump.
func dedupeReceipts(receipts []Receipt) []Receipt {
	out := receipts[:0]
	for _, receipt := range receipts {
		merged := false
		for i := range out {
			if !sameReceiptKey(out[i], receipt) {
				continue
			}
			if betterReceipt(receipt, out[i]) {
				out[i] = receipt
			}
			merged = true
			break
		}
		if !merged {
			out = append(out, receipt)
		}
	}
	return out
}

func betterReceipt(a, b Receipt) bool {
	if ra, rb := receiptSourceRank(a.Source), receiptSourceRank(b.Source); ra != rb {
		return ra > rb
	}
	return a.LastVerifiedAt.After(b.LastVerifiedAt)
}

func removeReceipts(receipts []Receipt, server, workspaceFP string) []Receipt {
	out := receipts[:0]
	for _, receipt := range receipts {
		if receipt.Server == server && (receipt.Scope == ScopeGlobal || receipt.WorkspaceFingerprint == workspaceFP) {
			continue
		}
		out = append(out, receipt)
	}
	return out
}

func removeLaunchGrants(grants []LaunchGrant, server, workspaceFP string) []LaunchGrant {
	out := grants[:0]
	for _, grant := range grants {
		if grant.Server == server && (grant.Scope == ScopeSession || grant.WorkspaceFingerprint == workspaceFP) {
			continue
		}
		out = append(out, grant)
	}
	return out
}

func removeLaunchGrantSlot(grants []LaunchGrant, server, configSource, workspaceFP string) []LaunchGrant {
	out := grants[:0]
	for _, grant := range grants {
		if grant.Server == server && grant.ConfigSource == configSource && (grant.Scope == ScopeSession || grant.WorkspaceFingerprint == workspaceFP) {
			continue
		}
		out = append(out, grant)
	}
	return out
}

func removeReceiptSlot(receipts []Receipt, server, configSource, workspaceFP string) []Receipt {
	out := receipts[:0]
	for _, receipt := range receipts {
		if receipt.Server == server && receipt.ConfigSource == configSource && receipt.Scope == ScopeSession && receipt.WorkspaceFingerprint == workspaceFP {
			continue
		}
		out = append(out, receipt)
	}
	return out
}

func selectReceipt(receipts []Receipt, server, configSource, workspaceFP string) (Receipt, bool) {
	var candidates []Receipt
	for _, receipt := range receipts {
		if receipt.Server != server {
			continue
		}
		if receipt.ConfigSource != configSource {
			continue
		}
		if receipt.Scope != ScopeGlobal && receipt.WorkspaceFingerprint != workspaceFP {
			continue
		}
		candidates = append(candidates, receipt)
	}
	if len(candidates) == 0 {
		return Receipt{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return receiptRank(candidates[i]) > receiptRank(candidates[j])
	})
	return candidates[0], true
}

func receiptRank(receipt Receipt) int {
	if receipt.Source == SourceOfficialCatalog && receipt.Scope == ScopeGlobal {
		return 30
	}
	if receipt.Scope == ScopeSession {
		return 20
	}
	if receipt.Scope == ScopeWorkspace {
		return 10
	}
	return 0
}

func stateForReceipt(receipt Receipt) TrustState {
	if receipt.Source == SourceOfficialCatalog && receipt.Scope == ScopeGlobal {
		return TrustOfficial
	}
	if receipt.Scope == ScopeSession {
		return TrustSession
	}
	return TrustWorkspace
}

func findToolReceipt(tools []ToolReceipt, rawName string) (ToolReceipt, bool) {
	for _, tool := range tools {
		if tool.RawName == rawName {
			return tool, true
		}
	}
	return ToolReceipt{}, false
}

// sameReceiptKey identifies one workspace trust decision. Source is
// deliberately not part of the key: an explicit user receipt and a legacy
// import for the same scope/workspace/server/config source describe the same
// decision and must not coexist.
func sameReceiptKey(a, b Receipt) bool {
	return a.Scope == b.Scope && a.WorkspaceFingerprint == b.WorkspaceFingerprint && a.Server == b.Server && a.ConfigSource == b.ConfigSource
}

// receiptSourceRank orders receipt sources for one decision slot: an explicit
// user (or signed catalog) receipt always outranks a legacy import.
func receiptSourceRank(source Source) int {
	if source == SourceLegacyImport {
		return 1
	}
	return 2
}

func validScope(scope Scope) bool {
	return scope == ScopeSession || scope == ScopeWorkspace || scope == ScopeGlobal
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

func canonicalPaths(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if path := canonicalPath(value); path != "" {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return compactStrings(out)
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

// acquireFileLock is a portable advisory lock based on exclusive creation.
// AtomicWriteFile still provides crash-safe replacement; the lock serializes
// independent Reasonix processes performing read-modify-write cycles.
func acquireFileLock(path string, wait time.Duration) (func(), error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate MCP trust lock owner: %w", err)
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
		if !errors.Is(err, os.ErrExist) && !trustLockContention(err) {
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
			return nil, fmt.Errorf("timed out waiting for MCP trust state lock")
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
	return err == nil && trustLockProcessAlive(pid)
}

// removeOwnedFileLock prevents an old holder from deleting a replacement lock
// after a stale-takeover race. The exact owner payload is the generation token.
func removeOwnedFileLock(path string, owner []byte) {
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(owner) {
		return
	}
	_ = os.Remove(path)
}
