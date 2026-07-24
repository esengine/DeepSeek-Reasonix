package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/sandbox"
	"reasonix/internal/sandboxauth"
)

type capabilityGrantDocument struct {
	Sandbox struct {
		CapabilityGrants []toml.Primitive `toml:"capability_grants"`
	} `toml:"sandbox"`
}

type capabilityGrantEntry struct {
	CanonicalExecutable         string                  `toml:"canonical_executable"`
	ArgvPrefix                  []string                `toml:"argv_prefix"`
	Network                     bool                    `toml:"network"`
	Reads                       []capabilityPathEntry   `toml:"reads"`
	Writes                      []capabilityPathEntry   `toml:"writes"`
	Devices                     []capabilityDeviceEntry `toml:"devices"`
	Background                  bool                    `toml:"background"`
	PreserveBackgroundProcesses bool                    `toml:"preserve_background_processes"`
}

type capabilityPathEntry struct {
	Identity string `toml:"identity"`
	Path     string `toml:"path"`
	Kind     string `toml:"kind"`
}

type capabilityDeviceEntry struct {
	Path  string `toml:"path"`
	Kind  string `toml:"kind"`
	Major uint32 `toml:"major"`
	Minor uint32 `toml:"minor"`
}

// CapabilityGrantStore is the project-scoped source and atomic persister used
// by the runtime authorization engine.
type CapabilityGrantStore struct {
	Workspace string
}

// SandboxCapabilityGrants implements sandboxauth.GrantSource.
func (s CapabilityGrantStore) SandboxCapabilityGrants(_ context.Context, workspace string) ([]sandboxauth.Grant, []sandboxauth.Diagnostic) {
	if strings.TrimSpace(workspace) == "" {
		workspace = s.Workspace
	}
	return LoadProjectCapabilityGrants(workspace)
}

// SaveSandboxCapabilityGrant implements sandboxauth.Persister.
func (s CapabilityGrantStore) SaveSandboxCapabilityGrant(_ context.Context, grant sandboxauth.Grant) error {
	workspace := s.Workspace
	if strings.TrimSpace(workspace) == "" {
		workspace = grant.Workspace
	}
	return PersistProjectCapabilityGrant(workspace, grant)
}

// loadCapabilityGrantsFrom reads and parses [[sandbox.capability_grants]] from
// the given file, validating each entry against root. Returns nil when the
// file does not exist.
func loadCapabilityGrantsFrom(path, root string) ([]sandboxauth.Grant, []sandboxauth.Diagnostic) {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []sandboxauth.Diagnostic{{Source: path, Entry: -1, Code: "read", Message: err.Error()}}
	}
	var document capabilityGrantDocument
	if _, err := toml.Decode(string(data), &document); err != nil {
		return nil, []sandboxauth.Diagnostic{{Source: path, Entry: -1, Code: "toml", Message: fmt.Sprintf("load capability grants: %v", err)}}
	}
	grants := make([]sandboxauth.Grant, 0, len(document.Sandbox.CapabilityGrants))
	diagnostics := make([]sandboxauth.Diagnostic, 0)
	for i, primitive := range document.Sandbox.CapabilityGrants {
		var entry capabilityGrantEntry
		if err := toml.PrimitiveDecode(primitive, &entry); err != nil {
			diagnostics = append(diagnostics, grantDiagnostic(path, i, "decode", err))
			continue
		}
		grant, err := validateCapabilityGrantEntry(root, entry)
		if err != nil {
			diagnostics = append(diagnostics, grantDiagnostic(path, i, "invalid", err))
			continue
		}
		duplicate := false
		for _, existing := range grants {
			if canonicalGrantEqual(existing, grant) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			grants = append(grants, grant)
		}
	}
	return grants, diagnostics
}

// LoadProjectCapabilityGrants validates each project entry independently and
// returns exact-canonical duplicates only once.
func LoadProjectCapabilityGrants(workspace string) ([]sandboxauth.Grant, []sandboxauth.Diagnostic) {
	root, err := canonicalCapabilityWorkspace(workspace)
	if err != nil {
		return nil, []sandboxauth.Diagnostic{{Source: capabilityConfigPath(workspace), Entry: -1, Code: "workspace", Message: err.Error()}}
	}
	path := capabilityConfigPath(root)
	return loadCapabilityGrantsFrom(path, root)
}

// LoadUserCapabilityGrants reads capability grants from the user-level config
// file (~/.reasonix/config.toml). Returns nil when the file does not exist.
func LoadUserCapabilityGrants() ([]sandboxauth.Grant, []sandboxauth.Diagnostic) {
	path := UserConfigPath()
	if path == "" {
		return nil, nil
	}
	userHome, _ := os.UserHomeDir()
	root := userHome
	if root == "" {
		root = "/"
	}
	return loadCapabilityGrantsFrom(path, root)
}

