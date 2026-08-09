package pluginpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/extensioncontract"
	fileencoding "reasonix/internal/fileutil/encoding"
)

// ManifestAPIVersionV2 is the only native plugin manifest apiVersion Reasonix
// accepts for install, doctor, and boot. v1 and legacy (no apiVersion) native
// manifests are rejected. The explicit migrate command is only for legacy
// pre-extension manifests that omit apiVersion.
const ManifestAPIVersionV2 = "reasonix.io/plugin/v2"

// CapabilityRef is the wire form of a capability in a v2 manifest.
type CapabilityRef struct {
	Namespace    string `json:"namespace"`
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Version      string `json:"version,omitempty"`
	VersionRange string `json:"versionRange,omitempty"`
	SchemaHash   string `json:"schemaHash,omitempty"`
	Optional     bool   `json:"optional,omitempty"`
}

// ToCapability converts a provides entry.
func (c CapabilityRef) ToCapability() extensioncontract.Capability {
	return extensioncontract.Capability{
		Key: extensioncontract.CapabilityKey{
			Namespace: strings.TrimSpace(c.Namespace),
			Kind:      strings.TrimSpace(c.Kind),
			ID:        strings.TrimSpace(c.ID),
		},
		Version:    strings.TrimSpace(c.Version),
		SchemaHash: strings.TrimSpace(c.SchemaHash),
	}
}

// ToRequirement converts a requires entry.
func (c CapabilityRef) ToRequirement() extensioncontract.Requirement {
	return extensioncontract.Requirement{
		Capability: extensioncontract.Capability{
			Key: extensioncontract.CapabilityKey{
				Namespace: strings.TrimSpace(c.Namespace),
				Kind:      strings.TrimSpace(c.Kind),
				ID:        strings.TrimSpace(c.ID),
			},
			Version:    strings.TrimSpace(c.Version),
			SchemaHash: strings.TrimSpace(c.SchemaHash),
		},
		VersionRange: strings.TrimSpace(c.VersionRange),
		Optional:     c.Optional,
	}
}

// checkAPIVersionV2 gates the v2 parser. Only the exact frozen string
// reasonix.io/plugin/v2 is accepted (no v2.0 / v2.1 aliases).
func checkAPIVersionV2(v string) error {
	if v == ManifestAPIVersionV2 {
		return nil
	}
	return fmt.Errorf("%s: unsupported apiVersion %q: want exact %s", NativeManifest, v, ManifestAPIVersionV2)
}

// rejectNonV2Native explains why a non-v2 native manifest is refused.
func rejectNonV2Native(apiVersion string) error {
	if apiVersion == "" {
		return fmt.Errorf("%s: missing apiVersion; native manifests must declare %s", NativeManifest, ManifestAPIVersionV2)
	}
	return checkAPIVersionV2(apiVersion)
}

