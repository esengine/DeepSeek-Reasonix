package runtimeservice

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"reasonix/internal/command"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/skill"
)

var ErrInvalidCatalogProjection = errors.New("runtime service: invalid session catalog projection")

// CatalogCommandSource is used for already-safe command namespaces such as MCP
// prompts. Custom command bodies and paths are accepted separately as
// command.Command values so ProjectSessionCatalog can prove it ignores them.
type CatalogCommandSource struct {
	Name        string
	Description string
}

// CatalogMCPSource is the deliberately narrow projection collected from a live
// MCP Host. Transport, command, args, URL, environment/header names, failures,
// and authentication state have no field here and therefore cannot cross the
// shared RuntimeService boundary.
type CatalogMCPSource struct {
	Name      string
	Available bool
	ToolCount int
}

type SessionCatalogSource struct {
	CustomCommands     []command.Command
	AdditionalCommands []CatalogCommandSource
	MCPServers         []CatalogMCPSource
	Skills             []skill.Skill
	Plugins            []pluginpkg.InstalledPlugin
}

// BuiltinCommandDescriptors is the target-neutral command catalogue used by
// Host catalog projection. Execution remains in Controller; these descriptors
// contain only names and presentation text and import no Desktop package.
func BuiltinCommandDescriptors() []runtimeapi.CommandCatalogItem {
	return []runtimeapi.CommandCatalogItem{
		{Name: "new", Description: "start a new session"},
		{Name: "clear", Description: "clear the current session"},
		{Name: "compact", Description: "compact the current context"},
		{Name: "model", Description: "select a configured model"},
		{Name: "provider", Description: "select a configured provider"},
		{Name: "effort", Description: "select reasoning effort"},
		{Name: "memory", Description: "inspect session memory"},
		{Name: "migrate", Description: "inspect memory migration"},
		{Name: "goal", Description: "manage the active goal"},
		{Name: "remember", Description: "save a memory note"},
		{Name: "mcp", Description: "manage MCP servers"},
		{Name: "hooks", Description: "inspect hooks"},
		{Name: "plugins", Description: "inspect installed plugins"},
		{Name: "theme", Description: "select the interface theme"},
		{Name: "skill", Description: "manage skills"},
		{Name: "reload-cmd", Description: "reload project commands"},
	}
}

func ProjectSessionCatalog(source SessionCatalogSource) (runtimeapi.SessionCatalog, error) {
	commands, err := projectCatalogCommands(source)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	mcpServers, err := projectCatalogMCP(source.MCPServers)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	skills, err := projectCatalogSkills(source.Skills)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	plugins, err := projectCatalogPlugins(source.Plugins)
	if err != nil {
		return runtimeapi.SessionCatalog{}, err
	}
	result := runtimeapi.SessionCatalog{
		Commands: commands, MCPServers: mcpServers, Skills: skills, Plugins: plugins,
	}
	revisionInput := struct {
		Commands   []runtimeapi.CommandCatalogItem   `json:"commands"`
		MCPServers []runtimeapi.MCPServerCatalogItem `json:"mcpServers"`
		Skills     []runtimeapi.SkillCatalogItem     `json:"skills"`
		Plugins    []runtimeapi.PluginCatalogItem    `json:"plugins"`
	}{commands, mcpServers, skills, plugins}
	raw, err := json.Marshal(revisionInput)
	if err != nil {
		return runtimeapi.SessionCatalog{}, ErrQueryFailed
	}
	sum := sha256.Sum256(raw)
	result.Revision = runtimeapi.CatalogRevision("catalog_" + base64.RawURLEncoding.EncodeToString(sum[:]))
	return result, nil
}

func projectCatalogCommands(source SessionCatalogSource) ([]runtimeapi.CommandCatalogItem, error) {
	byName := make(map[string]runtimeapi.CommandCatalogItem)
	add := func(name, description string) error {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if err := validateCatalogName("command", name); err != nil {
			return err
		}
		if !utf8.ValidString(description) {
			return ErrInvalidCatalogProjection
		}
		if _, exists := byName[name]; !exists {
			byName[name] = runtimeapi.CommandCatalogItem{Name: name, Description: description}
		}
		return nil
	}
	for _, item := range BuiltinCommandDescriptors() {
		if err := add(item.Name, item.Description); err != nil {
			return nil, err
		}
	}
	for _, item := range source.CustomCommands {
		if item.Hidden {
			continue
		}
		// Body, Source, Plugin roots, and argument implementation details are
		// intentionally not read here.
		if err := add(item.Name, item.Description); err != nil {
			return nil, err
		}
	}
	for _, item := range source.AdditionalCommands {
		if err := add(item.Name, item.Description); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]runtimeapi.CommandCatalogItem, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func projectCatalogMCP(items []CatalogMCPSource) ([]runtimeapi.MCPServerCatalogItem, error) {
	byName := make(map[string]runtimeapi.MCPServerCatalogItem, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if err := validateCatalogName("MCP server", name); err != nil || item.ToolCount < 0 {
			return nil, ErrInvalidCatalogProjection
		}
		current, exists := byName[name]
		if !exists || (!current.Available && item.Available) {
			byName[name] = runtimeapi.MCPServerCatalogItem{Name: name, Available: item.Available, ToolCount: item.ToolCount}
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]runtimeapi.MCPServerCatalogItem, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func projectCatalogSkills(items []skill.Skill) ([]runtimeapi.SkillCatalogItem, error) {
	byName := make(map[string]runtimeapi.SkillCatalogItem, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.SlashName())
		if err := validateCatalogName("skill", name); err != nil || !utf8.ValidString(item.Description) {
			return nil, ErrInvalidCatalogProjection
		}
		scope := strings.TrimSpace(string(item.Scope))
		if item.Plugin != "" || item.SlashPrefix != "" {
			scope = "plugin"
		}
		if scope == "" {
			return nil, ErrInvalidCatalogProjection
		}
		identity := string(item.Scope) + "\x00" + item.Plugin + "\x00" + item.SlashPrefix + "\x00" + item.Name
		sum := sha256.Sum256([]byte(identity))
		byName[name] = runtimeapi.SkillCatalogItem{
			ID:   runtimeapi.SkillID("skill_" + base64.RawURLEncoding.EncodeToString(sum[:])),
			Name: name, Description: item.Description, Scope: scope,
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]runtimeapi.SkillCatalogItem, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func projectCatalogPlugins(items []pluginpkg.InstalledPlugin) ([]runtimeapi.PluginCatalogItem, error) {
	byName := make(map[string]runtimeapi.PluginCatalogItem, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if err := validateCatalogName("plugin", name); err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(name))
		byName[name] = runtimeapi.PluginCatalogItem{
			ID: "plugin_" + base64.RawURLEncoding.EncodeToString(sum[:]), Name: name, Enabled: item.Enabled,
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]runtimeapi.PluginCatalogItem, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func validateCatalogName(kind, value string) error {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 256 {
		return ErrInvalidCatalogProjection
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ErrInvalidCatalogProjection
		}
	}
	_ = kind
	return nil
}