// UserCapabilityGrantStore implements sandboxauth.GrantSource for the
// user-level config file.
type UserCapabilityGrantStore struct{}

// SandboxCapabilityGrants implements sandboxauth.GrantSource.
func (s UserCapabilityGrantStore) SandboxCapabilityGrants(_ context.Context, _ string) ([]sandboxauth.Grant, []sandboxauth.Diagnostic) {
	return LoadUserCapabilityGrants()
}

// PersistProjectCapabilityGrant performs the capability-specific locked,
// strict-fresh, targeted append transaction.
func PersistProjectCapabilityGrant(workspace string, grant sandboxauth.Grant) error {
	root, err := canonicalCapabilityWorkspace(workspace)
	if err != nil {
		return fmt.Errorf("persist capability grant: canonicalize workspace: %w", err)
	}
	path := capabilityConfigPath(root)
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return fmt.Errorf("persist capability grant: lock config: %w", err)
	}
	defer unlock()
	if _, err := LoadForEditReadOnlyStrict(path); err != nil {
		return fmt.Errorf("persist capability grant: strict fresh read: %w", err)
	}
	entry, canonical, err := capabilityGrantForPersistence(root, grant)
	if err != nil {
		return fmt.Errorf("persist capability grant: validate grant: %w", err)
	}
	existing, _ := LoadProjectCapabilityGrants(root)
	for _, current := range existing {
		if canonicalGrantEqual(current, canonical) {
			return nil
		}
	}
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persist capability grant: read config: %w", err)
	}
	body := strings.TrimRight(string(raw), "\n")
	if body != "" {
		body += "\n\n"
	}
	if !tomlBodyHasSection(body, "sandbox") {
		body += "[sandbox]\n\n"
	}
	body += renderCapabilityGrantEntry(entry)
	if _, err := toml.Decode(body, &map[string]any{}); err != nil {
		return fmt.Errorf("persist capability grant: validate appended config: %w", err)
	}
	if err := writeConfigFile(path, body); err != nil {
		return fmt.Errorf("persist capability grant: write config: %w", err)
	}
	return nil
}

// userConfigPathForGrants returns the path and root for user-level grant operations.
func userConfigPathForGrants() (path, root string) {
	path = UserConfigPath()
	if path == "" {
		return "", ""
	}
	userHome, _ := os.UserHomeDir()
	root = userHome
	if root == "" {
		root = "/"
	}
	return path, root
}

// PersistUserCapabilityGrant appends a grant to the user-level config file.
func PersistUserCapabilityGrant(grant sandboxauth.Grant) error {
	path, root := userConfigPathForGrants()
	if path == "" {
		return fmt.Errorf("persist user capability grant: cannot resolve user config path")
	}
	unlock := LockUserConfigEdits()
	defer unlock()
	entry, canonical, err := capabilityGrantForPersistence(root, grant)
	if err != nil {
		return fmt.Errorf("persist user capability grant: %w", err)
	}
	existing, _ := LoadUserCapabilityGrants()
	for _, current := range existing {
		if canonicalGrantEqual(current, canonical) {
			return nil
		}
	}
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("persist user capability grant: read config: %w", err)
	}
	body := strings.TrimRight(string(raw), "\n")
	if body != "" {
		body += "\n\n"
	}
	if !tomlBodyHasSection(body, "sandbox") {
		body += "[sandbox]\n\n"
	}
	body += renderCapabilityGrantEntry(entry)
	if _, err := toml.Decode(body, &map[string]any{}); err != nil {
		return fmt.Errorf("persist user capability grant: validate: %w", err)
	}
	return writeConfigFile(path, body)
}

// DeleteProjectCapabilityGrant removes the first grant matching `match` from
// the project-level config file, comparing by canonical equality.
func DeleteProjectCapabilityGrant(workspace string, match sandboxauth.Grant) error {
	root, err := canonicalCapabilityWorkspace(workspace)
	if err != nil {
		return fmt.Errorf("delete project capability grant: %w", err)
	}
	path := capabilityConfigPath(root)
	unlock, err := LockConfigFileEdits(path)
	if err != nil {
		return fmt.Errorf("delete project capability grant: lock: %w", err)
	}
	defer unlock()
	return deleteGrantFromFile(path, root, match)
}