type v2Root struct {
	APIVersion  string          `json:"apiVersion"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Homepage    string          `json:"homepage"`
	Repository  string          `json:"repository"`
	Requires    json.RawMessage `json:"requires"`
	Provides    json.RawMessage `json:"provides"`
	Contributes json.RawMessage `json:"contributes"`
	Runtime     json.RawMessage `json:"runtime"`
	// Resource path fields (same shapes as earlier native manifests).
	Skills     json.RawMessage              `json:"skills"`
	Commands   json.RawMessage              `json:"commands"`
	Hooks      map[string][]json.RawMessage `json:"hooks"`
	MCPServers map[string]json.RawMessage   `json:"mcpServers"`
}

func parseNativeV2(b []byte, root, apiVersion string) (Package, []string, error) {
	if err := checkAPIVersionV2(apiVersion); err != nil {
		return Package{}, nil, err
	}
	var raw v2Root
	if err := strictDecode(b, &raw, NativeManifest); err != nil {
		return Package{}, nil, err
	}
	var contrib v1Contributes
	if len(raw.Contributes) > 0 && string(raw.Contributes) != "null" {
		if err := strictDecode(raw.Contributes, &contrib, "contributes"); err != nil {
			return Package{}, nil, err
		}
	}

	legacySkills, err := parseV1PathList(raw.Skills, "skills")
	if err != nil {
		return Package{}, nil, err
	}
	legacyCommands, err := parseV1PathList(raw.Commands, "commands")
	if err != nil {
		return Package{}, nil, err
	}
	contribSkills, err := parseV1PathList(contrib.Skills, "contributes.skills")
	if err != nil {
		return Package{}, nil, err
	}
	contribAgents, err := parseV1PathList(contrib.Agents, "contributes.agents")
	if err != nil {
		return Package{}, nil, err
	}
	contribCommands, err := parseV1PathList(contrib.Commands, "contributes.commands")
	if err != nil {
		return Package{}, nil, err
	}
	contribPrompts, err := parseV1PathList(contrib.Prompts, "contributes.prompts")
	if err != nil {
		return Package{}, nil, err
	}
	contribThemes, err := parseV1PathList(contrib.Themes, "contributes.themes")
	if err != nil {
		return Package{}, nil, err
	}
	legacyHooks, err := parseV1HookMap(raw.Hooks, "hooks")
	if err != nil {
		return Package{}, nil, err
	}
	contribHooks, err := parseV1HookMap(contrib.Hooks, "contributes.hooks")
	if err != nil {
		return Package{}, nil, err
	}
	legacyMCP, err := parseV1MCPServerMap(raw.MCPServers, "mcpServers")
	if err != nil {
		return Package{}, nil, err
	}
	contribMCP, err := parseV1MCPServerMap(contrib.MCPServers, "contributes.mcpServers")
	if err != nil {
		return Package{}, nil, err
	}
	hooks, err := mergeV1Hooks(legacyHooks, contribHooks)
	if err != nil {
		return Package{}, nil, err
	}
	mcpServers, err := mergeV1MCPServers(legacyMCP, contribMCP)
	if err != nil {
		return Package{}, nil, err
	}
	runtime, err := parseV1Runtime(raw.Runtime)
	if err != nil {
		return Package{}, nil, err
	}
	requires, err := parseCapabilityRefs(raw.Requires, "requires", true)
	if err != nil {
		return Package{}, nil, err
	}
	provides, err := parseCapabilityRefs(raw.Provides, "provides", false)
	if err != nil {
		return Package{}, nil, err
	}

	manifest := Manifest{
		APIVersion:  ManifestAPIVersionV2,
		Name:        strings.TrimSpace(raw.Name),
		Version:     strings.TrimSpace(raw.Version),
		Description: strings.TrimSpace(raw.Description),
		Homepage:    strings.TrimSpace(raw.Homepage),
		Repository:  strings.TrimSpace(raw.Repository),
		Skills:      unionPathLists(legacySkills, contribSkills),
		Commands:    unionPathLists(legacyCommands, contribCommands),
		Agents:      contribAgents,
		Prompts:     contribPrompts,
		Themes:      contribThemes,
		Hooks:       hooks,
		MCPServers:  mcpServers,
		Runtime:     runtime,
		Requires:    requires,
		Provides:    provides,
	}
	if err := validateManifest(root, &manifest); err != nil {
		return Package{}, nil, err
	}
	if err := validateV2Capabilities(&manifest); err != nil {
		return Package{}, nil, err
	}
	var warnings []string
	pathWarnings, err := validateV1Paths(root, &manifest)
	warnings = append(warnings, pathWarnings...)
	if err != nil {
		return Package{}, warnings, err
	}
	pkg := Package{Root: root, ManifestKind: "reasonix", Manifest: manifest}
	pkg.Compatibility = compatibilityFor(pkg, nil)
	return pkg, warnings, nil
}

func parseCapabilityRefs(raw json.RawMessage, path string, asRequirement bool) ([]CapabilityRef, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s must be an array", path)
	}
	out := make([]CapabilityRef, 0, len(items))
	for i, item := range items {
		var ref CapabilityRef
		if err := strictDecode(item, &ref, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return nil, err
		}
		ref.Namespace = strings.TrimSpace(ref.Namespace)
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Version = strings.TrimSpace(ref.Version)
		ref.VersionRange = strings.TrimSpace(ref.VersionRange)
		ref.SchemaHash = strings.TrimSpace(ref.SchemaHash)
		if asRequirement {
			if err := ref.ToRequirement().Validate(); err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", path, i, err)
			}
		} else {
			if err := ref.ToCapability().Validate(); err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", path, i, err)
			}
		}
		out = append(out, ref)
	}
	return out, nil
}

func validateV2Capabilities(m *Manifest) error {
	seen := map[string]bool{}
	for i, p := range m.Provides {
		key := p.ToCapability().Key.String()
		if seen[key] {
			return fmt.Errorf("provides[%d]: duplicate capability %s", i, key)
		}
		seen[key] = true
	}
	return nil
}

// ComponentDescriptorFields projects a package into kernel component fields.
func (p Package) Requires() []extensioncontract.Requirement {
	out := make([]extensioncontract.Requirement, 0, len(p.Manifest.Requires))
	for _, r := range p.Manifest.Requires {
		out = append(out, r.ToRequirement())
	}
	return out
}

// ProvidesCapabilities returns the manifest capability ceiling.
func (p Package) ProvidesCapabilities() []extensioncontract.Capability {
	out := make([]extensioncontract.Capability, 0, len(p.Manifest.Provides))
	for _, c := range p.Manifest.Provides {
		out = append(out, c.ToCapability())
	}
	return out
}

// MigrateManifestToV2 converts a legacy native package into a v2
// manifest document. Dependencies that cannot be inferred are returned as
// errors rather than invented.
func MigrateManifestToV2(pkg Package) ([]byte, error) {
	if pkg.ManifestKind != "reasonix" && pkg.ManifestKind != "" {
		return nil, fmt.Errorf("migrate: only native reasonix manifests can be migrated (got %s)", pkg.ManifestKind)
	}
	if apiVersion := strings.TrimSpace(pkg.Manifest.APIVersion); apiVersion != "" {
		return nil, fmt.Errorf("migrate: only pre-extension manifests without apiVersion can be migrated (got %s)", apiVersion)
	}
	name := strings.TrimSpace(pkg.Manifest.Name)
	if name == "" {
		return nil, errors.New("migrate: manifest name is required")
	}
	version := strings.TrimSpace(pkg.Manifest.Version)
	if version == "" {
		version = "1.0.0"
	}
	doc := map[string]any{
		"apiVersion": ManifestAPIVersionV2,
		"name":       name,
		"version":    version,
	}
	if d := strings.TrimSpace(pkg.Manifest.Description); d != "" {
		doc["description"] = d
	}
	if h := strings.TrimSpace(pkg.Manifest.Homepage); h != "" {
		doc["homepage"] = h
	}
	if r := strings.TrimSpace(pkg.Manifest.Repository); r != "" {
		doc["repository"] = r
	}

	contrib := map[string]any{}
	if len(pkg.Manifest.Skills) > 0 {
		contrib["skills"] = pkg.Manifest.Skills
	}
	if len(pkg.Manifest.Agents) > 0 {
		contrib["agents"] = pkg.Manifest.Agents
	}
	if len(pkg.Manifest.Commands) > 0 {
		contrib["commands"] = pkg.Manifest.Commands
	}
	if len(pkg.Manifest.Prompts) > 0 {
		contrib["prompts"] = pkg.Manifest.Prompts
	}
	if len(pkg.Manifest.Themes) > 0 {
		contrib["themes"] = pkg.Manifest.Themes
	}
	if len(pkg.Manifest.Hooks) > 0 {
		contrib["hooks"] = pkg.Manifest.Hooks
	}
	if len(pkg.Manifest.MCPServers) > 0 {
		contrib["mcpServers"] = pkg.Manifest.MCPServers
	}
	if len(contrib) > 0 {
		doc["contributes"] = contrib
	}
	if rt := pkg.Manifest.Runtime; rt != nil {
		doc["runtime"] = rt
		// Infer provides from runtime capabilities when possible; otherwise leave empty.
		var provides []CapabilityRef
		for _, capName := range rt.Capabilities {
			switch capName {
			case "providers":
				return nil, fmt.Errorf("migrate: runtime capability %q requires explicit provides entries (cannot infer schemaHash)", capName)
			case "ui":
				return nil, fmt.Errorf("migrate: runtime capability %q requires explicit provides entries (cannot infer schemaHash)", capName)
			case "interceptors", "strategies":
				provides = append(provides, CapabilityRef{
					Namespace: "plugin/" + name,
					Kind:      capName,
					ID:        "default",
					Version:   "1.0.0",
				})
			}
		}
		if len(provides) > 0 {
			doc["provides"] = provides
		}
	}
	if len(pkg.Manifest.Requires) > 0 {
		doc["requires"] = pkg.Manifest.Requires
	}
	if len(pkg.Manifest.Provides) > 0 {
		doc["provides"] = pkg.Manifest.Provides
	}
	return json.MarshalIndent(doc, "", "  ")
}

// WriteMigratedManifestV2 writes a v2 manifest, keeping a .bak backup of the
// previous reasonix-plugin.json when present.
func WriteMigratedManifestV2(root string, data []byte) error {
	root = filepath.Clean(root)
	path := filepath.Join(root, NativeManifest)
	if b, err := os.ReadFile(path); err == nil {
		bak := path + ".bak"
		if err := os.WriteFile(bak, b, 0o644); err != nil {
			return fmt.Errorf("migrate: backup: %w", err)
		}
	}
	if !strings.HasSuffix(string(data), "\n") {
		data = append(data, '\n')
	}
	return os.WriteFile(path, data, 0o644)
}

// ParseNativeForMigrate accepts legacy native manifests without apiVersion for
// explicit migration only. Normal ParseDir rejects them; v1 is unsupported.
func ParseNativeForMigrate(root string) (Package, []string, error) {
	root = filepath.Clean(root)
	path := filepath.Join(root, NativeManifest)
	b, err := os.ReadFile(path)
	if err != nil {
		return Package{}, nil, err
	}
	apiVersion, err := sniffManifestAPIVersion(b)
	if err != nil {
		return Package{}, nil, err
	}
	if apiVersion == "" {
		return parseNativeLegacy(b, root)
	}
	if apiVersion == ManifestAPIVersionV2 {
		return Package{}, nil, fmt.Errorf("%s: already uses %s; migration only supports pre-extension manifests without apiVersion", NativeManifest, ManifestAPIVersionV2)
	}
	return Package{}, nil, rejectNonV2Native(apiVersion)
}

func parseNative(path, root string) (Package, []string, error) {
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return Package{}, nil, err
	}
	apiVersion, err := sniffManifestAPIVersion(b)
	if err != nil {
		return Package{}, nil, err
	}
	if apiVersion != ManifestAPIVersionV2 {
		return Package{}, nil, rejectNonV2Native(apiVersion)
	}
	return parseNativeV2(b, root, apiVersion)
}