// DeleteUserCapabilityGrant removes the first grant matching `match` from the
// user-level config file, comparing by canonical equality.
func DeleteUserCapabilityGrant(match sandboxauth.Grant) error {
	path, root := userConfigPathForGrants()
	if path == "" {
		return fmt.Errorf("delete user capability grant: cannot resolve user config path")
	}
	unlock := LockUserConfigEdits()
	defer unlock()
	return deleteGrantFromFile(path, root, match)
}

// deleteGrantFromFile removes the first grant matching `match` from the file at
// path and rewrites the file. Callers must hold the appropriate lock.
func deleteGrantFromFile(path, root string, match sandboxauth.Grant) error {
	grants, _ := loadCapabilityGrantsFrom(path, root)
	index := -1
	for i, g := range grants {
		if canonicalGrantEqual(g, match) {
			index = i
			break
		}
	}
	if index == -1 {
		return nil // nothing to delete
	}
	grants = append(grants[:index], grants[index+1:]...)
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("delete grant: read: %w", err)
	}
	body := string(raw)
	marker := "\n[[sandbox.capability_grants]]"
	split := strings.Index(body, marker)
	if split < 0 {
		return nil // no grants section found
	}
	header := body[:split]
	var b strings.Builder
	b.WriteString(strings.TrimRight(header, "\n"))
	for _, g := range grants {
		entry, _, err := capabilityGrantForPersistence(root, g)
		if err != nil {
			return fmt.Errorf("delete grant: normalize: %w", err)
		}
		b.WriteString("\n")
		b.WriteString(renderCapabilityGrantEntry(entry))
	}
	return writeConfigFile(path, b.String())
}

// CapabilityGrantEntryForDesktop is the desktop-settings-panel representation of a
// capability grant entry, used to validate and persist user-created grants.
type CapabilityGrantEntryForDesktop struct {
	CanonicalExecutable         string
	ArgvPrefix                  []string
	Network                     bool
	Background                  bool
	PreserveBackgroundProcesses bool
	Reads                       []struct {
		Identity string
		Path     string
		Kind     string
	}
	Writes []struct {
		Identity string
		Path     string
		Kind     string
	}
	Devices []struct {
		Path  string
		Kind  string
		Major uint32
		Minor uint32
	}
}

// ValidateGrantView validates a CapabilityGrantEntryForDesktop against the given
// root and returns a canonical sandboxauth.Grant. When root is empty, defaults to
// user home directory.
func ValidateGrantView(entry CapabilityGrantEntryForDesktop, root string) (sandboxauth.Grant, error) {
	if root == "" {
		userHome, _ := os.UserHomeDir()
		root = userHome
		if root == "" {
			root = "/"
		}
	}
	toEntry := capabilityGrantEntry{
		CanonicalExecutable:         entry.CanonicalExecutable,
		ArgvPrefix:                  entry.ArgvPrefix,
		Network:                     entry.Network,
		Background:                  entry.Background,
		PreserveBackgroundProcesses: entry.PreserveBackgroundProcesses,
	}
	for _, r := range entry.Reads {
		toEntry.Reads = append(toEntry.Reads, capabilityPathEntry{Identity: r.Identity, Path: r.Path, Kind: r.Kind})
	}
	for _, w := range entry.Writes {
		toEntry.Writes = append(toEntry.Writes, capabilityPathEntry{Identity: w.Identity, Path: w.Path, Kind: w.Kind})
	}
	for _, d := range entry.Devices {
		toEntry.Devices = append(toEntry.Devices, capabilityDeviceEntry{Path: d.Path, Kind: d.Kind, Major: d.Major, Minor: d.Minor})
	}
	return validateCapabilityGrantEntry(root, toEntry)
}

func validateCapabilityGrantEntry(root string, entry capabilityGrantEntry) (sandboxauth.Grant, error) {
	executable := filepath.Clean(strings.TrimSpace(entry.CanonicalExecutable))
	if executable == "." || !filepath.IsAbs(executable) {
		return sandboxauth.Grant{}, fmt.Errorf("canonical_executable must be absolute")
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	if len(entry.ArgvPrefix) == 0 {
		return sandboxauth.Grant{}, fmt.Errorf("argv_prefix must not be empty")
	}
	for _, arg := range entry.ArgvPrefix {
		if strings.ContainsRune(arg, '\x00') {
			return sandboxauth.Grant{}, fmt.Errorf("argv_prefix contains NUL")
		}
	}
	reads, err := validateCapabilityPaths(root, entry.Reads)
	if err != nil {
		return sandboxauth.Grant{}, fmt.Errorf("reads: %w", err)
	}
	writes, err := validateCapabilityPaths(root, entry.Writes)
	if err != nil {
		return sandboxauth.Grant{}, fmt.Errorf("writes: %w", err)
	}
	devices := make([]sandbox.CapabilityDevice, 0, len(entry.Devices))
	for i, device := range entry.Devices {
		kind := sandbox.CapabilityDeviceKind(device.Kind)
		if kind != sandbox.CapabilityCharacterDevice && kind != sandbox.CapabilityBlockDevice {
			return sandboxauth.Grant{}, fmt.Errorf("devices[%d].kind is invalid", i)
		}
		if !filepath.IsAbs(strings.TrimSpace(device.Path)) {
			return sandboxauth.Grant{}, fmt.Errorf("devices[%d].path must be absolute", i)
		}
		canonical, err := canonicalAbsolutePath(device.Path)
		if err != nil {
			return sandboxauth.Grant{}, fmt.Errorf("devices[%d].path: %w", i, err)
		}
		maj, min := device.Major, device.Minor
		if maj == 0 && min == 0 {
			if resolved, err := sandbox.InspectCapabilityDevice(device.Path); err == nil {
				maj, min = resolved.Major, resolved.Minor
			}
		}
		devices = append(devices, sandbox.CapabilityDevice{Path: device.Path, Canonical: canonical, Kind: kind, Major: maj, Minor: min})
	}
	if !entry.Network && len(reads) == 0 && len(writes) == 0 && len(devices) == 0 {
		return sandboxauth.Grant{}, fmt.Errorf("capability bundle is empty")
	}
	sort.Slice(reads, func(i, j int) bool { return capabilityPathSortKey(reads[i]) < capabilityPathSortKey(reads[j]) })
	sort.Slice(writes, func(i, j int) bool { return capabilityPathSortKey(writes[i]) < capabilityPathSortKey(writes[j]) })
	sort.Slice(devices, func(i, j int) bool { return devices[i].Canonical < devices[j].Canonical })
	return sandboxauth.Grant{Workspace: root, CanonicalExecutable: executable, ArgvPrefix: append([]string(nil), entry.ArgvPrefix...), Capabilities: sandbox.CapabilitySet{Network: entry.Network, Reads: reads, Writes: writes, Devices: devices}, Background: entry.Background, PreserveBackgroundProcesses: entry.PreserveBackgroundProcesses}, nil
}

func validateCapabilityPaths(root string, entries []capabilityPathEntry) ([]sandbox.CapabilityPath, error) {
	paths := make([]sandbox.CapabilityPath, 0, len(entries))
	for i, entry := range entries {
		identity := sandbox.CapabilityPathIdentity(entry.Identity)
		if identity != sandbox.WorkspaceRelative && identity != sandbox.CanonicalAbsolute {
			return nil, fmt.Errorf("entry %d identity is invalid", i)
		}
		kind := sandbox.CapabilityPathKind(entry.Kind)
		if kind != sandbox.CapabilityFile && kind != sandbox.CapabilityDirectory {
			return nil, fmt.Errorf("entry %d kind is invalid", i)
		}
		logical := strings.TrimSpace(entry.Path)
		var canonical string
		var err error
		if identity == sandbox.WorkspaceRelative {
			if logical == "" || filepath.IsAbs(logical) {
				return nil, fmt.Errorf("entry %d workspace_relative path must be relative", i)
			}
			canonical, err = canonicalAbsolutePath(filepath.Join(root, logical))
			if err == nil {
				rel, relErr := filepath.Rel(root, canonical)
				if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					err = fmt.Errorf("escapes workspace")
				}
			}
		} else {
			if !filepath.IsAbs(logical) {
				return nil, fmt.Errorf("entry %d canonical_absolute path must be absolute", i)
			}
			canonical, err = canonicalAbsolutePath(logical)
		}
		if err != nil {
			return nil, fmt.Errorf("entry %d path: %w", i, err)
		}
		paths = append(paths, sandbox.CapabilityPath{Identity: identity, Path: logical, Canonical: canonical, Kind: kind})
	}
	return paths, nil
}

func capabilityGrantForPersistence(root string, grant sandboxauth.Grant) (capabilityGrantEntry, sandboxauth.Grant, error) {
	entry := capabilityGrantEntry{CanonicalExecutable: grant.CanonicalExecutable, ArgvPrefix: append([]string(nil), grant.ArgvPrefix...), Network: grant.Capabilities.Network, Background: grant.Background, PreserveBackgroundProcesses: grant.PreserveBackgroundProcesses}
	for _, path := range grant.Capabilities.Reads {
		entry.Reads = append(entry.Reads, persistedCapabilityPath(root, path))
	}
	for _, path := range grant.Capabilities.Writes {
		entry.Writes = append(entry.Writes, persistedCapabilityPath(root, path))
	}
	for _, device := range grant.Capabilities.Devices {
		entry.Devices = append(entry.Devices, capabilityDeviceEntry{Path: firstNonBlank(device.Path, device.Canonical), Kind: string(device.Kind), Major: device.Major, Minor: device.Minor})
	}
	canonical, err := validateCapabilityGrantEntry(root, entry)
	if err != nil {
		return entry, sandboxauth.Grant{}, fmt.Errorf("normalize persisted grant: %w", err)
	}
	return entry, canonical, nil
}

func persistedCapabilityPath(root string, path sandbox.CapabilityPath) capabilityPathEntry {
	identity := path.Identity
	logical := path.Path
	if identity == "" {
		identity = sandbox.CanonicalAbsolute
		logical = path.Canonical
		if rel, err := filepath.Rel(root, path.Canonical); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			identity = sandbox.WorkspaceRelative
			logical = rel
		}
	}
	return capabilityPathEntry{Identity: string(identity), Path: logical, Kind: string(path.Kind)}
}

func renderCapabilityGrantEntry(entry capabilityGrantEntry) string {
	var b strings.Builder
	b.WriteString("[[sandbox.capability_grants]]\n")
	fmt.Fprintf(&b, "canonical_executable = %s\n", strconv.Quote(entry.CanonicalExecutable))
	fmt.Fprintf(&b, "argv_prefix = %s\n", renderStringArray(entry.ArgvPrefix))
	fmt.Fprintf(&b, "network = %v\n", entry.Network)
	fmt.Fprintf(&b, "background = %v\n", entry.Background)
	fmt.Fprintf(&b, "preserve_background_processes = %v\n", entry.PreserveBackgroundProcesses)
	for _, path := range entry.Reads {
		b.WriteString("[[sandbox.capability_grants.reads]]\n")
		renderCapabilityPath(&b, path)
	}
	for _, path := range entry.Writes {
		b.WriteString("[[sandbox.capability_grants.writes]]\n")
		renderCapabilityPath(&b, path)
	}
	for _, device := range entry.Devices {
		b.WriteString("[[sandbox.capability_grants.devices]]\n")
		fmt.Fprintf(&b, "path = %s\nkind = %s\nmajor = %d\nminor = %d\n", strconv.Quote(device.Path), strconv.Quote(device.Kind), device.Major, device.Minor)
	}
	return b.String()
}

func renderCapabilityPath(b *strings.Builder, path capabilityPathEntry) {
	fmt.Fprintf(b, "identity = %s\npath = %s\nkind = %s\n", strconv.Quote(path.Identity), strconv.Quote(path.Path), strconv.Quote(path.Kind))
}

func canonicalCapabilityWorkspace(workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		workspace = "."
	}
	return canonicalAbsolutePath(workspace)
}

func canonicalAbsolutePath(path string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func capabilityConfigPath(workspace string) string { return filepath.Join(workspace, "reasonix.toml") }

func grantDiagnostic(path string, entry int, code string, err error) sandboxauth.Diagnostic {
	return sandboxauth.Diagnostic{Source: path, Entry: entry, Code: code, Message: fmt.Sprintf("%s capability_grants[%d]: %v", path, entry, err)}
}

func capabilityPathSortKey(path sandbox.CapabilityPath) string {
	return path.Canonical + "\x00" + string(path.Kind)
}

func canonicalGrantEqual(a, b sandboxauth.Grant) bool {
	canonicalize := func(grant sandboxauth.Grant) sandboxauth.Grant {
		grant.ArgvPrefix = append([]string(nil), grant.ArgvPrefix...)
		grant.Capabilities.Reads = append([]sandbox.CapabilityPath(nil), grant.Capabilities.Reads...)
		grant.Capabilities.Writes = append([]sandbox.CapabilityPath(nil), grant.Capabilities.Writes...)
		grant.Capabilities.Devices = append([]sandbox.CapabilityDevice(nil), grant.Capabilities.Devices...)
		for i := range grant.Capabilities.Reads {
			grant.Capabilities.Reads[i].Identity = ""
			grant.Capabilities.Reads[i].Path = ""
		}
		for i := range grant.Capabilities.Writes {
			grant.Capabilities.Writes[i].Identity = ""
			grant.Capabilities.Writes[i].Path = ""
		}
		for i := range grant.Capabilities.Devices {
			grant.Capabilities.Devices[i].Path = ""
		}
		return grant
	}
	return reflect.DeepEqual(canonicalize(a), canonicalize(b))
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
